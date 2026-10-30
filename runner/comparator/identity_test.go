package comparator

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// paramsFromJSON decodes a params literal the way the corpus loader does, so a
// vector exercises the same types a real corpus produces — json.Number for
// every number, not float64.
func paramsFromJSON(t *testing.T, literal string) []interface{} {
	t.Helper()
	var params []interface{}
	dec := json.NewDecoder(strings.NewReader(literal))
	dec.UseNumber()
	if err := dec.Decode(&params); err != nil {
		t.Fatalf("decode %s: %v", literal, err)
	}
	return params
}

// The vectors below were produced by rpc-corpus-tools' own implementation
// (src/rpc_corpus/canonical.py, request_id) at commit 0bc6e31 and are the
// contract between the two sides. The first fourteen are real lines from that
// project's committed control corpus replay/corpora/genesis-hoodi-alloc — one
// per method — checked against the request ids in its counts.json, so they are
// not a restatement of this implementation but of the corpus it must read.
//
// A change here that is not also a change there is a bug: an identity that does
// not match cannot reconcile a run against the manifest that produced it.
var identityVectors = []struct {
	name      string
	method    string
	params    string
	canonical string
	id        string
}{
	{
		"control corpus: eth_call",
		"eth_call",
		`[{"to":"0x00000000000000000000000000000000000c0de2","data":"0x"},"0x0"]`,
		`["eth_call",[{"data":"0x","to":"0x00000000000000000000000000000000000c0de2"},"0x0"]]`,
		"2b06cb0f01a5b3c8c0e6670ba25c1282e0c9903fd1e982eabee455904b349dd8",
	},
	{
		"control corpus: eth_chainId, empty params",
		"eth_chainId",
		`[]`,
		`["eth_chainId",[]]`,
		"526abcdf8df91272d36a5986d5bcc40cc15cfdf3cd2005fa3f012e34ffdb96b7",
	},
	{
		"control corpus: eth_estimateGas",
		"eth_estimateGas",
		`[{"to":"0x00000000000000000000000000000000000c0de2","data":"0x"},"0x0"]`,
		`["eth_estimateGas",[{"data":"0x","to":"0x00000000000000000000000000000000000c0de2"},"0x0"]]`,
		"2444e841848fdf70a8c8d94081f05bbde5b5aa50dce8e5708da5c82606a8e278",
	},
	{
		"control corpus: eth_getBalance",
		"eth_getBalance",
		`["0x00000000000000000000000000000000000c0de1","0x0"]`,
		`["eth_getBalance",["0x00000000000000000000000000000000000c0de1","0x0"]]`,
		"3e30ceb5e646cef6fd8a68a552d6bfa0d1fac05f90d2d16fc122690f6b6bb317",
	},
	{
		"control corpus: eth_getBlockByHash, boolean param",
		"eth_getBlockByHash",
		`["0x16bc1fb31ab717aa62897721dac81c2fd491a9323fc72700e7b7d58d78bb47ae",false]`,
		`["eth_getBlockByHash",["0x16bc1fb31ab717aa62897721dac81c2fd491a9323fc72700e7b7d58d78bb47ae",false]]`,
		"c5a21f4a9ce85042df90ac2dcac035445d238e90c15416a9a1a1b3329ca49138",
	},
	{
		"control corpus: eth_getBlockByNumber",
		"eth_getBlockByNumber",
		`["0x0",false]`,
		`["eth_getBlockByNumber",["0x0",false]]`,
		"94543e0d254d7075c48e5e8c766da447f995588a6f46d6d86cf13c44542daa46",
	},
	{
		"control corpus: eth_getBlockReceipts",
		"eth_getBlockReceipts",
		`["0x0"]`,
		`["eth_getBlockReceipts",["0x0"]]`,
		"498c5c5bd6159533fbe59158095c6dd30f2c80fbbe6708b5b2544624ce2c027b",
	},
	{
		"control corpus: eth_getBlockTransactionCountByNumber",
		"eth_getBlockTransactionCountByNumber",
		`["0x0"]`,
		`["eth_getBlockTransactionCountByNumber",["0x0"]]`,
		"851d7d5e9267f8b6f8959d28cf9088a4a39bb0bf7ee57c94c598c785b0582414",
	},
	{
		"control corpus: eth_getCode",
		"eth_getCode",
		`["0x00000000000000000000000000000000000c0de2","0x0"]`,
		`["eth_getCode",["0x00000000000000000000000000000000000c0de2","0x0"]]`,
		"00f81a37232793129ef106c70e5b8cd8db41fe807505258bdc056f9d4b98fe54",
	},
	{
		"control corpus: eth_getLogs",
		"eth_getLogs",
		`[{"fromBlock":"0x0","toBlock":"0x0"}]`,
		`["eth_getLogs",[{"fromBlock":"0x0","toBlock":"0x0"}]]`,
		"051e18ea585d9d25f2a464fb6dd1e80d8d6f8046bbc89f3cfeacc16a532f7644",
	},
	{
		"control corpus: eth_getStorageAt",
		"eth_getStorageAt",
		`["0x00000000000000000000000000000000000c0de1","0x1","0x0"]`,
		`["eth_getStorageAt",["0x00000000000000000000000000000000000c0de1","0x1","0x0"]]`,
		"04b3a7a334cc0eed172d81f6ca2bf4710701de6294d5574f9e8c72bd23c7c2f9",
	},
	{
		"control corpus: eth_getTransactionByBlockHashAndIndex",
		"eth_getTransactionByBlockHashAndIndex",
		`["0x16bc1fb31ab717aa62897721dac81c2fd491a9323fc72700e7b7d58d78bb47ae","0x0"]`,
		`["eth_getTransactionByBlockHashAndIndex",["0x16bc1fb31ab717aa62897721dac81c2fd491a9323fc72700e7b7d58d78bb47ae","0x0"]]`,
		"bec70c225bbf2a50b643f3ad8afc07125118dcf0130ff0b0fb522e436a4316ea",
	},
	{
		"control corpus: eth_getTransactionCount",
		"eth_getTransactionCount",
		`["0x00000000000000000000000000000000000ba1a1","0x0"]`,
		`["eth_getTransactionCount",["0x00000000000000000000000000000000000ba1a1","0x0"]]`,
		"644637c3b53fe620b5aa27db40a1d79e9ed0ccbf1e3609bec8a1a3db77423b1f",
	},
	{
		"control corpus: eth_getUncleCountByBlockHash",
		"eth_getUncleCountByBlockHash",
		`["0x16bc1fb31ab717aa62897721dac81c2fd491a9323fc72700e7b7d58d78bb47ae"]`,
		`["eth_getUncleCountByBlockHash",["0x16bc1fb31ab717aa62897721dac81c2fd491a9323fc72700e7b7d58d78bb47ae"]]`,
		"e03620fdbe0bf33107aac984e8982d34a0cf548c3c15e425d7e5f345c9282426",
	},
	{
		// Object keys are sorted, and sorted by byte, so "A" precedes "a".
		"object keys are sorted",
		"x_keys",
		`[{"b":1,"a":2,"A":3}]`,
		`["x_keys",[{"A":3,"a":2,"b":1}]]`,
		"d67ea029f40fb645486fdb7a5eb7b363e2cc529f29d6efc17eeda3c91e196afd",
	},
	{
		// ensure_ascii=False on the producer side: UTF-8 stays UTF-8.
		"non-ASCII strings are not escaped",
		"x_unicode",
		`["café","日本"]`,
		`["x_unicode",["café","日本"]]`,
		"3b2e6572d9fc9323d599524652d1150ab81752e0220f0521f1068e0a4f0173e2",
	},
	{
		// The trap this encoder exists for: Go escapes <, > and & by default.
		"angle brackets and ampersands are not HTML-escaped",
		"x_html",
		`["<a>&b</a>","a\"b"]`,
		`["x_html",["<a>&b</a>","a\"b"]]`,
		"5e3f014647b93237cd3100d077d80bc5ff68fd0be895410c89388683a391f7b0",
	},
	{
		// Through float64 this would canonicalize as 9007199254740992.
		"an integer above 2^53 keeps its digits",
		"x_bigint",
		`[9007199254740993]`,
		`["x_bigint",[9007199254740993]]`,
		"27ad6cf870d75ee1a4ae97e037a7d0681c649b81752501000eb6e110fac6b781",
	},
	{
		"a fractional number keeps its digits",
		"x_float",
		`[1.5]`,
		`["x_float",[1.5]]`,
		"a1b21675bc81654ccdbf42ac6c06ff5337c837524c5c15b5c410275c7541eaf2",
	},
	{
		"nested objects, nulls and booleans",
		"x_nested",
		`[{"z":[1,{"y":"0x1"}],"a":null},true,false,null]`,
		`["x_nested",[{"a":null,"z":[1,{"y":"0x1"}]},true,false,null]]`,
		"6c9607d5d5e5f49be275e1e98a8acf7a412fe59d79ed6cb857fd3ea84371f6a7",
	},
}

