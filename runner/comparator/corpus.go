package comparator

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jsonrpc-bench/runner/config"
)

// corpusPinnable lists methods excluded by default but kept when a block
// override pins them to a static block. The rest of the default method policy
// lives in DefaultCorpusExclusions (corpus_exclusions.go) and can be narrowed
// per run with --corpus-exclude; this rule cannot, because without an override
// eth_feeHistory's newestBlock is resolved against each node's own head.
var corpusPinnable = map[string]struct{}{
	"eth_feeHistory": {},
}

type corpusEntry struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`

	// Scenario and Steps are set instead of Method and Params on a scenario
	// line (scenario.go). opaque is filled in by readCorpusFile.
	Scenario string         `json:"scenario"`
	Steps    []ScenarioStep `json:"steps"`
	opaque   []bool
}

// isScenario reports whether the entry is a scenario line.
func (e corpusEntry) isScenario() bool {
	return e.Scenario != "" || e.Steps != nil
}

// calls is how many calls the entry stands for: one, or one per step.
func (e corpusEntry) calls() int {
	if e.isScenario() {
		return len(e.Steps)
	}
	return 1
}

// corpusScenario is one loaded scenario on its way to selection, the scenario
// counterpart of corpusCall.
type corpusScenario struct {
	Label      string
	Steps      []ScenarioStep
	Opaque     []bool
	Path       string
	ScenarioID string
	StepIDs    []string
	IDError    string
}

// corpusCall is one loaded call on its way to selection: the params, the file
// it came from, and its request identity, carried together so the load report
// can account for a call that sampling later drops.
type corpusCall struct {
	Params    []interface{}
	Path      string
	RequestID string
	IDError   string
}

// CorpusSkip records a corpus file that could not be used, so the caller can
// report it. A corpus tree commonly holds files that are not corpora at all
// (generator inputs, for instance), and one of those must not abort the run.
type CorpusSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// CorpusFileReport is the per-file accounting of one file that parsed: how many
// entries it held and what became of them. A file that parsed but named no
// method appears here with its entries counted as Unnamed, *and* in Skips with
// the reason — the two answer different questions, and an entry that was read
// has to be counted whatever the file turned out to be.
//
// A scenario line counts once per step in Entries, Excluded and Loaded, because
// every step is a call the run has to account for; Scenarios counts the lines.
type CorpusFileReport struct {
	Path      string `json:"path"`
	Entries   int    `json:"entries"`
	Unnamed   int    `json:"unnamed"`
	Excluded  int    `json:"excluded"`
	Loaded    int    `json:"loaded"`
	Scenarios int    `json:"scenarios,omitempty"`
}

// CorpusRequest names one request by its identity (see RequestID) rather than
// by its position in a file, so a consumer can reconcile what was selected,
// excluded and dropped against the manifest that produced the corpus.
//
// IdentityError is set, and RequestID empty, only when the params could not be
// canonicalized at all. It is recorded rather than swallowed: an unidentifiable
// request must be visible to whatever is reconciling the run.
//
// A scenario step also carries its scenario's label and identity and its
// position in it; RequestID is then the step identity (ScenarioStepID).
type CorpusRequest struct {
	RequestID     string `json:"request_id"`
	Method        string `json:"method"`
	CallID        string `json:"call_id,omitempty"`
	Path          string `json:"path"`
	IdentityError string `json:"identity_error,omitempty"`
	Scenario      string `json:"scenario,omitempty"`
	ScenarioID    string `json:"scenario_id,omitempty"`
	Step          *int   `json:"step,omitempty"`
}

// CorpusReport describes what a corpus load actually ingested. Excluded counts
// files whose calls were all dropped by the method exclusions (see
// CorpusExclusions) — expected, unlike a skip.
//
// Files, Entries, Excluded and Skips are the counts the log line carries and
// are unchanged. The rest is the machine-readable accounting written to
// corpus-load-report.json.
type CorpusReport struct {
	Files    int
	Entries  int
	Excluded int
	Skips    []CorpusSkip

	FilesScanned int
	PerFile      []CorpusFileReport

	Selected         []CorpusRequest
	ExcludedRequests []CorpusRequest
	SampleDropped    []CorpusRequest

	Sample        int
	SampleSeed    int64
	BlockOverride string

	// Exclusions is the policy that produced ExcludedRequests, recorded so
	// the load report states the effective list rather than implying the
	// default.
	Exclusions CorpusExclusions
}

// LoadCorpusConfig builds a ComparisonConfig by ingesting a corpus directory
// recursively under the default exclusion policy (DefaultCorpusExclusions).
// See LoadCorpusConfigWithExclusions.
func LoadCorpusConfig(dir string, sample int, seed int64, blockOverride string) (*ComparisonConfig, *CorpusReport, error) {
	return LoadCorpusConfigWithExclusions(dir, sample, seed, blockOverride, DefaultCorpusExclusions())
}

// LoadCorpusConfigWithExclusions builds a ComparisonConfig by ingesting a corpus
// directory recursively. It reads both line-delimited *.jsonl files and *.json
// files holding a JSON array of {method, params} objects. When sample > 0 at
// most that many calls per method are kept, chosen deterministically from
// seed. Methods the exclusions name are dropped; pinnable methods like
// eth_feeHistory are kept when blockOverride is set. A file that does not
// parse as corpus entries is skipped and reported rather than failing the
// load, which only happens when nothing usable was found.
func LoadCorpusConfigWithExclusions(dir string, sample int, seed int64, blockOverride string, exclusions CorpusExclusions) (*ComparisonConfig, *CorpusReport, error) {
	// Resolve the root through any symlink before walking: WalkDir does not
	// follow a symlinked root, so a linked corpus directory would otherwise look
	// empty. Reading files under the resolved root also keeps the containment
	// check in readCorpusFile comparing like with like.
	walkRoot := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		walkRoot = resolved
	}

	var files []string
	walkErr := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") || strings.HasSuffix(path, ".json") {
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, fmt.Errorf("failed to scan corpus dir: %w", walkErr)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no .jsonl or .json files found under %s", dir)
	}
	sort.Strings(files)

	keepPinnable := blockOverride != ""

	report := &CorpusReport{
		FilesScanned:  len(files),
		Sample:        sample,
		SampleSeed:    seed,
		BlockOverride: blockOverride,
		Exclusions:    exclusions,
	}
	byMethod := make(map[string][]corpusCall)
	order := make([]string, 0)
	byLabel := make(map[string][]corpusScenario)
	labels := make([]string, 0)
	for _, file := range files {
		entries, err := readCorpusFile(walkRoot, file)
		if err != nil {
			report.Skips = append(report.Skips, CorpusSkip{Path: file, Reason: err.Error()})
			continue
		}
		fileReport := CorpusFileReport{Path: file}
		for _, entry := range entries {
			fileReport.Entries += entry.calls()
		}
		used, named := 0, 0
		for _, entry := range entries {
			if entry.isScenario() {
				named++
				fileReport.Scenarios++
				scenario := loadScenario(file, entry)
				if scenarioExcluded(exclusions, entry.Steps, keepPinnable) {
					fileReport.Excluded += len(entry.Steps)
					report.ExcludedRequests = append(report.ExcludedRequests, scenario.requests("")...)
					continue
				}
				if _, seen := byLabel[entry.Scenario]; !seen {
					labels = append(labels, entry.Scenario)
				}
				byLabel[entry.Scenario] = append(byLabel[entry.Scenario], scenario)
				used += len(entry.Steps)
				continue
			}
			if entry.Method == "" {
				fileReport.Unnamed++
				continue
			}
			named++
			if entry.Params == nil {
				entry.Params = []interface{}{}
			}
			id, idErr := requestIDOf(entry.Method, entry.Params)
			if exclusions.Excludes(entry.Method, keepPinnable) {
				fileReport.Excluded++
				report.ExcludedRequests = append(report.ExcludedRequests, CorpusRequest{
					RequestID:     id,
					Method:        entry.Method,
					Path:          file,
					IdentityError: idErr,
				})
				continue
			}
			if _, seen := byMethod[entry.Method]; !seen {
				order = append(order, entry.Method)
			}
			byMethod[entry.Method] = append(byMethod[entry.Method], corpusCall{
				Params:    entry.Params,
				Path:      file,
				RequestID: id,
				IDError:   idErr,
			})
			used++
		}
		// Recorded before the skip branch, and for every file that parsed: a file
		// whose entries name no method still parsed N entries, and throwing that
		// count away is what would make entries_parsed disagree with the number
		// of entries actually read.
		fileReport.Loaded = used
		report.PerFile = append(report.PerFile, fileReport)
		if named == 0 {
			// Parsed, but nothing in it names a method: not a corpus file.
			report.Skips = append(report.Skips, CorpusSkip{Path: file, Reason: `no entries with a "method" field`})
			continue
		}
		if used == 0 {
			report.Excluded++
			continue
		}
		report.Files++
		report.Entries += used
	}

	if len(order) == 0 && len(labels) == 0 {
		return nil, report, fmt.Errorf("corpus in %s contained no usable calls after exclusions (%d of %d files skipped)", dir, len(report.Skips), len(files))
	}
	sort.Strings(order)
	sort.Strings(labels)

	rng := rand.New(rand.NewSource(seed))
	cfg := &ComparisonConfig{
		Name:             fmt.Sprintf("corpus:%s", filepath.Base(strings.TrimRight(dir, "/"))),
		Description:      fmt.Sprintf("Sampled from %s (sample=%d)", dir, sample),
		Methods:          make([]string, 0),
		MethodRPCNames:   make(map[string]string),
		CustomParameters: make(map[string][]interface{}),
	}

	for _, method := range order {
		chosen, dropped := sampleCalls(byMethod[method], sample, rng)
		for i, call := range chosen {
			identifier := fmt.Sprintf("%s_variant%d", method, i+1)
			cfg.Methods = append(cfg.Methods, identifier)
			cfg.MethodRPCNames[identifier] = method
			cfg.CustomParameters[identifier] = call.Params
			report.Selected = append(report.Selected, CorpusRequest{
				RequestID:     call.RequestID,
				Method:        method,
				CallID:        identifier,
				Path:          call.Path,
				IdentityError: call.IDError,
			})
		}
		for _, call := range dropped {
			report.SampleDropped = append(report.SampleDropped, CorpusRequest{
				RequestID:     call.RequestID,
				Method:        method,
				Path:          call.Path,
				IdentityError: call.IDError,
			})
		}
	}

	// Scenarios are sampled after every method, as whole units per label, so
	// a corpus without them draws exactly the selection it always did.
	for _, label := range labels {
		chosen, dropped := sampleScenarios(byLabel[label], sample, rng)
		for i, scenario := range chosen {
			callPrefix := fmt.Sprintf("scenario:%s_variant%d", label, i+1)
			call := ScenarioCall{
				Label:      label,
				ScenarioID: scenario.ScenarioID,
				Steps:      scenario.Steps,
				StepIDs:    scenario.StepIDs,
				Opaque:     scenario.Opaque,
			}
			for step := range scenario.Steps {
				call.CallIDs = append(call.CallIDs, fmt.Sprintf("%s#%d", callPrefix, step))
			}
			cfg.Scenarios = append(cfg.Scenarios, call)
			report.Selected = append(report.Selected, scenario.requests(callPrefix)...)
		}
		for _, scenario := range dropped {
			report.SampleDropped = append(report.SampleDropped, scenario.requests("")...)
		}
	}

	return cfg, report, nil
}

// loadScenario computes a scenario line's identities. An identity that cannot
// be computed is recorded on every step, as it is for a call.
func loadScenario(path string, entry corpusEntry) corpusScenario {
	scenario := corpusScenario{
		Label:  entry.Scenario,
		Steps:  entry.Steps,
		Opaque: entry.opaque,
		Path:   path,
	}
	id, err := ScenarioID(entry.Steps)
	if err != nil {
		scenario.IDError = err.Error()
		scenario.StepIDs = make([]string, len(entry.Steps))
		return scenario
	}
	scenario.ScenarioID = id
	for i, step := range entry.Steps {
		stepID, err := ScenarioStepID(id, i, step)
		if err != nil && scenario.IDError == "" {
			scenario.IDError = err.Error()
		}
		scenario.StepIDs = append(scenario.StepIDs, stepID)
	}
	return scenario
}

// requests names every step of the scenario for the load report. callPrefix is
// set for a selected scenario, whose steps are handed to the comparator under
// "<callPrefix>#<step>".
func (s corpusScenario) requests(callPrefix string) []CorpusRequest {
	out := make([]CorpusRequest, len(s.Steps))
	for i, step := range s.Steps {
		index := i
		out[i] = CorpusRequest{
			RequestID:     s.StepIDs[i],
			Method:        step.Method,
			Path:          s.Path,
			IdentityError: s.IDError,
			Scenario:      s.Label,
			ScenarioID:    s.ScenarioID,
			Step:          &index,
		}
		if callPrefix != "" {
			out[i].CallID = fmt.Sprintf("%s#%d", callPrefix, i)
		}
	}
	return out
}

// scenarioExcluded reports whether the exclusions drop a scenario: they do
// when they name any of its steps, because a scenario with a step missing is a
// different scenario.
func scenarioExcluded(exclusions CorpusExclusions, steps []ScenarioStep, keepPinnable bool) bool {
	for _, step := range steps {
		if exclusions.Excludes(step.Method, keepPinnable) {
			return true
		}
	}
	return false
}

// requestIDOf returns the request identity, or an empty identity and the reason
// it could not be computed. A call whose params came from JSON always has one;
// a config-supplied param that is not representable as JSON does not, and that
// has to be reported rather than silently become the empty string.
func requestIDOf(method string, params []interface{}) (string, string) {
	id, err := RequestID(method, params)
	if err != nil {
		return "", err.Error()
	}
	return id, ""
}

// readCorpusFile parses one corpus file. A .json file is a JSON array of
// entries; a .jsonl file is one entry per line. root is the operator-supplied
// corpus directory the path was discovered under.
//
// Numbers are decoded with UseNumber, so a numeric param keeps the digits the
// corpus recorded. Through float64 a param above 2^53 would be re-emitted as a
// different number on the wire, and its canonical form — hence its request
// identity — would stop matching the producer's.
func readCorpusFile(root, path string) ([]corpusEntry, error) {
	safePath, err := config.SafeReadPathUnder(root, path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read corpus file %s: %w", path, err)
	}

	if strings.HasSuffix(path, ".json") {
		var entries []corpusEntry
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&entries); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		if dec.More() {
			return nil, fmt.Errorf("failed to parse %s: trailing data after the top-level array", path)
		}
		if err := checkScenarioEntries(entries); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		return entries, nil
	}

	var entries []corpusEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry corpusEntry
		dec := json.NewDecoder(strings.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&entry); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	if err := checkScenarioEntries(entries); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return entries, nil
}

// checkScenarioEntries validates every scenario line in a parsed file and
// records which of its steps are referenced. A line that is both a call and a
// scenario is malformed: which of the two it meant cannot be known.
func checkScenarioEntries(entries []corpusEntry) error {
	for i := range entries {
		entry := &entries[i]
		if !entry.isScenario() {
			continue
		}
		if entry.Method != "" || entry.Params != nil {
			return fmt.Errorf("entry %d carries both a call and a scenario", i+1)
		}
		opaque, err := validateScenario(entry.Scenario, entry.Steps)
		if err != nil {
			return err
		}
		entry.opaque = opaque
	}
	return nil
}

// sampleScenarios is sampleCalls for scenarios: at most n per label, chosen
// with the same seeded shuffle, the rest returned as dropped.
func sampleScenarios(scenarios []corpusScenario, n int, rng *rand.Rand) (chosen, dropped []corpusScenario) {
	if n <= 0 || len(scenarios) <= n {
		return scenarios, nil
	}
	keep := sampleKeep(len(scenarios), n, rng)
	for i, scenario := range scenarios {
		if _, ok := keep[i]; ok {
			chosen = append(chosen, scenario)
			continue
		}
		dropped = append(dropped, scenario)
	}
	return chosen, dropped
}

// sampleKeep draws n of count indices with a seeded shuffle.
func sampleKeep(count, n int, rng *rand.Rand) map[int]struct{} {
	idx := make([]int, count)
	for i := range idx {
		idx[i] = i
	}
	rng.Shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
	keep := make(map[int]struct{}, n)
	for _, i := range idx[:n] {
		keep[i] = struct{}{}
	}
	return keep
}

// sampleCalls returns at most n calls, chosen with a seeded shuffle so the
// selection is reproducible, preserving original order within the selection.
// The calls it did not choose are returned too, in original order, so the load
// report can account for every call the corpus held.
func sampleCalls(calls []corpusCall, n int, rng *rand.Rand) (chosen, dropped []corpusCall) {
	if n <= 0 || len(calls) <= n {
		return calls, nil
	}
	keep := sampleKeep(len(calls), n, rng)
	chosen = make([]corpusCall, 0, n)
	dropped = make([]corpusCall, 0, len(calls)-n)
	for i, call := range calls {
		if _, ok := keep[i]; ok {
			chosen = append(chosen, call)
			continue
		}
		dropped = append(dropped, call)
	}
	return chosen, dropped
}
