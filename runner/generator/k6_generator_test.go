package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

func readK6Config(t *testing.T, cfg *config.Config) map[string]any {
	t.Helper()
	dir := t.TempDir()
	path, err := GenerateK6Config(cfg, dir)
	if err != nil {
		t.Fatalf("GenerateK6Config: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	var generated map[string]any
	if err := json.Unmarshal(body, &generated); err != nil {
		t.Fatalf("parse generated config: %v", err)
	}
	if base := filepath.Base(path); base != K6ConfigFilename {
		t.Errorf("config written to %q, want %s", base, K6ConfigFilename)
	}
	return generated
}

func thresholdKeys(t *testing.T, generated map[string]any) map[string]struct{} {
	t.Helper()
	options, ok := generated["options"].(map[string]any)
	if !ok {
		t.Fatalf("generated config has no options: %v", generated)
	}
	thresholds, ok := options["thresholds"].(map[string]any)
	if !ok {
		t.Fatalf("generated config has no thresholds: %v", options)
	}
	keys := make(map[string]struct{}, len(thresholds))
	for k := range thresholds {
		keys[k] = struct{}{}
	}
	return keys
}

func benchCfg() *config.Config {
	return &config.Config{
		TestName: "test",
		Duration: "10s",
		RPS:      10,
		VUs:      2,
		ResolvedClients: []*types.ClientConfig{
			{Name: "geth", URL: "http://localhost:8545"},
		},
		Calls: []*config.Call{{Name: "multimethod", Method: "eth_call", Params: []interface{}{}}},
	}
}

func TestGenerateK6Config_DeclaredCallsKeyOnReqName(t *testing.T) {
	keys := thresholdKeys(t, readK6Config(t, benchCfg()))

	for _, want := range []string{
		"http_req_duration{scenario:geth,req_name:multimethod}",
		"http_reqs{scenario:geth,req_name:multimethod}",
		"http_req_failed{scenario:geth,req_name:multimethod}",
	} {
		if _, ok := keys[want]; !ok {
			t.Errorf("missing threshold %q; got %v", want, keys)
		}
	}
}

// With a calls file the declared names are just labels, so the submetrics k6 is
// asked to emit must be keyed on the rpc_method tag the script attaches to every
// request — otherwise the only submetric is a zero-count row for the declared
// name, and the per-method breakdown is unrecoverable after the run.
func TestGenerateK6Config_CallsFileKeysOnRPCMethod(t *testing.T) {
	cfg := benchCfg()
	cfg.CallsFile = "requests.csv"
	cfg.CallsFileMethods = []string{"eth_call", "eth_getLogs"}

	keys := thresholdKeys(t, readK6Config(t, cfg))

	for _, want := range []string{
		"http_req_duration{scenario:geth,rpc_method:eth_call}",
		"http_reqs{scenario:geth,rpc_method:eth_call}",
		"http_req_failed{scenario:geth,rpc_method:eth_call}",
		"http_req_duration{scenario:geth,rpc_method:eth_getLogs}",
	} {
		if _, ok := keys[want]; !ok {
			t.Errorf("missing threshold %q; got %v", want, keys)
		}
	}
	if _, ok := keys["http_req_duration{scenario:geth,req_name:multimethod}"]; ok {
		t.Error("a calls-file run should not register req_name submetrics for the declared call name")
	}
}

func TestGenerateK6Config_AsksK6ForP75AndP999(t *testing.T) {
	generated := readK6Config(t, benchCfg())
	options := generated["options"].(map[string]any)
	stats, ok := options["summaryTrendStats"].([]any)
	if !ok {
		t.Fatalf("no summaryTrendStats in %v", options)
	}
	present := make(map[string]struct{}, len(stats))
	for _, s := range stats {
		present[s.(string)] = struct{}{}
	}
	for _, want := range []string{"p(75)", "p(99.9)"} {
		if _, ok := present[want]; !ok {
			t.Errorf("summaryTrendStats missing %s, so the CSV column can never be filled: %v", want, stats)
		}
	}
}

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
