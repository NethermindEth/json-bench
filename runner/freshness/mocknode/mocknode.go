// Package mocknode is a scriptable execution + beacon node for freshness
// tests. Tests decide when each part of a block (state, header, logs,
// receipts) becomes visible, and how the node phrases "not yet".
package mocknode

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
)

type Block struct {
	Number       uint64
	Hash         common.Hash
	ParentHash   common.Hash
	Timestamp    uint64
	Bloom        types.Bloom
	ReceiptsRoot common.Hash
	GasUsed      uint64
	Receipts     []*types.Receipt
}

func (b *Block) Logs() []*types.Log {
	var out []*types.Log
	for _, r := range b.Receipts {
		out = append(out, r.Logs...)
	}
	return out
}

// Visibility says which parts of a block the node serves. PartialLogs > 0
// serves only that many logs while Logs is false.
type Visibility struct {
	State       bool
	Header      bool
	Logs        bool
	PartialLogs int
	Receipts    bool
}

func All() Visibility { return Visibility{State: true, Header: true, Logs: true, Receipts: true} }

// NotReadyStyle shapes the node's answer for data it does not have.
type NotReadyStyle struct {
	Code       int
	Message    string
	HTTPStatus int
	EmptyLogs  bool
}

var DefaultStyle = NotReadyStyle{Code: -32000, Message: "header not found"}

// chain is shared by every node of a test network, so peers serve the same
// block identities while each keeps its own visibility.
type chain struct {
	mu        sync.Mutex
	chainID   uint64
	blocks    map[uint64]*Block
	byHash    map[common.Hash]*Block
	tip       uint64
	salt      uint64
	genesisTS uint64
	slotSecs  uint64
}

// Node fields are guarded by the shared chain mutex.
type Node struct {
	*chain
	vis     map[uint64]Visibility
	style   NotReadyStyle
	syncing bool
	delay   map[string]time.Duration
	calls   map[string]int
	server  *httptest.Server
	beacon  *httptest.Server
	subsMu  sync.Mutex
	subs    []chan string
}

// New builds a chain of history blocks 0..historyLen-1, all visible, with
// timestamps one slot apart ending now.
func New(chainID uint64, historyLen int, slotSecs uint64) *Node {
	c := &chain{
		chainID:  chainID,
		blocks:   map[uint64]*Block{},
		byHash:   map[common.Hash]*Block{},
		slotSecs: slotSecs,
	}
	now := uint64(time.Now().Unix())
	c.genesisTS = now - uint64(historyLen-1)*slotSecs
	n := newNode(c)
	for i := 0; i < historyLen; i++ {
		b := n.build(uint64(i), c.genesisTS+uint64(i)*slotSecs, 2, 2)
		c.blocks[b.Number] = b
		c.byHash[b.Hash] = b
		n.vis[b.Number] = All()
		c.tip = b.Number
	}
	return n
}

// Peer returns another node on the same chain with its own endpoints and
// visibility; blocks visible on n are visible on the peer too.
func (n *Node) Peer() *Node {
	n.mu.Lock()
	p := newNode(n.chain)
	for num, v := range n.vis {
		if v == All() {
			p.vis[num] = v
		}
	}
	n.mu.Unlock()
	return p
}

func newNode(c *chain) *Node {
	n := &Node{
		chain: c,
		vis:   map[uint64]Visibility{},
		style: DefaultStyle,
		delay: map[string]time.Duration{},
		calls: map[string]int{},
	}
	n.server = httptest.NewServer(http.HandlerFunc(n.serveRPC))
	n.beacon = httptest.NewServer(http.HandlerFunc(n.serveBeacon))
	return n
}

func (n *Node) URL() string       { return n.server.URL }
func (n *Node) BeaconURL() string { return n.beacon.URL }
func (n *Node) Close() {
	n.subsMu.Lock()
	for _, c := range n.subs {
		close(c)
	}
	n.subs = nil
	n.subsMu.Unlock()
	n.server.CloseClientConnections()
	n.beacon.CloseClientConnections()
	n.server.Close()
	n.beacon.Close()
}

func (n *Node) SetStyle(s NotReadyStyle) { n.mu.Lock(); n.style = s; n.mu.Unlock() }
func (n *Node) SetSyncing(v bool)        { n.mu.Lock(); n.syncing = v; n.mu.Unlock() }

// SetDelay adds latency to every call of method.
func (n *Node) SetDelay(method string, d time.Duration) {
	n.mu.Lock()
	n.delay[method] = d
	n.mu.Unlock()
}

func (n *Node) Calls(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[method]
}

func (n *Node) Tip() *Block {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.blocks[n.tip]
}

