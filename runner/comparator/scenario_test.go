package comparator

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jsonrpc-bench/runner/types"
)

const filterScenarioLine = `{"scenario":"log_filter","steps":[` +
	`{"method":"eth_newFilter","params":[{"address":"0xabc","fromBlock":"0x10","toBlock":"0x1f"}]},` +
	`{"method":"eth_getFilterLogs","params":[{"$ref":0}]},` +
	`{"method":"eth_uninstallFilter","params":[{"$ref":0}]},` +
	`{"method":"eth_getFilterChanges","params":[{"$ref":0}]}]}`

const blockFilterScenarioLine = `{"scenario":"block_filter","steps":[` +
	`{"method":"eth_newBlockFilter","params":[]},` +
	`{"method":"eth_getFilterChanges","params":[{"$ref":0}]}]}`

// The identities below were produced by rpc-corpus-tools (canonical.py,
// scenario_id and scenario_step_id) for the two lines above. The scenario
// identity hashes ["scenario", [{method, params}, ...]]; a step's hashes
// [method, [scenario_id, index, params]].
const (
	filterScenarioID       = "aa8c880dea4359e4ce1b2ffd1a36be4181eb1d740f0130e5cc3b1b4c208b5cc5"
	blockFilterScenarioID  = "3ecab8d2fcaad58437fea51c21635cb43dcd163fcdd2399732895162448545a6"
	filterScenarioStep2ID  = "08744f052bf79b77df83eadbdbce3e32472397d75874e1167f8ec49841b35473"
	blockFilterScenarioID0 = "7490898586d6c3b9a1215f5c9d6f35c8a588c31b3e0ce9960691ff9e69d1f2bb"
)

var filterScenarioStepIDs = []string{
	"de4fb1b1ff6f847faebb2016c472b312c83214331056a367700f7802d91989b1",
	"6cfb05703b281f4e3a07a269a433c6274a0361933687783601f11db5e7c7f21d",
	filterScenarioStep2ID,
	"fad9295b933aa9ad575a666b035c3a95f7d762234854cebf0f0b4e19cd568b4b",
}

func decodeScenarioLine(t *testing.T, line string) corpusEntry {
	t.Helper()
	dir := writeCorpus(t, map[string]string{"s.jsonl": line + "\n"})
	entries, err := readCorpusFile(dir, filepath.Join(dir, "s.jsonl"))
	if err != nil {
		t.Fatalf("readCorpusFile: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	return entries[0]
}

func TestScenarioIdentitiesMatchTheProducer(t *testing.T) {
	entry := decodeScenarioLine(t, filterScenarioLine)
	id, err := ScenarioID(entry.Steps)
	if err != nil {
		t.Fatal(err)
	}
	if id != filterScenarioID {
		t.Fatalf("ScenarioID = %s, want %s", id, filterScenarioID)
	}
	for i, step := range entry.Steps {
		got, err := ScenarioStepID(id, i, step)
		if err != nil {
			t.Fatal(err)
		}
		if got != filterScenarioStepIDs[i] {
			t.Errorf("step %d id = %s, want %s", i, got, filterScenarioStepIDs[i])
		}
	}

	block := decodeScenarioLine(t, blockFilterScenarioLine)
	id, err = ScenarioID(block.Steps)
	if err != nil {
		t.Fatal(err)
	}
	if id != blockFilterScenarioID {
		t.Fatalf("block ScenarioID = %s, want %s", id, blockFilterScenarioID)
	}
	if got, _ := ScenarioStepID(id, 0, block.Steps[0]); got != blockFilterScenarioID0 {
		t.Errorf("block step 0 id = %s, want %s", got, blockFilterScenarioID0)
	}
}

// The same step in two scenarios, and the same params at two positions of
// one, are different requests: reconciliation compares sets of identities.
func TestScenarioStepIdentitiesAreUniqueAcrossScenarios(t *testing.T) {
	a := decodeScenarioLine(t, filterScenarioLine)
	b := decodeScenarioLine(t, blockFilterScenarioLine)
	seen := map[string]string{}
	for _, entry := range []corpusEntry{a, b} {
		id, _ := ScenarioID(entry.Steps)
		for i, step := range entry.Steps {
			stepID, _ := ScenarioStepID(id, i, step)
			plain, _ := RequestID(step.Method, step.Params)
			if stepID == plain {
				t.Errorf("step %d of %s has its plain request identity", i, entry.Scenario)
			}
			if prior, dup := seen[stepID]; dup {
				t.Errorf("step identity %s shared by %s and %s#%d", stepID, prior, entry.Scenario, i)
			}
			seen[stepID] = fmt.Sprintf("%s#%d", entry.Scenario, i)
		}
	}
}

func TestScenarioLineValidation(t *testing.T) {
	cases := map[string]string{
		"forward ref":           `{"scenario":"x","steps":[{"method":"eth_getFilterLogs","params":[{"$ref":0}]}]}`,
		"later ref":             `{"scenario":"x","steps":[{"method":"a","params":[{"$ref":1}]},{"method":"b","params":[]}]}`,
		"no steps":              `{"scenario":"x","steps":[]}`,
		"no label":              `{"steps":[{"method":"a","params":[]}]}`,
		"step without method":   `{"scenario":"x","steps":[{"params":[]}]}`,
		"call and scenario":     `{"method":"eth_chainId","params":[],"scenario":"x","steps":[{"method":"a","params":[]}]}`,
		"ref with a second key": `{"scenario":"x","steps":[{"method":"a","params":[]},{"method":"b","params":[{"$ref":0,"x":1}]}]}`,
		"ref not an integer":    `{"scenario":"x","steps":[{"method":"a","params":[]},{"method":"b","params":[{"$ref":"0"}]}]}`,
		"negative ref":          `{"scenario":"x","steps":[{"method":"a","params":[]},{"method":"b","params":[{"$ref":-1}]}]}`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			dir := writeCorpus(t, map[string]string{"s.jsonl": line + "\n"})
			if _, err := readCorpusFile(dir, filepath.Join(dir, "s.jsonl")); err == nil {
				t.Fatalf("line %s parsed", line)
			}
		})
	}
	// A nested $ref, at any depth inside params, is a reference too.
	entry := decodeScenarioLine(t, `{"scenario":"x","steps":[{"method":"a","params":[]},{"method":"b","params":[{"inner":[{"$ref":0}]}]}]}`)
	if !entry.opaque[0] || entry.opaque[1] {
		t.Fatalf("opaque = %v, want [true false]", entry.opaque)
	}
}

