package ethrpc

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// Candidate is a normalised result ready to be judged. Canon is what gets
// stored under responses/ and hashed into Digest.
type Candidate struct {
	Canon   any
	Digest  string
	OrderOK bool
}

// Canonicalize turns a probe result into its canonical form.
func Canonicalize(probe string, raw json.RawMessage) (Candidate, error) {
	switch probe {
	case schema.ProbeStateNumber, schema.ProbeStateLatest, schema.ProbeStateHashCanonical:
		h, err := CanonHash(raw)
		if err != nil {
			return Candidate{}, err
		}
		return Candidate{Canon: h, Digest: Digest(h), OrderOK: true}, nil
	case schema.ProbeLogsNumber, schema.ProbeLogsHash:
		set, err := CanonLogs(raw)
		if err != nil {
			return Candidate{}, err
		}
		return Candidate{Canon: set.Logs, Digest: set.Digest(), OrderOK: set.OrderOK}, nil
	case schema.ProbeTransactionReceipt:
		r, err := CanonReceipt(raw)
		if err != nil {
			return Candidate{}, err
		}
		return Candidate{Canon: r, Digest: Digest(r), OrderOK: true}, nil
	case schema.ProbeBlockReceipts:
		rs, err := CanonReceipts(raw)
		if err != nil {
			return Candidate{}, err
		}
		return Candidate{Canon: rs, Digest: Digest(rs), OrderOK: true}, nil
	}
	return Candidate{}, fmt.Errorf("unknown probe %q", probe)
}

// Expectation is what the probe knows locally about a target. Fields stay
// zero until known; checks that need a missing field return LocalPending.
type Expectation struct {
	ParentHash *common.Hash
	Header     *Header
	TxHash     *common.Hash
}

// Check judges a canonical candidate against local knowledge. A local match
// is necessary, not sufficient: review verifies against the reference.
func Check(probe string, c Candidate, exp Expectation) (verdict, reason string) {
	switch probe {
	case schema.ProbeStateNumber, schema.ProbeStateLatest, schema.ProbeStateHashCanonical:
		if exp.ParentHash == nil {
			return schema.LocalPending, "parent hash unknown"
		}
		if c.Canon.(string) != HashHex(*exp.ParentHash) {
			return schema.LocalMismatch, "value is not the parent hash"
		}
		return schema.LocalMatch, ""
	case schema.ProbeLogsNumber, schema.ProbeLogsHash:
		if exp.Header == nil {
			return schema.LocalPending, "header unknown"
		}
		return checkLogs(c.Canon.([]Log), exp.Header)
	case schema.ProbeTransactionReceipt:
		if exp.Header == nil || exp.TxHash == nil {
			return schema.LocalPending, "header unknown"
		}
		r := c.Canon.(Receipt)
		if r.BlockHash != HashHex(exp.Header.Hash) {
			return schema.LocalMismatch, "receipt from a different block"
		}
		if r.TransactionHash != HashHex(*exp.TxHash) {
			return schema.LocalMismatch, "receipt for a different transaction"
		}
		if LogsBloom(r.Logs) != types.BytesToBloom(common.FromHex(r.LogsBloom)) {
			return schema.LocalMismatch, "receipt logs do not match its bloom"
		}
		return schema.LocalMatch, ""
	case schema.ProbeBlockReceipts:
		if exp.Header == nil {
			return schema.LocalPending, "header unknown"
		}
		rs := c.Canon.([]Receipt)
		if len(rs) != len(exp.Header.TxHashes) {
			return schema.LocalMismatch, fmt.Sprintf("%d receipts for %d transactions", len(rs), len(exp.Header.TxHashes))
		}
		if ReceiptsRoot(rs) != exp.Header.ReceiptsRoot {
			return schema.LocalMismatch, "receipts root mismatch"
		}
		return schema.LocalMatch, ""
	}
	return schema.LocalMismatch, "unknown probe"
}

func checkLogs(logs []Log, h *Header) (string, string) {
	if len(logs) == 0 {
		if h.LogsBloom == (types.Bloom{}) {
			return schema.LocalMatch, ""
		}
		return schema.LocalMismatch, "empty log set for a block with a non-empty bloom"
	}
	hash := HashHex(h.Hash)
	for i, l := range logs {
		if l.BlockHash != hash || l.blockNumber != h.Number {
			return schema.LocalMismatch, "log from a different block"
		}
		if l.Removed {
			return schema.LocalMismatch, "removed log"
		}
		if l.logIndex != uint64(i) {
			return schema.LocalMismatch, fmt.Sprintf("log index gap at %d", i)
		}
	}
	if LogsBloom(logs) != h.LogsBloom {
		return schema.LocalMismatch, "logs do not reproduce the header bloom"
	}
	return schema.LocalMatch, ""
}
