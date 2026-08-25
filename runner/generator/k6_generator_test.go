package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jsonrpc-bench/runner/config"
)

func seededRequests(t *testing.T, seed int64) string {
	t.Helper()
	calls := make([]config.RPCCall, 0, 50)
	for i := 0; i < 50; i++ {
		calls = append(calls, config.RPCCall{Method: "eth_call", Params: []interface{}{i}})
	}
	cfg := &config.Config{
		Duration: "2s",
		RPS:      100,
		Seed:     seed,
		Calls: []*config.Call{
			{Name: "a", Weight: 3, Calls: calls},
			{Name: "b", Weight: 1, Method: "eth_blockNumber", Params: []interface{}{}},
		},
	}
	path, err := GenerateK6Requests(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSeededRequestsAreReproducible(t *testing.T) {
	if seededRequests(t, 7) != seededRequests(t, 7) {
		t.Fatal("same seed produced different request sequences")
	}
	if seededRequests(t, 7) == seededRequests(t, 8) {
		t.Fatal("different seeds produced the same request sequence")
	}
}
