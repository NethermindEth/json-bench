package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		err     error
		outcome Outcome
		rpcCode int
	}{
		{
			name: "result", status: 200,
			body:    `{"jsonrpc":"2.0","id":1,"result":"0x10d4f"}`,
			outcome: OutcomeOK,
		},
		{
			name: "rpc error keeps its code", status: 200,
			body:    `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"execution reverted"}}`,
			outcome: OutcomeRPCError, rpcCode: -32000,
		},
		{
			name: "rpc error with a data payload", status: 200,
			body:    `{"jsonrpc":"2.0","id":1,"error":{"code":3,"message":"reverted","data":"0x08c3"}}`,
			outcome: OutcomeRPCError, rpcCode: 3,
		},
		{
			name: "explicit null result", status: 200,
			body:    `{"jsonrpc":"2.0","id":1,"result":null}`,
			outcome: OutcomeRPCNull,
		},
		{
			name: "empty log array", status: 200,
			body:    `{"jsonrpc":"2.0","id":1,"result":[]}`,
			outcome: OutcomeRPCNull,
		},
		{
			name: "empty hex", status: 200,
			body:    `{"jsonrpc":"2.0","id":1,"result":"0x"}`,
			outcome: OutcomeRPCNull,
		},
		{
			name: "non-empty array is a result", status: 200,
			body:    `{"jsonrpc":"2.0","id":1,"result":[{"address":"0x0"}]}`,
			outcome: OutcomeOK,
		},
		{
			name: "missing result and error", status: 200,
			body:    `{"jsonrpc":"2.0","id":1}`,
			outcome: OutcomeTruncated,
		},
		{
			name: "body cut short", status: 200,
			body:    `{"jsonrpc":"2.0","id":1,"result":"0x10`,
			outcome: OutcomeTruncated,
		},
		{
			name: "server error", status: 500,
			body:    `{"error":"boom"}`,
			outcome: OutcomeHTTPError,
		},
		{
			name: "rate limited", status: http.StatusTooManyRequests,
			body:    `too many`,
			outcome: OutcomeHTTPError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, code := classify(tc.status, []byte(tc.body), tc.err)
			assert.Equal(t, tc.outcome, outcome)
			assert.Equal(t, tc.rpcCode, code)
		})
	}
}

func TestClassifyTransportErrors(t *testing.T) {
	cases := map[string]struct {
		err     error
		outcome Outcome
	}{
		"deadline":           {err: context.DeadlineExceeded, outcome: OutcomeTimeout},
		"cancelled":          {err: context.Canceled, outcome: OutcomeTimeout},
		"wrapped deadline":   {err: fmt.Errorf("Post %q: %w", "http://x", context.DeadlineExceeded), outcome: OutcomeTimeout},
		"net timeout":        {err: &net.DNSError{IsTimeout: true}, outcome: OutcomeTimeout},
		"connection refused": {err: errors.New("connect: connection refused"), outcome: OutcomeTransport},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			outcome, code := classify(0, nil, tc.err)
			assert.Equal(t, tc.outcome, outcome)
			assert.Zero(t, code)
		})
	}
}

// A response whose headers arrived and whose body then failed was cut short,
// not lost: it is the stale-keep-alive case, and it must never read as a fast
// success.
func TestClassifyTreatsAFailedBodyReadAsTruncated(t *testing.T) {
	outcome, _ := classify(200, []byte(`{"jsonrpc":"2.0","result"`), errors.New("unexpected EOF"))
	assert.Equal(t, OutcomeTruncated, outcome)
}

// With no response at all there is no status to reason from, so the failure is
// reported as what it was.
func TestClassifyReportsAFailureWithNoResponse(t *testing.T) {
	outcome, _ := classify(0, nil, errors.New("connect: connection refused"))
	assert.Equal(t, OutcomeTransport, outcome)
}

func TestOutcomeErrorAccounting(t *testing.T) {
	assert.False(t, OutcomeOK.IsError())
	assert.False(t, OutcomeRPCNull.IsError(), "a call that returned nothing still succeeded")

	for _, outcome := range []Outcome{
		OutcomeRPCError, OutcomeHTTPError, OutcomeTruncated, OutcomeTimeout, OutcomeTransport,
	} {
		assert.True(t, outcome.IsError(), "%s must count against the error rate", outcome)
	}
}
