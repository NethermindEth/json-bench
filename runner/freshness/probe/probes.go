package probe

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/jsonrpc-bench/runner/freshness/ethrpc"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// callerAddress and callGas pin the eth_call options every client receives,
// so client-specific defaults cannot change what is executed. The zero
// address is an ordinary account, not the EIP-2935 system caller.
const (
	callerAddress = "0x0000000000000000000000000000000000000000"
	callGas       = "0x186a0"
)

// hashAddressed probes need the block hash, which is only known once the
// probe's own node has served the header; they are never pre-armed.
func hashAddressed(probe string) bool {
	switch probe {
	case schema.ProbeStateHashCanonical, schema.ProbeLogsHash, schema.ProbeTransactionReceipt, schema.ProbeBlockReceipts:
		return true
	}
	return false
}

// stablePolls reports whether a probe's local check is weak enough that it
// must see the same matching answer several times before stopping.
func needsStablePolls(probe string) bool {
	switch probe {
	case schema.ProbeLogsNumber, schema.ProbeLogsHash, schema.ProbeTransactionReceipt:
		return true
	}
	return false
}

func historyCall(parent uint64) map[string]any {
	return map[string]any{
		"from": callerAddress,
		"to":   ethrpc.HistoryContract,
		"gas":  callGas,
		"data": ethrpc.HistoryCalldata(parent),
	}
}

// request builds the JSON-RPC call for probe on block number n. hash and tx
// are only needed by hash-addressed probes.
func request(probe string, n uint64, hash common.Hash, tx *common.Hash) (string, []any) {
	switch probe {
	case schema.ProbeStateNumber:
		return "eth_call", []any{historyCall(n - 1), ethrpc.Quantity(n)}
	case schema.ProbeStateLatest:
		return "eth_call", []any{historyCall(n - 1), "latest"}
	case schema.ProbeStateHashCanonical:
		return "eth_call", []any{historyCall(n - 1), map[string]any{"blockHash": ethrpc.HashHex(hash), "requireCanonical": true}}
	case schema.ProbeLogsNumber:
		q := ethrpc.Quantity(n)
		return "eth_getLogs", []any{map[string]any{"fromBlock": q, "toBlock": q}}
	case schema.ProbeLogsHash:
		return "eth_getLogs", []any{map[string]any{"blockHash": ethrpc.HashHex(hash)}}
	case schema.ProbeTransactionReceipt:
		return "eth_getTransactionReceipt", []any{ethrpc.HashHex(*tx)}
	case schema.ProbeBlockReceipts:
		return "eth_getBlockReceipts", []any{ethrpc.HashHex(hash)}
	}
	panic("unknown probe " + probe)
}

// chainSlotPresets covers networks whose slot duration is well known, used
// when neither the config nor a beacon endpoint provides it.
var chainSlotPresets = map[uint64]uint64{
	1:        12,
	11155111: 12,
	17000:    12,
	560048:   12,
	100:      5,
	10200:    5,
}