func TestLoadCorpusConfigScenarios(t *testing.T) {
	dir := writeCorpus(t, map[string]string{
		"eth_chainId.jsonl": `{"method":"eth_chainId","params":[]}` + "\n",
		"scenarios.jsonl":   filterScenarioLine + "\n" + blockFilterScenarioLine + "\n",
	})
	cfg, report, err := LoadCorpusConfigWithExclusions(dir, 0, 42, "0x20", CorpusExclusions{Methods: map[string]struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Methods) != 1 || len(cfg.Scenarios) != 2 {
		t.Fatalf("methods %v, %d scenarios", cfg.Methods, len(cfg.Scenarios))
	}
	// Labels are sorted, like methods.
	if cfg.Scenarios[0].Label != "block_filter" || cfg.Scenarios[1].Label != "log_filter" {
		t.Fatalf("scenario order %s, %s", cfg.Scenarios[0].Label, cfg.Scenarios[1].Label)
	}
	logFilter := cfg.Scenarios[1]
	if logFilter.ScenarioID != filterScenarioID {
		t.Fatalf("scenario id %s", logFilter.ScenarioID)
	}
	wantOpaque := []bool{true, false, false, false}
	for i := range wantOpaque {
		if logFilter.Opaque[i] != wantOpaque[i] {
			t.Fatalf("opaque = %v, want %v", logFilter.Opaque, wantOpaque)
		}
	}
	if logFilter.CallIDs[2] != "scenario:log_filter_variant1#2" {
		t.Fatalf("call id %s", logFilter.CallIDs[2])
	}

	doc := NewCorpusLoadReportDocument(dir, report)
	// One call plus 4 + 2 steps: every step is an entry, a selected call and a
	// request the report names.
	if doc.Totals.EntriesParsed != 7 || doc.Totals.EntriesLoaded != 7 || doc.Totals.CallsSelected != 7 {
		t.Fatalf("totals %+v", doc.Totals)
	}
	if len(doc.Requests.Selected) != 7 {
		t.Fatalf("%d selected", len(doc.Requests.Selected))
	}
	var steps []CorpusRequest
	for _, r := range doc.Requests.Selected {
		if r.Scenario == "log_filter" {
			steps = append(steps, r)
		}
	}
	if len(steps) != 4 {
		t.Fatalf("%d log_filter steps", len(steps))
	}
	for i, r := range steps {
		if r.RequestID != filterScenarioStepIDs[i] || r.Step == nil || *r.Step != i || r.ScenarioID != filterScenarioID {
			t.Errorf("step %d: %+v", i, r)
		}
		if r.CallID != fmt.Sprintf("scenario:log_filter_variant1#%d", i) {
			t.Errorf("step %d call id %s", i, r.CallID)
		}
	}
	for _, f := range doc.Files {
		if strings.HasSuffix(f.Path, "scenarios.jsonl") && (f.Scenarios != 2 || f.Entries != 6 || f.Loaded != 6) {
			t.Errorf("scenario file report %+v", f)
		}
		if strings.HasSuffix(f.Path, "eth_chainId.jsonl") && f.Scenarios != 0 {
			t.Errorf("call file report %+v", f)
		}
	}
}

// A call-only corpus writes no scenario fields at all: the load report of a
// corpus without scenarios is the document it always was.
func TestLoadReportWithoutScenariosHasNoScenarioKeys(t *testing.T) {
	dir := writeCorpus(t, map[string]string{"eth_chainId.jsonl": `{"method":"eth_chainId","params":[]}` + "\n"})
	_, report, err := LoadCorpusConfig(dir, 0, 42, "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(NewCorpusLoadReportDocument(dir, report))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"scenario`, `"step"`} {
		if strings.Contains(string(data), key) {
			t.Errorf("report carries %s: %s", key, data)
		}
	}
}

func TestLoadCorpusConfigExcludesAWholeScenario(t *testing.T) {
	dir := writeCorpus(t, map[string]string{"scenarios.jsonl": filterScenarioLine + "\n" + blockFilterScenarioLine + "\n"})
	exclusions, err := ParseCorpusExclusions("eth_uninstallFilter")
	if err != nil {
		t.Fatal(err)
	}
	cfg, report, err := LoadCorpusConfigWithExclusions(dir, 0, 42, "", exclusions)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Scenarios) != 1 || cfg.Scenarios[0].Label != "block_filter" {
		t.Fatalf("scenarios %+v", cfg.Scenarios)
	}
	doc := NewCorpusLoadReportDocument(dir, report)
	if doc.Totals.EntriesExcluded != 4 || len(doc.Requests.Excluded) != 4 || doc.Totals.EntriesLoaded != 2 {
		t.Fatalf("totals %+v", doc.Totals)
	}
	for i, r := range doc.Requests.Excluded {
		if r.RequestID != filterScenarioStepIDs[i] || r.CallID != "" {
			t.Errorf("excluded %d: %+v", i, r)
		}
	}
}

func TestLoadCorpusConfigSamplesScenariosAsUnits(t *testing.T) {
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, fmt.Sprintf(`{"scenario":"block_filter","steps":[{"method":"eth_newBlockFilter","params":[]},{"method":"eth_getFilterChanges","params":[{"$ref":0}]},{"method":"eth_chainId","params":["%d"]}]}`, i))
	}
	dir := writeCorpus(t, map[string]string{"scenarios.jsonl": strings.Join(lines, "\n") + "\n"})
	cfg, report, err := LoadCorpusConfigWithExclusions(dir, 2, 7, "", CorpusExclusions{Methods: map[string]struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Scenarios) != 2 {
		t.Fatalf("%d scenarios chosen", len(cfg.Scenarios))
	}
	doc := NewCorpusLoadReportDocument(dir, report)
	if doc.Totals.CallsSelected != 6 || doc.Totals.CallsSampleDropped != 9 || doc.Totals.EntriesLoaded != 15 {
		t.Fatalf("totals %+v", doc.Totals)
	}
}

// filterNode is a stateful fake of one node's filter API: ids are handed out
// from a per-node counter, so two nodes answer different ids for the same
// filter, which is what the opaque comparison exists for.
type filterNode struct {
	mu       sync.Mutex
	next     int
	filters  map[string]string
	received []rpcRequest
	failNew  bool
}

func newFilterNode(t *testing.T, firstID int, failNew bool) (*filterNode, *httptest.Server) {
	t.Helper()
	node := &filterNode{next: firstID, filters: map[string]string{}, failNew: failNew}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		resp := map[string]interface{}{"jsonrpc": "2.0", "id": req.ID}
		node.mu.Lock()
		node.received = append(node.received, req)
		id, _ := firstParam(req)
		switch req.Method {
		case "eth_chainId":
			resp["result"] = "0x1"
		case "eth_newFilter", "eth_newBlockFilter":
			if node.failNew {
				resp["error"] = map[string]interface{}{"code": -32000, "message": "filters disabled"}
				break
			}
			fid := fmt.Sprintf("0x%x", node.next)
			node.next++
			node.filters[fid] = req.Method
			resp["result"] = fid
		case "eth_getFilterLogs":
			if node.filters[id] == "eth_newFilter" {
				resp["result"] = []interface{}{map[string]interface{}{"address": "0xabc", "blockNumber": "0x11"}}
			} else {
				resp["error"] = map[string]interface{}{"code": -32000, "message": "filter not found"}
			}
		case "eth_getFilterChanges":
			if _, ok := node.filters[id]; ok {
				resp["result"] = []interface{}{}
			} else {
				resp["error"] = map[string]interface{}{"code": -32000, "message": "filter not found"}
			}
		case "eth_uninstallFilter":
			_, ok := node.filters[id]
			delete(node.filters, id)
			resp["result"] = ok
		default:
			resp["result"] = nil
		}
		node.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return node, srv
}

func firstParam(req rpcRequest) (string, bool) {
	if len(req.Params) == 0 {
		return "", false
	}
	s, ok := req.Params[0].(string)
	return s, ok
}

func runScenarioCorpus(t *testing.T, lines []string, concurrency int, nodes ...*httptest.Server) *Comparator {
	t.Helper()
	dir := writeCorpus(t, map[string]string{"scenarios.jsonl": strings.Join(lines, "\n") + "\n"})
	cfg, _, err := LoadCorpusConfigWithExclusions(dir, 0, 42, "0x20", CorpusExclusions{Methods: map[string]struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	for i, node := range nodes {
		cfg.Clients = append(cfg.Clients, &types.ClientConfig{Name: fmt.Sprintf("node%d", i), URL: node.URL})
	}
	cfg.OutputDir = t.TempDir()
	cfg.Concurrency = concurrency
	cfg.MaxRetries = 1
	cfg.BlockOverride = "0x20"
	cfg.StrictResponseComparison = true
	comp, err := NewComparator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := comp.Run(); err != nil {
		t.Fatal(err)
	}
	return comp
}

func resultsByCall(comp *Comparator) map[string]ComparisonResult {
	out := map[string]ComparisonResult{}
	for _, r := range comp.GetResults() {
		out[r.Method] = r
	}
	return out
}

func TestScenarioRunsInOrderAndComparesIDsAsOpaque(t *testing.T) {
	a, srvA := newFilterNode(t, 1, false)
	b, srvB := newFilterNode(t, 0x100, false)
	// Several copies run concurrently; each must still see its own ids.
	var lines []string
	for i := 0; i < 6; i++ {
		lines = append(lines, strings.Replace(filterScenarioLine, `"0xabc"`, fmt.Sprintf(`"0xabc%d"`, i), 1))
	}
	lines = append(lines, blockFilterScenarioLine)
	comp := runScenarioCorpus(t, lines, 4, srvA, srvB)

	results := comp.GetResults()
	if len(results) != 6*4+2 {
		t.Fatalf("%d results", len(results))
	}
	summary := comp.Summarize()
	if summary.Identical != len(results) {
		for _, r := range results {
			if !r.isIdentical() {
				t.Logf("%s: diff %v transport %v", r.Method, r.Differences, r.TransportErrors)
			}
		}
		t.Fatalf("summary %+v", summary)
	}

	byCall := resultsByCall(comp)
	created := byCall["scenario:log_filter_variant1#0"]
	if created.Responses["node0"].(map[string]interface{})["result"] == created.Responses["node1"].(map[string]interface{})["result"] {
		t.Fatalf("the two nodes answered the same id; the test proves nothing")
	}
	logs := byCall["scenario:log_filter_variant1#1"]
	if logs.RequestID == "" || logs.RPCMethod != "eth_getFilterLogs" {
		t.Fatalf("step result %+v", logs)
	}
	effective := logs.Metadata["effective_params"].(map[string]interface{})
	want0 := created.Responses["node0"].(map[string]interface{})["result"]
	if got := effective["node0"].([]interface{})[0]; got != want0 {
		t.Fatalf("node0 was sent %v, its own filter is %v", got, want0)
	}
	if logs.Params[0].(map[string]interface{})[ScenarioRefKey] == nil {
		t.Fatalf("the result's params are not the corpus form: %v", logs.Params)
	}
	if logs.Metadata["scenario"] != "log_filter" || logs.Metadata["step"] != 1 {
		t.Fatalf("metadata %v", logs.Metadata)
	}

	// Each node saw every scenario's steps in order: a filter is created
	// before it is read, and read before it is uninstalled.
	for _, node := range []*filterNode{a, b} {
		state := map[string]int{}
		for _, req := range node.received {
			id, _ := firstParam(req)
			switch req.Method {
			case "eth_getFilterLogs":
				if state[id] != 0 {
					t.Fatalf("%s out of order", req.Method)
				}
				state[id] = 1
			case "eth_uninstallFilter":
				if state[id] != 1 {
					t.Fatalf("uninstall before read for %s", id)
				}
				state[id] = 2
			}
		}
	}
}

func TestScenarioOpaqueStepStillComparesKind(t *testing.T) {
	if !mustDiffer(t, map[string]interface{}{"result": "0x1"}, map[string]interface{}{"result": float64(1)}) {
		t.Fatal("a string handle and a number handle compared equal")
	}
	if !mustDiffer(t, map[string]interface{}{"result": "0x1"}, map[string]interface{}{"error": map[string]interface{}{"code": -32000.0, "message": "x"}}) {
		t.Fatal("a handle and an error compared equal")
	}
	if mustDiffer(t, map[string]interface{}{"result": "0x1"}, map[string]interface{}{"result": "0x2"}) {
		t.Fatal("two string handles differ")
	}
}

func mustDiffer(t *testing.T, a, b map[string]interface{}) bool {
	t.Helper()
	ctx := newDiffContextStrict("eth_newFilter", nil, true)
	diff, err := compareJSONRPCResponses(ctx, opaqueResponse(a), opaqueResponse(b))
	if err != nil {
		t.Fatal(err)
	}
	return len(diff) > 0
}

// A node that refuses to create the filter cannot be asked about it: the later
// steps are not sent to it and are reported as uncompared, never silently
// dropped and never as a difference.
func TestScenarioUnresolvedRefIsATransportFailure(t *testing.T) {
	_, srvA := newFilterNode(t, 1, false)
	b, srvB := newFilterNode(t, 1, true)
	comp := runScenarioCorpus(t, []string{filterScenarioLine}, 1, srvA, srvB)

	results := resultsByCall(comp)
	if len(results) != 4 {
		t.Fatalf("%d results", len(results))
	}
	if r := results["scenario:log_filter_variant1#0"]; len(r.Differences) == 0 {
		t.Fatalf("a refused creation compared equal to a created filter: %+v", r)
	}
	for step := 1; step < 4; step++ {
		r := results[fmt.Sprintf("scenario:log_filter_variant1#%d", step)]
		if r.TransportErrorClass["node1"] != TransportScenarioRef {
			t.Fatalf("step %d: %+v", step, r.TransportErrorClass)
		}
		if _, ok := r.Responses["node0"]; !ok {
			t.Fatalf("step %d was not sent to the node that has the filter", step)
		}
	}
	for _, req := range b.received {
		if req.Method != "eth_newFilter" && req.Method != "eth_chainId" {
			t.Fatalf("node1 was sent %s", req.Method)
		}
	}
	if !comp.HasTransportErrors() {
		t.Fatal("--fail-on-transport-error would not trip")
	}
	if s := comp.Summarize(); s.TransportError != 3 || s.Total != 4 {
		t.Fatalf("summary %+v", s)
	}
}

// The provenance call count includes every step, so a consumer comparing it
// with the selected calls sees the same number.
func TestScenarioProvenanceCountsSteps(t *testing.T) {
	_, srvA := newFilterNode(t, 1, false)
	_, srvB := newFilterNode(t, 1, false)
	comp := runScenarioCorpus(t, []string{filterScenarioLine, blockFilterScenarioLine}, 2, srvA, srvB)
	if got := comp.Provenance()["call_count"]; got != 6 {
		t.Fatalf("call_count = %v", got)
	}
	path := filepath.Join(t.TempDir(), "results.json")
	if err := comp.SaveResults(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc ComparisonResultsDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range doc.Results {
		ids[r.RequestID] = true
	}
	for _, id := range filterScenarioStepIDs {
		if !ids[id] {
			t.Errorf("step %s missing from the results", id)
		}
	}
}
