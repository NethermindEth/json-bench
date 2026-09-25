package ethrpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/freshness/schema"
)

func TestNormalizeMessage(t *testing.T) {
	require.Equal(t, "header not found for block #", NormalizeMessage("Header not found for block 0x1A2b"))
	require.Equal(t, "block # is not available", NormalizeMessage("  block   19999 is not available "))
}

func TestParseTolerantEnvelopes(t *testing.T) {
	cases := map[string]struct {
		resp  Response
		class string
		code  *int
	}{
		"string code":    {Response{HTTPStatus: 200, Body: []byte(`{"id":1,"error":{"code":"-32000","message":"x"}}`)}, schema.ClassRPCError, intp(-32000)},
		"object message": {Response{HTTPStatus: 200, Body: []byte(`{"id":1,"error":{"code":-1,"message":{"a":1}}}`)}, schema.ClassRPCError, intp(-1)},
		"500 json error": {Response{HTTPStatus: 500, Body: []byte(`{"error":{"code":-32603,"message":"internal"}}`)}, schema.ClassRPCError, intp(-32603)},
		"500 html":       {Response{HTTPStatus: 500, Body: []byte(`<html>oops</html>`)}, schema.ClassHTTPError, nil},
		"truncated":      {Response{HTTPStatus: 200, Body: []byte(`{"result":[{"a"`)}, schema.ClassBadBody, nil},
		"null result":    {Response{HTTPStatus: 200, Body: []byte(`{"result":null}`)}, schema.ClassNull, nil},
		"missing result": {Response{HTTPStatus: 200, Body: []byte(`{"id":1}`)}, schema.ClassBadBody, nil},
		"ok":             {Response{HTTPStatus: 200, Body: []byte(`{"result":"0x1"}`)}, schema.ClassResult, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := Parse(c.resp)
			require.Equal(t, c.class, r.Class)
			require.Equal(t, c.code, r.Code)
		})
	}
}

func intp(v int) *int { return &v }

func TestNotReadyMatching(t *testing.T) {
	sig := Signature(Reply{Class: schema.ClassRPCError, Code: intp(-32000), Message: "header not found for 0x2710"})
	require.Equal(t, schema.SigError, sig.Kind)

	nr, weak := NotReady(sig, Reply{Class: schema.ClassRPCError, Code: intp(-32000), Message: "header not found for 0x9"})
	require.True(t, nr)
	require.False(t, weak)

	nr, weak = NotReady(sig, Reply{Class: schema.ClassRPCError, Code: intp(-39001), Message: "Unknown block"})
	require.True(t, nr)
	require.True(t, weak, "a phrase-only match is weaker than the preflight signature")

	nr, _ = NotReady(sig, Reply{Class: schema.ClassRPCError, Code: intp(-32601), Message: "method not found"})
	require.False(t, nr)

	nr, _ = NotReady(sig, Reply{Class: schema.ClassRPCError, Code: intp(3), Message: "execution reverted"})
	require.False(t, nr, "a revert is not proof the block is unknown")

	empty := Signature(Reply{Class: schema.ClassResult, Result: json.RawMessage(` [ ] `)})
	require.Equal(t, schema.SigEmptyArray, empty.Kind)
	nr, _ = NotReady(empty, Reply{Class: schema.ClassResult, Result: json.RawMessage(`[]`)})
	require.True(t, nr)

	require.Nil(t, Signature(Reply{Class: schema.ClassResult, Result: json.RawMessage(`"0x01"`)}))
}

func TestCanonicalLogsIgnoreRepresentation(t *testing.T) {
	a := `[{"address":"0xABCDEF0000000000000000000000000000000001","topics":["0x00000000000000000000000000000000000000000000000000000000000000AA"],"data":"0xFF","blockNumber":"0x0a","blockHash":"0x00000000000000000000000000000000000000000000000000000000000000BB","transactionHash":"0x00000000000000000000000000000000000000000000000000000000000000CC","transactionIndex":"0x0","logIndex":"0x1","removed":false},
	       {"address":"0xabcdef0000000000000000000000000000000001","topics":[],"data":"0x","blockNumber":"0xa","blockHash":"0x00000000000000000000000000000000000000000000000000000000000000bb","transactionHash":"0x00000000000000000000000000000000000000000000000000000000000000cc","transactionIndex":"0x0","logIndex":"0x0","blockTimestamp":"0x1"}]`
	b := `[{"logIndex":"0x0","removed":false,"address":"0xabcdef0000000000000000000000000000000001","topics":[],"data":"0x","blockNumber":"0xa","blockHash":"0x00000000000000000000000000000000000000000000000000000000000000bb","transactionHash":"0x00000000000000000000000000000000000000000000000000000000000000cc","transactionIndex":"0x0"},
	       {"address":"0xabcdef0000000000000000000000000000000001","topics":["0x00000000000000000000000000000000000000000000000000000000000000aa"],"data":"0xff","blockNumber":"0xa","blockHash":"0x00000000000000000000000000000000000000000000000000000000000000bb","transactionHash":"0x00000000000000000000000000000000000000000000000000000000000000cc","transactionIndex":"0x0","logIndex":"0x1"}]`
	sa, err := CanonLogs(json.RawMessage(a))
	require.NoError(t, err)
	sb, err := CanonLogs(json.RawMessage(b))
	require.NoError(t, err)
	require.Equal(t, sa.Digest(), sb.Digest())
	require.False(t, sa.OrderOK, "out-of-order logs are flagged, not rejected")
	require.True(t, sb.OrderOK)

	dup, err := CanonLogs(json.RawMessage(`[` + b[1:len(b)-1] + `,` + b[1:len(b)-1] + `]`))
	require.NoError(t, err)
	require.NotEqual(t, sb.Digest(), dup.Digest(), "duplicates are kept")
}

