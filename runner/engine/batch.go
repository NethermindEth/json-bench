package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Batch is one HTTP round trip: a single request, or a JSON-RPC array of them.
//
// Every member of a batch shares one set of timings, because they share one
// round trip. That is not an approximation — each caller waited the whole batch
// for its answer, so the batch duration is the latency each of them saw. What
// it does not give is the server-side cost of an individual call, which means
// comparing methods against each other inside a batched run is not meaningful.
type Batch struct {
	Requests []Request
	Payload  []byte
}

// Size is how many JSON-RPC calls the round trip carries.
func (b Batch) Size() int { return len(b.Requests) }

// Batched reports whether this is a JSON-RPC array rather than a single call.
func (b Batch) Batched() bool { return len(b.Requests) > 1 }

// BuildBatches groups a request sequence into batches of at most size. A size
// of one or less leaves each request its own round trip, which is the
// unbatched path and keeps its payload byte-identical.
func BuildBatches(requests []Request, size int) ([]Batch, error) {
	if size <= 1 {
		out := make([]Batch, 0, len(requests))
		for _, req := range requests {
			out = append(out, Batch{Requests: []Request{req}, Payload: req.Payload})
		}
		return out, nil
	}

	out := make([]Batch, 0, (len(requests)+size-1)/size)
	for start := 0; start < len(requests); start += size {
		end := start + size
		if end > len(requests) {
			end = len(requests)
		}
		members := requests[start:end]

		payload, err := batchPayload(members)
		if err != nil {
			return nil, err
		}
		out = append(out, Batch{Requests: members, Payload: payload})
	}
	return out, nil
}

// batchPayload renders the members as a JSON-RPC array. The members' own
// payloads are spliced in verbatim rather than re-encoded, so a batched run
// sends byte-identical call objects to an unbatched one.
func batchPayload(members []Request) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, member := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		if !json.Valid(member.Payload) {
			return nil, fmt.Errorf("request %d carries a payload that is not valid JSON", member.ID)
		}
		b.Write(member.Payload)
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

// batchOutcome is what one member of a batch resolved to.
type batchOutcome struct {
	outcome  Outcome
	rpcCode  int
	respSize int
}

// classifyBatch resolves each member of a batch from one HTTP response.
//
// A batch can fail wholesale or in part, and both have to be attributed. A
// transport or HTTP failure belongs to every member. A node refusing the batch
// outright — which is what exceeding a max-batch-size limit looks like — answers
// with a single error object rather than an array, and that error belongs to
// every member too. Otherwise each member is matched to its own response by id.
func classifyBatch(batch Batch, status int, body []byte, transportErr error) []batchOutcome {
	out := make([]batchOutcome, len(batch.Requests))

	if !batch.Batched() {
		outcome, code := classify(status, body, transportErr)
		out[0] = batchOutcome{outcome: outcome, rpcCode: code, respSize: len(body)}
		return out
	}

	if transportErr != nil || status != 200 {
		outcome, code := classify(status, body, transportErr)
		for i := range out {
			out[i] = batchOutcome{outcome: outcome, rpcCode: code}
		}
		return out
	}

	trimmed := bytes.TrimSpace(body)

	// A single object where an array was asked for: the node rejected the batch
	// as a whole, so its error is every member's error.
	if len(trimmed) > 0 && trimmed[0] != '[' {
		outcome, code := classify(status, body, nil)
		if outcome == OutcomeOK || outcome == OutcomeRPCNull {
			// A lone success where an array was expected is not an answer to
			// anything in particular.
			outcome = OutcomeTruncated
			code = 0
		}
		for i := range out {
			out[i] = batchOutcome{outcome: outcome, rpcCode: code}
		}
		return out
	}

	var responses []json.RawMessage
	if err := json.Unmarshal(trimmed, &responses); err != nil {
		for i := range out {
			out[i] = batchOutcome{outcome: OutcomeTruncated}
		}
		return out
	}

	// A batch response may arrive in any order, so members are matched by id.
	byID := make(map[string]json.RawMessage, len(responses))
	for _, response := range responses {
		var envelope struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(response, &envelope); err != nil {
			continue
		}
		byID[string(bytes.TrimSpace(envelope.ID))] = response
	}

	for i, member := range batch.Requests {
		response, ok := byID[fmt.Sprint(member.ID)]
		if !ok {
			// The node answered the batch but not this call.
			out[i] = batchOutcome{outcome: OutcomeTruncated}
			continue
		}
		outcome, code := classify(status, response, nil)
		out[i] = batchOutcome{outcome: outcome, rpcCode: code, respSize: len(response)}
	}
	return out
}
