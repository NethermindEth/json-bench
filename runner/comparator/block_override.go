package comparator

import "strings"

// blockArgIndex maps a method to the positional index of its block argument.
// eth_getLogs is handled separately because its block lives in the filter
// object rather than at a fixed position.
var blockArgIndex = map[string]int{
	"eth_call":                             1,
	"eth_estimateGas":                      1,
	"eth_getCode":                          1,
	"eth_getBalance":                       1,
	"eth_getTransactionCount":              1,
	"eth_getStorageAt":                     2,
	"eth_getBlockByNumber":                 0,
	"eth_getBlockReceipts":                 0,
	"eth_getBlockTransactionCountByNumber": 0,
	"eth_feeHistory":                       1, // newestBlock
}

// applyBlockOverride rewrites latest/pending block tags to a static block and
// appends a block argument to calls that omit one, so archive nodes at
// different heads can be compared deterministically. The input params are not
// mutated. Methods without a known block argument are returned unchanged.
func applyBlockOverride(method string, params []interface{}, block string) []interface{} {
	if block == "" {
		return params
	}
	if method == "eth_getLogs" {
		return applyBlockOverrideGetLogs(params, block)
	}
	idx, ok := blockArgIndex[method]
	if !ok {
		return params
	}
	return setBlockArg(params, idx, block)
}

// setBlockArg sets the block argument at idx to block when it is missing or a
// rewritable tag, padding with nil if the slice is short.
func setBlockArg(params []interface{}, idx int, block string) []interface{} {
	out := append([]interface{}{}, params...)
	for len(out) <= idx {
		out = append(out, nil)
	}
	if out[idx] == nil || isRewritableTag(out[idx]) {
		out[idx] = block
	}
	return out
}

// applyBlockOverrideGetLogs pins the filter's fromBlock/toBlock to block when
// they are missing or a rewritable tag.
func applyBlockOverrideGetLogs(params []interface{}, block string) []interface{} {
	if len(params) == 0 {
		return params
	}
	filter, ok := params[0].(map[string]interface{})
	if !ok {
		return params
	}
	newFilter := make(map[string]interface{}, len(filter)+2)
	for k, v := range filter {
		newFilter[k] = v
	}
	for _, key := range []string{"fromBlock", "toBlock"} {
		if v, present := newFilter[key]; !present || isRewritableTag(v) {
			newFilter[key] = block
		}
	}
	out := append([]interface{}{}, params...)
	out[0] = newFilter
	return out
}

// pinnedBlock returns the numeric block a call is pinned to, when it can be
// determined from the params. Tag-addressed (latest/pending/earliest) and
// hash-addressed calls return ok=false.
func pinnedBlock(method string, params []interface{}) (uint64, bool) {
	if method == "eth_getLogs" {
		if len(params) > 0 {
			if f, ok := params[0].(map[string]interface{}); ok {
				return hexBlock(f["toBlock"])
			}
		}
		return 0, false
	}
	idx, ok := blockArgIndex[method]
	if !ok || idx >= len(params) {
		return 0, false
	}
	return hexBlock(params[idx])
}

func hexBlock(v interface{}) (uint64, bool) {
	s, ok := v.(string)
	if !ok || !strings.HasPrefix(s, "0x") {
		return 0, false
	}
	b, ok := parseHexBig(s)
	if !ok {
		return 0, false
	}
	return b.Uint64(), true
}

func isRewritableTag(v interface{}) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	return s == "latest" || s == "pending"
}
