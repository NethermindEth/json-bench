package ethrpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// Canonical forms: every hex value lower-case, quantities minimal, fixed field
// set. Two clients returning the same data in different JSON shapes produce
// the same canonical value and therefore the same digest.

// Digest hashes the JSON encoding of a canonical value.
func Digest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("canonical value is not JSON-encodable: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func quantity(raw json.RawMessage) (uint64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, fmt.Errorf("missing quantity")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return 0, fmt.Errorf("quantity %s: %w", raw, err)
		}
		return strconv.ParseUint(n.String(), 10, 64)
	}
	s = strings.TrimSpace(strings.ToLower(s))
	if !strings.HasPrefix(s, "0x") {
		return strconv.ParseUint(s, 10, 64)
	}
	body := strings.TrimLeft(s[2:], "0")
	if body == "" {
		return 0, nil
	}
	return strconv.ParseUint(body, 16, 64)
}

func optQuantity(raw json.RawMessage) (uint64, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false, nil
	}
	v, err := quantity(raw)
	return v, err == nil, err
}

func hexData(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "0x", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("data %s: %w", raw, err)
	}
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 == 1 {
		return "", fmt.Errorf("odd-length hex data")
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", err
	}
	return "0x" + s, nil
}

func fixedHex(raw json.RawMessage, size int) (string, error) {
	s, err := hexData(raw)
	if err != nil {
		return "", err
	}
	if len(s) != 2+2*size {
		return "", fmt.Errorf("expected %d bytes, got %q", size, s)
	}
	return s, nil
}

// optAddress normalises an optional address, mapping null and the zero
// address to "" (clients disagree on which one they return for "none").
func optAddress(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	s, err := fixedHex(raw, common.AddressLength)
	if err != nil || s == "0x"+strings.Repeat("0", 40) {
		return "", err
	}
	return s, nil
}

// CanonHash normalises a 32-byte hex value (the EIP-2935 canary result).
func CanonHash(raw json.RawMessage) (string, error) {
	return fixedHex(raw, common.HashLength)
}