func (n *Node) Block(num uint64) *Block {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.blocks[num]
}

// Mine appends an invisible block with txs transactions of logsPerTx logs
// each. slotsLater places its timestamp that many slots after the tip, so
// slotsLater > 1 models missed slots.
func (n *Node) Mine(txs, logsPerTx int, slotsLater uint64) *Block {
	n.mu.Lock()
	defer n.mu.Unlock()
	parent := n.blocks[n.tip]
	b := n.buildOn(parent, parent.Timestamp+slotsLater*n.slotSecs, txs, logsPerTx)
	n.blocks[b.Number] = b
	n.byHash[b.Hash] = b
	n.vis[b.Number] = Visibility{}
	n.tip = b.Number
	return b
}

// MineAt is Mine with an explicit timestamp.
func (n *Node) MineAt(ts uint64, txs, logsPerTx int) *Block {
	n.mu.Lock()
	defer n.mu.Unlock()
	parent := n.blocks[n.tip]
	b := n.buildOn(parent, ts, txs, logsPerTx)
	n.blocks[b.Number] = b
	n.byHash[b.Hash] = b
	n.vis[b.Number] = Visibility{}
	n.tip = b.Number
	return b
}

// Replace swaps the canonical block at num for a sibling (same parent,
// different hash), keeping the old one reachable by hash.
func (n *Node) Replace(num uint64, txs, logsPerTx int) *Block {
	n.mu.Lock()
	defer n.mu.Unlock()
	old := n.blocks[num]
	parent := n.blocks[num-1]
	b := n.buildOn(parent, old.Timestamp, txs, logsPerTx)
	n.blocks[num] = b
	n.byHash[b.Hash] = b
	return b
}

func (n *Node) Publish(num uint64, v Visibility) {
	n.mu.Lock()
	n.vis[num] = v
	n.mu.Unlock()
	if v.Header {
		n.broadcast(num)
	}
}

// Genesis returns the beacon genesis time the mock reports.
func (n *Node) Genesis() uint64 { return n.genesisTS }

func (n *Node) build(num, ts uint64, txs, logsPerTx int) *Block {
	var parent *Block
	if num > 0 {
		parent = n.blocks[num-1]
	}
	if parent == nil {
		parent = &Block{Number: ^uint64(0)}
	}
	return n.buildOn(parent, ts, txs, logsPerTx)
}

func (n *Node) buildOn(parent *Block, ts uint64, txs, logsPerTx int) *Block {
	num := parent.Number + 1
	n.salt++
	seed := make([]byte, 16)
	binary.BigEndian.PutUint64(seed, num)
	binary.BigEndian.PutUint64(seed[8:], n.salt)
	b := &Block{Number: num, Hash: crypto.Keccak256Hash(seed), ParentHash: parent.Hash, Timestamp: ts}
	if num == 0 {
		b.ParentHash = common.Hash{}
	}
	var cum uint64
	logIndex := uint(0)
	for i := 0; i < txs; i++ {
		txHash := crypto.Keccak256Hash(b.Hash.Bytes(), []byte{byte(i), byte(i >> 8)})
		gas := uint64(21000 + 1000*i)
		cum += gas
		r := &types.Receipt{
			Type:              types.DynamicFeeTxType,
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: cum,
			TxHash:            txHash,
			GasUsed:           gas,
			EffectiveGasPrice: big.NewInt(1),
			BlockHash:         b.Hash,
			BlockNumber:       new(big.Int).SetUint64(num),
			TransactionIndex:  uint(i),
		}
		for j := 0; j < logsPerTx; j++ {
			addr := common.BigToAddress(big.NewInt(int64(1000 + j)))
			r.Logs = append(r.Logs, &types.Log{
				Address:     addr,
				Topics:      []common.Hash{crypto.Keccak256Hash([]byte(fmt.Sprintf("topic-%d-%d-%d", num, i, j)))},
				Data:        []byte{byte(i), byte(j)},
				BlockNumber: num,
				TxHash:      txHash,
				TxIndex:     uint(i),
				BlockHash:   b.Hash,
				Index:       logIndex,
			})
			logIndex++
		}
		r.Bloom = types.CreateBloom(r)
		b.Receipts = append(b.Receipts, r)
	}
	b.GasUsed = cum
	b.Bloom = types.MergeBloom(b.Receipts)
	b.ReceiptsRoot = types.DeriveSha(types.Receipts(b.Receipts), trie.NewStackTrie(nil))
	return b
}