func TestRequestIDMatchesTheProducer(t *testing.T) {
	for _, vector := range identityVectors {
		t.Run(vector.name, func(t *testing.T) {
			params := paramsFromJSON(t, vector.params)

			canonical, err := canonicalRequest(vector.method, params)
			if err != nil {
				t.Fatalf("canonicalRequest: %v", err)
			}
			if string(canonical) != vector.canonical {
				t.Errorf("canonical form:\n got %s\nwant %s", canonical, vector.canonical)
			}
			if bytes.HasSuffix(canonical, []byte("\n")) {
				t.Error("the canonical string must not end in a newline")
			}

			id, err := RequestID(vector.method, params)
			if err != nil {
				t.Fatalf("RequestID: %v", err)
			}
			if id != vector.id {
				t.Errorf("request id:\n got %s\nwant %s", id, vector.id)
			}
		})
	}
}

// Absent params and empty params are one request: the loader substitutes an
// empty slice for a nil params, and so does the producer, because the node
// cannot tell the two apart either.
func TestRequestIDNormalizesAbsentParams(t *testing.T) {
	withNil, err := RequestID("eth_chainId", nil)
	if err != nil {
		t.Fatalf("RequestID(nil): %v", err)
	}
	withEmpty, err := RequestID("eth_chainId", []interface{}{})
	if err != nil {
		t.Fatalf("RequestID([]): %v", err)
	}
	if withNil != withEmpty {
		t.Errorf("absent and empty params must share an identity: %s vs %s", withNil, withEmpty)
	}
	if withNil != "526abcdf8df91272d36a5986d5bcc40cc15cfdf3cd2005fa3f012e34ffdb96b7" {
		t.Errorf("identity of eth_chainId with no params = %s", withNil)
	}
}