// Log is the canonical log representation.
type Log struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	BlockHash        string   `json:"blockHash"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex string   `json:"transactionIndex"`
	LogIndex         string   `json:"logIndex"`
	Removed          bool     `json:"removed"`

	blockNumber uint64
	logIndex    uint64
}

type rawLog struct {
	Address          json.RawMessage   `json:"address"`
	Topics           []json.RawMessage `json:"topics"`
	Data             json.RawMessage   `json:"data"`
	BlockNumber      json.RawMessage   `json:"blockNumber"`
	BlockHash        json.RawMessage   `json:"blockHash"`
	TransactionHash  json.RawMessage   `json:"transactionHash"`
	TransactionIndex json.RawMessage   `json:"transactionIndex"`
	LogIndex         json.RawMessage   `json:"logIndex"`
	Removed          *bool             `json:"removed"`
}

func canonLog(r rawLog) (Log, error) {
	var l Log
	var err error
	if l.Address, err = fixedHex(r.Address, common.AddressLength); err != nil {
		return l, fmt.Errorf("address: %w", err)
	}
	l.Topics = make([]string, 0, len(r.Topics))
	for _, t := range r.Topics {
		h, err := fixedHex(t, common.HashLength)
		if err != nil {
			return l, fmt.Errorf("topic: %w", err)
		}
		l.Topics = append(l.Topics, h)
	}
	if l.Data, err = hexData(r.Data); err != nil {
		return l, fmt.Errorf("data: %w", err)
	}
	if l.blockNumber, err = quantity(r.BlockNumber); err != nil {
		return l, fmt.Errorf("blockNumber: %w", err)
	}
	l.BlockNumber = hexutil.EncodeUint64(l.blockNumber)
	if l.BlockHash, err = fixedHex(r.BlockHash, common.HashLength); err != nil {
		return l, fmt.Errorf("blockHash: %w", err)
	}
	if l.TransactionHash, err = fixedHex(r.TransactionHash, common.HashLength); err != nil {
		return l, fmt.Errorf("transactionHash: %w", err)
	}
	txIndex, err := quantity(r.TransactionIndex)
	if err != nil {
		return l, fmt.Errorf("transactionIndex: %w", err)
	}
	l.TransactionIndex = hexutil.EncodeUint64(txIndex)
	if l.logIndex, err = quantity(r.LogIndex); err != nil {
		return l, fmt.Errorf("logIndex: %w", err)
	}
	l.LogIndex = hexutil.EncodeUint64(l.logIndex)
	l.Removed = r.Removed != nil && *r.Removed
	return l, nil
}

// LogSet is a canonical, logIndex-ordered log list. OrderOK records whether
// the node returned them in that order already; duplicates are kept.
type LogSet struct {
	Logs    []Log
	OrderOK bool
}

func (s LogSet) Digest() string { return Digest(s.Logs) }

func sortLogs(logs []Log) bool {
	ordered := sort.SliceIsSorted(logs, func(i, j int) bool { return logs[i].logIndex < logs[j].logIndex })
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].logIndex < logs[j].logIndex })
	return ordered
}

// CanonLogs parses an eth_getLogs result.
func CanonLogs(raw json.RawMessage) (LogSet, error) {
	var rs []rawLog
	if err := json.Unmarshal(raw, &rs); err != nil {
		return LogSet{}, fmt.Errorf("logs: %w", err)
	}
	logs := make([]Log, 0, len(rs))
	for i, r := range rs {
		l, err := canonLog(r)
		if err != nil {
			return LogSet{}, fmt.Errorf("log %d: %w", i, err)
		}
		logs = append(logs, l)
	}
	ordered := sortLogs(logs)
	return LogSet{Logs: logs, OrderOK: ordered}, nil
}

// LogsBloom recomputes the bloom a header would carry for these logs.
func LogsBloom(logs []Log) types.Bloom {
	var b types.Bloom
	for _, l := range logs {
		b.Add(common.FromHex(l.Address))
		for _, t := range l.Topics {
			b.Add(common.FromHex(t))
		}
	}
	return b
}

// Receipt is the canonical receipt: consensus fields plus inclusion identity.
// Client-specific extras (from/to, effectiveGasPrice, blob fields) are left
// out because references disagree on them without the data being wrong.
type Receipt struct {
	Type              string `json:"type"`
	Status            string `json:"status,omitempty"`
	Root              string `json:"root,omitempty"`
	CumulativeGasUsed string `json:"cumulativeGasUsed"`
	GasUsed           string `json:"gasUsed"`
	LogsBloom         string `json:"logsBloom"`
	Logs              []Log  `json:"logs"`
	TransactionHash   string `json:"transactionHash"`
	TransactionIndex  string `json:"transactionIndex"`
	BlockHash         string `json:"blockHash"`
	BlockNumber       string `json:"blockNumber"`
	ContractAddress   string `json:"contractAddress,omitempty"`

	txType  uint64
	status  uint64
	cumGas  uint64
	txIndex uint64
}

type rawReceipt struct {
	Type              json.RawMessage `json:"type"`
	Status            json.RawMessage `json:"status"`
	Root              json.RawMessage `json:"root"`
	CumulativeGasUsed json.RawMessage `json:"cumulativeGasUsed"`
	GasUsed           json.RawMessage `json:"gasUsed"`
	LogsBloom         json.RawMessage `json:"logsBloom"`
	Logs              []rawLog        `json:"logs"`
	TransactionHash   json.RawMessage `json:"transactionHash"`
	TransactionIndex  json.RawMessage `json:"transactionIndex"`
	BlockHash         json.RawMessage `json:"blockHash"`
	BlockNumber       json.RawMessage `json:"blockNumber"`
	ContractAddress   json.RawMessage `json:"contractAddress"`
}

func canonReceipt(r rawReceipt) (Receipt, error) {
	var c Receipt
	var err error
	if c.txType, _, err = optQuantity(r.Type); err != nil {
		return c, fmt.Errorf("type: %w", err)
	}
	c.Type = hexutil.EncodeUint64(c.txType)
	root, err := hexData(r.Root)
	if err != nil {
		return c, fmt.Errorf("root: %w", err)
	}
	if root != "0x" {
		c.Root = root
	}
	status, hasStatus, err := optQuantity(r.Status)
	if err != nil {
		return c, fmt.Errorf("status: %w", err)
	}
	if hasStatus {
		c.status = status
		c.Status = hexutil.EncodeUint64(status)
	} else if c.Root == "" {
		return c, fmt.Errorf("receipt has neither status nor root")
	}
	if c.cumGas, err = quantity(r.CumulativeGasUsed); err != nil {
		return c, fmt.Errorf("cumulativeGasUsed: %w", err)
	}
	c.CumulativeGasUsed = hexutil.EncodeUint64(c.cumGas)
	gas, err := quantity(r.GasUsed)
	if err != nil {
		return c, fmt.Errorf("gasUsed: %w", err)
	}
	c.GasUsed = hexutil.EncodeUint64(gas)
	if c.LogsBloom, err = fixedHex(r.LogsBloom, types.BloomByteLength); err != nil {
		return c, fmt.Errorf("logsBloom: %w", err)
	}
	c.Logs = make([]Log, 0, len(r.Logs))
	for i, rl := range r.Logs {
		l, err := canonLog(rl)
		if err != nil {
			return c, fmt.Errorf("log %d: %w", i, err)
		}
		c.Logs = append(c.Logs, l)
	}
	sortLogs(c.Logs)
	if c.TransactionHash, err = fixedHex(r.TransactionHash, common.HashLength); err != nil {
		return c, fmt.Errorf("transactionHash: %w", err)
	}
	if c.txIndex, err = quantity(r.TransactionIndex); err != nil {
		return c, fmt.Errorf("transactionIndex: %w", err)
	}
	c.TransactionIndex = hexutil.EncodeUint64(c.txIndex)
	if c.BlockHash, err = fixedHex(r.BlockHash, common.HashLength); err != nil {
		return c, fmt.Errorf("blockHash: %w", err)
	}
	num, err := quantity(r.BlockNumber)
	if err != nil {
		return c, fmt.Errorf("blockNumber: %w", err)
	}
	c.BlockNumber = hexutil.EncodeUint64(num)
	if c.ContractAddress, err = optAddress(r.ContractAddress); err != nil {
		return c, fmt.Errorf("contractAddress: %w", err)
	}
	return c, nil
}

// CanonReceipt parses an eth_getTransactionReceipt result.
func CanonReceipt(raw json.RawMessage) (Receipt, error) {
	var r rawReceipt
	if err := json.Unmarshal(raw, &r); err != nil {
		return Receipt{}, fmt.Errorf("receipt: %w", err)
	}
	return canonReceipt(r)
}

// CanonReceipts parses an eth_getBlockReceipts result, ordered by
// transaction index.
func CanonReceipts(raw json.RawMessage) ([]Receipt, error) {
	var rs []rawReceipt
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, fmt.Errorf("receipts: %w", err)
	}
	out := make([]Receipt, 0, len(rs))
	for i, r := range rs {
		c, err := canonReceipt(r)
		if err != nil {
			return nil, fmt.Errorf("receipt %d: %w", i, err)
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].txIndex < out[j].txIndex })
	return out, nil
}

// ReceiptsRoot derives the header receipts root from canonical receipts.
func ReceiptsRoot(rs []Receipt) common.Hash {
	list := make(types.Receipts, len(rs))
	for i, c := range rs {
		r := &types.Receipt{
			Type:              uint8(c.txType),
			CumulativeGasUsed: c.cumGas,
			Bloom:             types.BytesToBloom(common.FromHex(c.LogsBloom)),
		}
		if c.Root != "" {
			r.PostState = common.FromHex(c.Root)
		} else {
			r.Status = c.status
		}
		r.Logs = make([]*types.Log, len(c.Logs))
		for j, l := range c.Logs {
			topics := make([]common.Hash, len(l.Topics))
			for k, t := range l.Topics {
				topics[k] = common.HexToHash(t)
			}
			r.Logs[j] = &types.Log{Address: common.HexToAddress(l.Address), Topics: topics, Data: common.FromHex(l.Data)}
		}
		list[i] = r
	}
	return types.DeriveSha(list, trie.NewStackTrie(nil))
}

// BlockLogs flattens receipts into the log set eth_getLogs must return for
// the block.
func BlockLogs(rs []Receipt) LogSet {
	var logs []Log
	for _, r := range rs {
		logs = append(logs, r.Logs...)
	}
	if logs == nil {
		logs = []Log{}
	}
	sortLogs(logs)
	return LogSet{Logs: logs, OrderOK: true}
}

// Header is the subset of a block the probe needs. Transactions accepts both
// hash-only and full-object forms.
type Header struct {
	Hash         common.Hash
	ParentHash   common.Hash
	Number       uint64
	Timestamp    uint64
	GasUsed      uint64
	LogsBloom    types.Bloom
	ReceiptsRoot common.Hash
	TxHashes     []common.Hash
}

type rawHeader struct {
	Hash         json.RawMessage   `json:"hash"`
	ParentHash   json.RawMessage   `json:"parentHash"`
	Number       json.RawMessage   `json:"number"`
	Timestamp    json.RawMessage   `json:"timestamp"`
	GasUsed      json.RawMessage   `json:"gasUsed"`
	LogsBloom    json.RawMessage   `json:"logsBloom"`
	ReceiptsRoot json.RawMessage   `json:"receiptsRoot"`
	Transactions []json.RawMessage `json:"transactions"`
}

// ParseHeader parses an eth_getBlockBy* result. A null result returns
// (nil, nil).
func ParseHeader(raw json.RawMessage) (*Header, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var r rawHeader
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("block: %w", err)
	}
	h := &Header{}
	fields := []struct {
		name string
		raw  json.RawMessage
		dst  *common.Hash
	}{{"hash", r.Hash, &h.Hash}, {"parentHash", r.ParentHash, &h.ParentHash}, {"receiptsRoot", r.ReceiptsRoot, &h.ReceiptsRoot}}
	for _, f := range fields {
		s, err := fixedHex(f.raw, common.HashLength)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.name, err)
		}
		*f.dst = common.HexToHash(s)
	}
	var err error
	if h.Number, err = quantity(r.Number); err != nil {
		return nil, fmt.Errorf("number: %w", err)
	}
	if h.Timestamp, err = quantity(r.Timestamp); err != nil {
		return nil, fmt.Errorf("timestamp: %w", err)
	}
	if h.GasUsed, _, err = optQuantity(r.GasUsed); err != nil {
		return nil, fmt.Errorf("gasUsed: %w", err)
	}
	bloom, err := fixedHex(r.LogsBloom, types.BloomByteLength)
	if err != nil {
		return nil, fmt.Errorf("logsBloom: %w", err)
	}
	h.LogsBloom = types.BytesToBloom(common.FromHex(bloom))
	for i, tx := range r.Transactions {
		var obj struct {
			Hash json.RawMessage `json:"hash"`
		}
		src := tx
		if len(tx) > 0 && tx[0] == '{' {
			if err := json.Unmarshal(tx, &obj); err != nil {
				return nil, fmt.Errorf("transaction %d: %w", i, err)
			}
			src = obj.Hash
		}
		s, err := fixedHex(src, common.HashLength)
		if err != nil {
			return nil, fmt.Errorf("transaction %d: %w", i, err)
		}
		h.TxHashes = append(h.TxHashes, common.HexToHash(s))
	}
	return h, nil
}

// ParseQuantity parses a JSON-RPC quantity result such as eth_blockNumber.
func ParseQuantity(raw json.RawMessage) (uint64, error) { return quantity(raw) }

// ParseBigQuantity parses a quantity that may exceed uint64 (eth_chainId is
// small in practice, but the spec does not bound it).
func ParseBigQuantity(raw json.RawMessage) (*big.Int, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return hexutil.DecodeBig(normalizeLeadingZeros(s))
}

func normalizeLeadingZeros(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	body := strings.TrimLeft(strings.TrimPrefix(s, "0x"), "0")
	if body == "" {
		body = "0"
	}
	return "0x" + body
}

// HistoryCalldata is the EIP-2935 read input: the 32-byte big-endian block
// number, without an ABI selector.
func HistoryCalldata(number uint64) string {
	return hexutil.Encode(common.BigToHash(new(big.Int).SetUint64(number)).Bytes())
}

// HistoryContract is the EIP-2935 history storage address.
const HistoryContract = "0x0000F90827F1C53a10cb7A02335B175320002935"

func Quantity(n uint64) string { return hexutil.EncodeUint64(n) }

func HashHex(h common.Hash) string { return strings.ToLower(h.Hex()) }