type rpcReq struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (n *Node) serveRPC(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	var r rpcReq
	if err := json.Unmarshal(body, &r); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	n.mu.Lock()
	n.calls[r.Method]++
	d := n.delay[r.Method]
	n.mu.Unlock()
	if d > 0 {
		time.Sleep(d)
	}
	result, rerr, status := n.dispatch(r)
	resp := map[string]any{"jsonrpc": "2.0", "id": r.ID}
	if rerr != nil {
		resp["error"] = rerr
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	if status != 0 {
		w.WriteHeader(status)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (n *Node) notReady() (any, *rpcErr, int) {
	return nil, &rpcErr{Code: n.style.Code, Message: n.style.Message}, n.style.HTTPStatus
}

func (n *Node) dispatch(r rpcReq) (any, *rpcErr, int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	str := func(i int) string {
		if i >= len(r.Params) {
			return ""
		}
		var s string
		_ = json.Unmarshal(r.Params[i], &s)
		return s
	}
	switch r.Method {
	case "eth_chainId":
		return hexutil.EncodeUint64(n.chainID), nil, 0
	case "web3_clientVersion":
		return "mocknode/v1.0.0", nil, 0
	case "eth_syncing":
		if n.syncing {
			return map[string]any{"currentBlock": "0x1", "highestBlock": "0x2"}, nil, 0
		}
		return false, nil, 0
	case "eth_blockNumber":
		return hexutil.EncodeUint64(n.headLocked(func(v Visibility) bool { return v.Header })), nil, 0
	case "eth_getBlockByNumber":
		num, ok := n.resolveTag(str(0), func(v Visibility) bool { return v.Header })
		if !ok {
			return nil, nil, 0
		}
		return n.blockJSON(n.blocks[num]), nil, 0
	case "eth_getBlockByHash":
		b := n.byHash[common.HexToHash(str(0))]
		if b == nil || !n.vis[b.Number].Header {
			return nil, nil, 0
		}
		return n.blockJSON(b), nil, 0
	case "eth_getCode":
		if strings.EqualFold(str(0), "0x0000F90827F1C53a10cb7A02335B175320002935") {
			return "0x3373fffffffffffffffffffffffffffffffffffffffe14604657602036036042575f35600143038111604257611fff81430311604257611fff9006545f5260205ff35b5f5ffd5b5f35611fff60014303065500", nil, 0
		}
		return "0x", nil, 0
	case "eth_call":
		return n.call(r)
	case "eth_getLogs":
		return n.logs(r)
	case "eth_getBlockReceipts":
		b := n.byHash[common.HexToHash(str(0))]
		if b == nil {
			if num, ok := n.resolveTag(str(0), func(v Visibility) bool { return v.Receipts }); ok {
				b = n.blocks[num]
			}
		}
		if b == nil || !n.vis[b.Number].Receipts {
			return nil, nil, 0
		}
		return b.Receipts, nil, 0
	case "eth_getTransactionReceipt":
		h := common.HexToHash(str(0))
		for _, b := range n.blocks {
			for _, rc := range b.Receipts {
				if rc.TxHash == h && n.vis[b.Number].Receipts {
					return rc, nil, 0
				}
			}
		}
		return nil, nil, 0
	}
	return nil, &rpcErr{Code: -32601, Message: "the method " + r.Method + " does not exist/is not available"}, 0
}

func (n *Node) headLocked(ok func(Visibility) bool) uint64 {
	var head uint64
	for num := uint64(0); num <= n.tip; num++ {
		if !ok(n.vis[num]) {
			break
		}
		head = num
	}
	return head
}

func (n *Node) resolveTag(tag string, ok func(Visibility) bool) (uint64, bool) {
	switch tag {
	case "latest", "safe", "finalized", "":
		return n.headLocked(ok), true
	case "earliest":
		return 0, true
	}
	num, err := hexutil.DecodeUint64(tag)
	if err != nil {
		return 0, false
	}
	if _, exists := n.blocks[num]; !exists || !ok(n.vis[num]) {
		return 0, false
	}
	return num, true
}

func (n *Node) call(r rpcReq) (any, *rpcErr, int) {
	var obj struct {
		Data string `json:"data"`
	}
	_ = json.Unmarshal(r.Params[0], &obj)
	x := new(big.Int).SetBytes(common.FromHex(obj.Data))
	var target *Block
	var sel string
	if json.Unmarshal(r.Params[1], &sel) == nil {
		num, ok := n.resolveTag(sel, func(v Visibility) bool { return v.State })
		if !ok {
			return n.notReady()
		}
		target = n.blocks[num]
	} else {
		var bh struct {
			BlockHash        string `json:"blockHash"`
			RequireCanonical bool   `json:"requireCanonical"`
		}
		_ = json.Unmarshal(r.Params[1], &bh)
		target = n.byHash[common.HexToHash(bh.BlockHash)]
		if target == nil || !n.vis[target.Number].State {
			return nil, &rpcErr{Code: -32000, Message: "header for hash not found"}, 0
		}
		if bh.RequireCanonical && n.blocks[target.Number] != target {
			return nil, &rpcErr{Code: -32000, Message: "hash is not currently canonical"}, 0
		}
	}
	if !x.IsUint64() || x.Uint64() >= target.Number {
		return nil, &rpcErr{Code: 3, Message: "execution reverted"}, 0
	}
	// The history contract reads the ancestor of the block the call runs on.
	anc := target
	for anc != nil && anc.Number > x.Uint64() {
		anc = n.byHash[anc.ParentHash]
	}
	if anc == nil {
		return nil, &rpcErr{Code: 3, Message: "execution reverted"}, 0
	}
	return anc.Hash.Hex(), nil, 0
}

func (n *Node) logs(r rpcReq) (any, *rpcErr, int) {
	var f struct {
		FromBlock string `json:"fromBlock"`
		ToBlock   string `json:"toBlock"`
		BlockHash string `json:"blockHash"`
	}
	_ = json.Unmarshal(r.Params[0], &f)
	var b *Block
	if f.BlockHash != "" {
		b = n.byHash[common.HexToHash(f.BlockHash)]
		if b == nil {
			return nil, &rpcErr{Code: -32000, Message: "unknown block"}, 0
		}
	} else {
		num, err := hexutil.DecodeUint64(f.FromBlock)
		if err != nil {
			return nil, &rpcErr{Code: -32602, Message: "invalid params"}, 0
		}
		b = n.blocks[num]
		if b == nil || !n.vis[num].Header {
			if n.style.EmptyLogs {
				return []any{}, nil, 0
			}
			return n.notReady()
		}
	}
	v := n.vis[b.Number]
	logs := b.Logs()
	switch {
	case v.Logs:
	case v.PartialLogs > 0 && v.PartialLogs < len(logs):
		logs = logs[:v.PartialLogs]
	default:
		logs = nil
	}
	if logs == nil {
		return []any{}, nil, 0
	}
	return logs, nil, 0
}

func (n *Node) blockJSON(b *Block) map[string]any {
	txs := make([]string, len(b.Receipts))
	for i, r := range b.Receipts {
		txs[i] = r.TxHash.Hex()
	}
	return map[string]any{
		"number":       hexutil.EncodeUint64(b.Number),
		"hash":         b.Hash.Hex(),
		"parentHash":   b.ParentHash.Hex(),
		"timestamp":    hexutil.EncodeUint64(b.Timestamp),
		"gasUsed":      hexutil.EncodeUint64(b.GasUsed),
		"logsBloom":    hexutil.Encode(b.Bloom.Bytes()),
		"receiptsRoot": b.ReceiptsRoot.Hex(),
		"transactions": txs,
	}
}

func (n *Node) serveBeacon(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch req.URL.Path {
	case "/eth/v1/node/syncing":
		n.mu.Lock()
		syncing := n.syncing
		n.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"head_slot": "1", "sync_distance": "0", "is_syncing": syncing, "is_optimistic": false, "el_offline": false}})
	case "/eth/v1/node/version":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": "mockbeacon/v1.0.0"}})
	case "/eth/v1/config/spec":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"SECONDS_PER_SLOT": strconv.FormatUint(n.slotSecs, 10)}})
	case "/eth/v1/beacon/genesis":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"genesis_time": strconv.FormatUint(n.genesisTS, 10)}})
	case "/eth/v1/events":
		n.serveEvents(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (n *Node) serveEvents(w http.ResponseWriter, req *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
		return
	}
	ch := make(chan string, 64)
	n.subsMu.Lock()
	n.subs = append(n.subs, ch)
	n.subsMu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	for {
		select {
		case <-req.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = io.WriteString(w, msg)
			fl.Flush()
		}
	}
}

func (n *Node) broadcast(num uint64) {
	n.mu.Lock()
	b := n.blocks[num]
	n.mu.Unlock()
	if b == nil {
		return
	}
	slot := (b.Timestamp - n.genesisTS) / n.slotSecs
	msg := fmt.Sprintf("event: head\ndata: {\"slot\":\"%d\",\"block\":\"%s\",\"execution_optimistic\":false}\n\n", slot, b.Hash.Hex())
	n.subsMu.Lock()
	defer n.subsMu.Unlock()
	for _, c := range n.subs {
		select {
		case c <- msg:
		default:
		}
	}
}