// Hex spelling and case are preserved, so 0xABC, 0xabc and 0x0abc are three
// requests rather than one. This is the producer's rule and the reason the
// comparator must not normalize params before identifying them.
func TestRequestIDPreservesHexSpelling(t *testing.T) {
	seen := map[string]string{}
	for _, spelling := range []string{"0xABC", "0xabc", "0x0abc"} {
		id, err := RequestID("eth_getBalance", []interface{}{spelling})
		if err != nil {
			t.Fatalf("RequestID(%s): %v", spelling, err)
		}
		if other, clash := seen[id]; clash {
			t.Errorf("%s and %s collided on %s", spelling, other, id)
		}
		seen[id] = spelling
	}
}

// The method name is the first element of an array, so it can never be
// confused with a param value.
func TestRequestIDSeparatesMethodFromParams(t *testing.T) {
	a, err := RequestID("eth_call", []interface{}{"x"})
	if err != nil {
		t.Fatalf("RequestID: %v", err)
	}
	b, err := RequestID("eth_call\",\"x", nil)
	if err != nil {
		t.Fatalf("RequestID: %v", err)
	}
	if a == b {
		t.Error("a method name must not be confusable with a param value")
	}
}

// A value encoding/json refuses is reported rather than turned into an empty
// identity behind the caller's back.
func TestRequestIDRefusesNonJSONValues(t *testing.T) {
	if _, err := RequestID("x", []interface{}{make(chan int)}); err == nil {
		t.Error("expected an error for a value that is not representable as JSON")
	}
	id, msg := requestIDOf("x", []interface{}{make(chan int)})
	if id != "" || msg == "" {
		t.Errorf("requestIDOf = (%q, %q), want an empty id and a reason", id, msg)
	}
}
