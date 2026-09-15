package comparator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// RequestID is the stable identity of one JSON-RPC request: the lowercase hex
// SHA-256 of its canonical request string. It is what lets a corpus line, a
// comparison result and an omission list refer to the same request without
// exchanging file paths or array positions.
//
// The encoding is fixed by the rpc-corpus-tools contract (CONTRACT.md §4) and
// this function is a byte-exact port of its producer-side implementation
// (src/rpc_corpus/canonical.py:95-134 at rpc-corpus-tools 0bc6e31). The two
// must agree digit for digit or nothing downstream can be reconciled, so
// identity_test.go checks this implementation against vectors that Python
// produced.
func RequestID(method string, params []interface{}) (string, error) {
	canonical, err := canonicalRequest(method, params)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalRequest renders the two-element [method, params] array that
// RequestID hashes.
//
// The array form means a method name can never be confused with a param value.
// Object keys are sorted (encoding/json sorts map keys, which for UTF-8 is the
// same order Python's sort_keys gives), separators are compact, and absent or
// null params normalize to [] — the same substitution LoadCorpusConfig makes,
// so a capture that omitted params and one that sent [] are one request.
//
// SetEscapeHTML(false) is not cosmetic: Go escapes <, > and & by default and
// Python's ensure_ascii=False does not, so leaving it on would make every
// request whose params contain one of those three bytes hash differently on the
// two sides. Non-finite floats are refused by encoding/json, which is what
// allow_nan=False does on the producer side.
//
// One known divergence, out of reach of any RPC corpus: Go always escapes
// U+2028/U+2029 inside strings and Python with ensure_ascii=False does not.
func canonicalRequest(method string, params []interface{}) ([]byte, error) {
	normalized := params
	if normalized == nil {
		normalized = []interface{}{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode([2]interface{}{method, normalized}); err != nil {
		return nil, err
	}
	// Encode appends a newline; the canonical string does not have one.
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
