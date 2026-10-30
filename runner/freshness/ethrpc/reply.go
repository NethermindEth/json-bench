package ethrpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jsonrpc-bench/runner/freshness/schema"
)

// Reply is a Response reduced to its outcome class. Result is set only for
// ClassResult and ClassNull.
type Reply struct {
	Class      string
	Result     json.RawMessage
	Code       *int
	Message    string
	HTTPStatus int
	Bytes      int
	Err        string
}

func (r Reply) AsError() error {
	switch {
	case r.Err != "":
		return fmt.Errorf("%s: %s", r.Class, r.Err)
	case r.Code != nil:
		return fmt.Errorf("rpc error %d: %s", *r.Code, r.Message)
	default:
		return fmt.Errorf("%s (http %d)", r.Class, r.HTTPStatus)
	}
}

// IsEmptyArray reports a `[]` result, whitespace-insensitive.
func (r Reply) IsEmptyArray() bool {
	return r.Class == schema.ClassResult && bytes.Equal(bytes.Join(bytes.Fields(r.Result), nil), []byte("[]"))
}

type envelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    json.RawMessage `json:"code"`
		Message json.RawMessage `json:"message"`
	} `json:"error"`
}

// Parse classifies a Response. It tolerates error codes sent as strings,
// non-string messages and error bodies returned with non-200 statuses.
func Parse(resp Response) Reply {
	r := Reply{HTTPStatus: resp.HTTPStatus, Bytes: len(resp.Body)}
	if resp.Err != nil {
		r.Err = truncate(resp.Err.Error(), 300)
		if resp.Timeout {
			r.Class = schema.ClassTimeout
		} else {
			r.Class = schema.ClassTransport
		}
		return r
	}
	var env envelope
	if err := json.Unmarshal(resp.Body, &env); err != nil {
		if resp.HTTPStatus != 200 {
			r.Class = schema.ClassHTTPError
			r.Err = truncate(string(resp.Body), 300)
		} else {
			r.Class = schema.ClassBadBody
			r.Err = truncate(err.Error(), 300)
		}
		return r
	}
	switch {
	case env.Error != nil:
		r.Class = schema.ClassRPCError
		r.Code = parseCode(env.Error.Code)
		r.Message = truncate(parseMessage(env.Error.Message), 500)
	case env.Result == nil:
		if resp.HTTPStatus != 200 {
			r.Class = schema.ClassHTTPError
		} else {
			r.Class = schema.ClassBadBody
			r.Err = "response has neither result nor error"
		}
	case string(bytes.TrimSpace(env.Result)) == "null":
		r.Class = schema.ClassNull
		r.Result = env.Result
	default:
		r.Class = schema.ClassResult
		r.Result = env.Result
	}
	return r
}

func parseCode(raw json.RawMessage) *int {
	if len(raw) == 0 {
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		if v, err := strconv.Atoi(n.String()); err == nil {
			return &v
		}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return &v
		}
	}
	return nil
}

func parseMessage(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

var (
	hexRun   = regexp.MustCompile(`0x[0-9a-f]*`)
	digitRun = regexp.MustCompile(`[0-9]+`)
)

// NormalizeMessage strips the parts of an error message that vary per call
// (block numbers, hashes) so the same condition yields the same text.
func NormalizeMessage(msg string) string {
	m := strings.ToLower(msg)
	m = hexRun.ReplaceAllString(m, "#")
	m = digitRun.ReplaceAllString(m, "#")
	return strings.Join(strings.Fields(m), " ")
}

// Signature derives the not-ready signature from a reply known to concern a
// block the node cannot have. It returns nil when the reply carries data.
func Signature(r Reply) *schema.NotReadySignature {
	sig := &schema.NotReadySignature{HTTPStatus: r.HTTPStatus}
	switch {
	case r.Class == schema.ClassRPCError:
		sig.Kind = schema.SigError
		sig.Code = r.Code
		sig.Message = NormalizeMessage(r.Message)
		sig.RawMessage = r.Message
	case r.Class == schema.ClassNull:
		sig.Kind = schema.SigNull
	case r.IsEmptyArray():
		sig.Kind = schema.SigEmptyArray
	case r.Class == schema.ClassResult && isZeroData(r.Result):
		sig.Kind = schema.SigZeroResult
	default:
		return nil
	}
	return sig
}

// notReadyPhrases catch "block unknown" errors whose exact form was not seen
// at preflight (e.g. a different code path for an in-flight block). A match on
// these is recorded as weak.
var notReadyPhrases = []string{
	"header not found", "unknown block", "block not found", "block does not exist",
	"no such block", "could not find block", "cannot find block", "not yet available",
	"beyond current head", "block is not available", "resource not found",
	"invalid block number", "requested block", "future block", "header for hash not found",
	"missing block",
}

// NotReady reports whether a reply is an explicit "this block is not known
// yet" answer. strong is true for an exact preflight-signature match; weak
// matches only a known phrase or the same error code.
func NotReady(sig *schema.NotReadySignature, r Reply) (notReady, weak bool) {
	if sig != nil {
		switch sig.Kind {
		case schema.SigError:
			if r.Class == schema.ClassRPCError && codesEqual(sig.Code, r.Code) && sig.Message == NormalizeMessage(r.Message) {
				return true, false
			}
		case schema.SigNull:
			if r.Class == schema.ClassNull {
				return true, false
			}
		case schema.SigEmptyArray:
			if r.IsEmptyArray() {
				return true, false
			}
		case schema.SigZeroResult:
			if r.Class == schema.ClassResult && isZeroData(r.Result) {
				return true, false
			}
		}
	}
	if r.Class != schema.ClassRPCError {
		return false, false
	}
	msg := NormalizeMessage(r.Message)
	if strings.Contains(msg, "method not found") || strings.Contains(msg, "not supported") {
		return false, false
	}
	for _, p := range notReadyPhrases {
		if strings.Contains(msg, p) {
			return true, true
		}
	}
	if sig != nil && sig.Kind == schema.SigError && sig.Code != nil && codesEqual(sig.Code, r.Code) {
		return true, true
	}
	return false, false
}

func codesEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func isZeroData(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	return strings.Trim(s, "0") == ""
}