func sampleReceipts() (types.Receipts, common.Hash) {
	blockHash := common.HexToHash("0xb1")
	var rs types.Receipts
	var cum uint64
	for i := 0; i < 3; i++ {
		cum += 21000
		r := &types.Receipt{Type: uint8(i % 3), Status: uint64(i % 2), CumulativeGasUsed: cum, GasUsed: 21000,
			TxHash: common.BigToHash(big.NewInt(int64(100 + i))), BlockHash: blockHash, BlockNumber: big.NewInt(7), TransactionIndex: uint(i)}
		if i > 0 {
			r.Logs = []*types.Log{{Address: common.HexToAddress("0x01"), Topics: []common.Hash{common.BigToHash(big.NewInt(int64(0x200 + i)))}, Data: []byte{byte(i)},
				BlockNumber: 7, BlockHash: blockHash, TxHash: r.TxHash, TxIndex: uint(i), Index: uint(i - 1)}}
		}
		r.Bloom = types.CreateBloom(r)
		rs = append(rs, r)
	}
	return rs, types.DeriveSha(rs, trie.NewStackTrie(nil))
}

func TestReceiptsRootMatchesGeth(t *testing.T) {
	rs, root := sampleReceipts()
	raw, err := json.Marshal(rs)
	require.NoError(t, err)
	canon, err := CanonReceipts(raw)
	require.NoError(t, err)
	require.Equal(t, root, ReceiptsRoot(canon))

	logs := BlockLogs(canon)
	require.Len(t, logs.Logs, 2)
	require.Equal(t, types.MergeBloom(rs), LogsBloom(logs.Logs))

	canon[1].CumulativeGasUsed = Quantity(1)
	canon[1].cumGas = 1
	require.NotEqual(t, root, ReceiptsRoot(canon))
}

func TestCheckLogsAgainstHeader(t *testing.T) {
	rs, root := sampleReceipts()
	raw, _ := json.Marshal(rs)
	canon, _ := CanonReceipts(raw)
	logs := BlockLogs(canon)
	h := &Header{Hash: common.HexToHash("0xb1"), Number: 7, LogsBloom: types.MergeBloom(rs), ReceiptsRoot: root}

	v, _ := checkLogs(logs.Logs, h)
	require.Equal(t, schema.LocalMatch, v)
	v, why := checkLogs(logs.Logs[:1], h)
	require.Equal(t, schema.LocalMismatch, v)
	require.Contains(t, why, "bloom")
	v, why = checkLogs(logs.Logs[1:], h)
	require.Equal(t, schema.LocalMismatch, v)
	require.Contains(t, why, "gap")
	v, _ = checkLogs(nil, h)
	require.Equal(t, schema.LocalMismatch, v, "an early [] is not readiness")
	v, _ = checkLogs(nil, &Header{})
	require.Equal(t, schema.LocalMatch, v)
}

func TestParseHeaderFullTransactions(t *testing.T) {
	raw := `{"hash":"0x00000000000000000000000000000000000000000000000000000000000000b1","parentHash":"0x00000000000000000000000000000000000000000000000000000000000000b0","number":"0x07","timestamp":"0x10","gasUsed":"0x0","logsBloom":"0x` + zeros(512) + `","receiptsRoot":"0x` + zeros(64) + `","transactions":[{"hash":"0x00000000000000000000000000000000000000000000000000000000000000c1"},"0x00000000000000000000000000000000000000000000000000000000000000c2"]}`
	h, err := ParseHeader(json.RawMessage(raw))
	require.NoError(t, err)
	require.Equal(t, uint64(7), h.Number)
	require.Len(t, h.TxHashes, 2)
	h, err = ParseHeader(json.RawMessage(`null`))
	require.NoError(t, err)
	require.Nil(t, h)
}

func zeros(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}

func TestHistoryCalldata(t *testing.T) {
	require.Equal(t, "0x"+zeros(60)+"1234", HistoryCalldata(0x1234))
}

func TestOutOfRangeValuesAreRejected(t *testing.T) {
	_, err := CanonReceipt(json.RawMessage(`{"type":"0x100","status":"0x1","cumulativeGasUsed":"0x1","gasUsed":"0x1","logsBloom":"0x` + zeros(512) + `","logs":[],"transactionHash":"0x` + zeros(64) + `","transactionIndex":"0x0","blockHash":"0x` + zeros(64) + `","blockNumber":"0x1"}`))
	require.ErrorContains(t, err, "exceeds one byte", "a type wider than a byte must not wrap into a valid one")

	raw := `{"hash":"0x` + zeros(64) + `","parentHash":"0x` + zeros(64) + `","number":"0x1","timestamp":"0xffffffffffffffff","logsBloom":"0x` + zeros(512) + `","receiptsRoot":"0x` + zeros(64) + `","transactions":[]}`
	_, err = ParseHeader(json.RawMessage(raw))
	require.ErrorContains(t, err, "out of range")

	ns, err := SlotStartNanos(1790344644)
	require.NoError(t, err)
	require.Equal(t, int64(1790344644)*1e9, ns)
}
