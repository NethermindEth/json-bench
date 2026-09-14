package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
)

// rpcEnvelope is the part of a JSON-RPC response the classification depends on.
// Result stays raw so an absent field and an explicit null stay distinguishable.
type rpcEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

// emptyResults are the encodings of a result that carries no data. A node that
// answers instantly with nothing must not read as a fast success.
var emptyResults = [][]byte{
	[]byte("null"),
	[]byte("[]"),
	[]byte("{}"),
	[]byte(`""`),
	[]byte("0x"),
	[]byte(`"0x"`),
}

// classify decides the outcome of one attempt. A transport failure takes
// precedence: without a complete response there is nothing else to judge.
func classify(status int, body []byte, transportErr error) (Outcome, int) {
	if transportErr != nil {
		// Headers arrived and then the body failed, which is a response cut
		// short rather than a node that never answered — in practice a stale
		// keep-alive being handed to a non-retryable POST.
		if status == http.StatusOK {
			return OutcomeTruncated, 0
		}
		return classifyTransportError(transportErr), 0
	}

	if status != http.StatusOK {
		return OutcomeHTTPError, 0
	}

	var env rpcEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		// A 200 whose body will not parse was cut short in transit. It is not a
		// node that disagrees, and it must not be counted as a success.
		return OutcomeTruncated, 0
	}

	if env.Error != nil {
		return OutcomeRPCError, env.Error.Code
	}

	if env.Result == nil {
		return OutcomeTruncated, 0
	}

	trimmed := bytes.TrimSpace(env.Result)
	for _, empty := range emptyResults {
		if bytes.EqualFold(trimmed, empty) {
			return OutcomeRPCNull, 0
		}
	}

	return OutcomeOK, 0
}

func classifyTransportError(err error) Outcome {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return OutcomeTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return OutcomeTimeout
	}
	return OutcomeTransport
}
