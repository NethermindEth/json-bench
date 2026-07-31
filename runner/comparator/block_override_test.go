package comparator

import (
	"reflect"
	"testing"
)

func TestApplyBlockOverride(t *testing.T) {
	block := "0x1406f40"
	tests := []struct {
		name   string
		method string
		params []interface{}
		want   []interface{}
	}{
		{
			name:   "eth_call appends block when omitted",
			method: "eth_call",
			params: []interface{}{map[string]interface{}{"to": "0xabc"}},
			want:   []interface{}{map[string]interface{}{"to": "0xabc"}, block},
		},
		{
			name:   "eth_call rewrites latest tag",
			method: "eth_call",
			params: []interface{}{map[string]interface{}{"to": "0xabc"}, "latest"},
			want:   []interface{}{map[string]interface{}{"to": "0xabc"}, block},
		},
		{
			name:   "eth_call leaves explicit block untouched",
			method: "eth_call",
			params: []interface{}{map[string]interface{}{"to": "0xabc"}, "0x10"},
			want:   []interface{}{map[string]interface{}{"to": "0xabc"}, "0x10"},
		},
		{
			name:   "eth_getStorageAt appends at index 2",
			method: "eth_getStorageAt",
			params: []interface{}{"0xaddr", "0x0"},
			want:   []interface{}{"0xaddr", "0x0", block},
		},
		{
			name:   "eth_getBlockByNumber rewrites index 0 pending",
			method: "eth_getBlockByNumber",
			params: []interface{}{"pending", true},
			want:   []interface{}{block, true},
		},
		{
			name:   "unknown method untouched",
			method: "eth_getBlockByHash",
			params: []interface{}{"0xhash", true},
			want:   []interface{}{"0xhash", true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applyBlockOverride(tc.method, tc.params, block)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyBlockOverrideGetLogs(t *testing.T) {
	block := "0x1406f40"
	params := []interface{}{map[string]interface{}{"fromBlock": "latest", "address": "0xabc"}}
	got := applyBlockOverride("eth_getLogs", params, block)
	filter := got[0].(map[string]interface{})
	if filter["fromBlock"] != block || filter["toBlock"] != block {
		t.Errorf("expected fromBlock/toBlock pinned to %s, got %v", block, filter)
	}
	if filter["address"] != "0xabc" {
		t.Errorf("address should be preserved, got %v", filter["address"])
	}
	// The original params must not be mutated.
	if params[0].(map[string]interface{})["fromBlock"] != "latest" {
		t.Error("input params were mutated")
	}
}

func TestPinnedBlock(t *testing.T) {
	tests := []struct {
		method string
		params []interface{}
		want   uint64
		ok     bool
	}{
		{"eth_getBlockByNumber", []interface{}{"0x10", true}, 16, true},
		{"eth_getBlockByNumber", []interface{}{"latest", true}, 0, false},
		{"eth_call", []interface{}{map[string]interface{}{}, "0xff"}, 255, true},
		{"eth_getLogs", []interface{}{map[string]interface{}{"toBlock": "0x20"}}, 32, true},
		{"eth_getBlockByHash", []interface{}{"0xhash", true}, 0, false},

		// eth_getBlockReceipts takes a block number or a block hash in the same
		// position. Truncating a hash to 64 bits yields a height above any real
		// head, which made --skip-above-head silently drop every by-hash call.
		{"eth_getBlockReceipts", []interface{}{"0x112a880"}, 18000000, true},
		{"eth_getBlockReceipts", []interface{}{"0x95b198e154acbfc64109dfd22d8224fe927fd8dfdedfae01587674482ba4baf3"}, 0, false},
		// A hash whose low 64 bits are small would still be a plausible height.
		{"eth_getBlockReceipts", []interface{}{"0x95b198e154acbfc64109dfd22d8224fe927fd8dfdedfae0158767448200000ff"}, 0, false},
		// Oversized but shorter than a hash: not representable, so not pinned.
		{"eth_getBlockByNumber", []interface{}{"0x1ffffffffffffffff", true}, 0, false},
	}
	for _, tc := range tests {
		got, ok := pinnedBlock(tc.method, tc.params)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("pinnedBlock(%s,%v)=(%d,%v) want (%d,%v)", tc.method, tc.params, got, ok, tc.want, tc.ok)
		}
	}
}
