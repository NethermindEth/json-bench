package stubnode

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const maxRequestBytes = 8 << 20

func readAll(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("stubnode: reading request: %w", err)
	}
	if len(body) > maxRequestBytes {
		return nil, fmt.Errorf("stubnode: request exceeds %d bytes", maxRequestBytes)
	}
	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// serveTruncated announces a length it does not deliver and closes the
// connection mid-body, which is how a real node's stale keep-alive presents: a
// 200 whose body will not parse.
func serveTruncated(w http.ResponseWriter, id json.RawMessage) {
	full, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": rawOrNull(id), "result": "0xdeadbeef",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprint(len(full)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(full[:len(full)/2])

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	panic(http.ErrAbortHandler)
}
