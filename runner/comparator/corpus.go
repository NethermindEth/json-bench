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

// corpusExcluded lists methods that are unsuitable for cross-client archive
// correctness comparison: proofs are not always stored, and head-dependent
// methods diverge legitimately between nodes at different heads. The debug_
// namespace is excluded by prefix. eth_feeHistory is excluded only when no
// block override is set — with a static newestBlock it is deterministic (see
// isCorpusExcluded).
var corpusExcluded = map[string]struct{}{
	"eth_getProof":             {},
	"eth_gasPrice":             {},
	"eth_syncing":              {},
	"eth_blockNumber":          {},
	"eth_maxPriorityFeePerGas": {},
}

// corpusPinnable lists methods excluded by default but kept when a block
// override pins them to a static block.
var corpusPinnable = map[string]struct{}{
	"eth_feeHistory": {},
}

type corpusEntry struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// LoadCorpusConfig builds a ComparisonConfig by ingesting a corpus directory
// recursively. It reads both line-delimited *.jsonl files and *.json files
// holding a JSON array of {method, params} objects. When sample > 0 at most
// that many calls per method are kept, chosen deterministically from seed.
// Excluded methods (see corpusExcluded and the debug_ prefix) are dropped;
// pinnable methods like eth_feeHistory are kept when blockOverride is set.
func LoadCorpusConfig(dir string, sample int, seed int64, blockOverride string) (*ComparisonConfig, error) {
	var files []string
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
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
		return nil, fmt.Errorf("failed to scan corpus dir: %w", walkErr)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .jsonl or .json files found under %s", dir)
	}
	sort.Strings(files)

	keepPinnable := blockOverride != ""

	byMethod := make(map[string][][]interface{})
	order := make([]string, 0)
	for _, file := range files {
		entries, err := readCorpusFile(file)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Method == "" || isCorpusExcluded(entry.Method, keepPinnable) {
				continue
			}
			if entry.Params == nil {
				entry.Params = []interface{}{}
			}
			if _, seen := byMethod[entry.Method]; !seen {
				order = append(order, entry.Method)
			}
			byMethod[entry.Method] = append(byMethod[entry.Method], entry.Params)
		}
	}

	if len(order) == 0 {
		return nil, fmt.Errorf("corpus in %s contained no usable calls after exclusions", dir)
	}
	sort.Strings(order)

	rng := rand.New(rand.NewSource(seed))
	cfg := &ComparisonConfig{
		Name:             fmt.Sprintf("corpus:%s", filepath.Base(strings.TrimRight(dir, "/"))),
		Description:      fmt.Sprintf("Sampled from %s (sample=%d)", dir, sample),
		Methods:          make([]string, 0),
		MethodRPCNames:   make(map[string]string),
		CustomParameters: make(map[string][]interface{}),
	}

	for _, method := range order {
		calls := sampleCalls(byMethod[method], sample, rng)
		for i, params := range calls {
			identifier := fmt.Sprintf("%s_variant%d", method, i+1)
			cfg.Methods = append(cfg.Methods, identifier)
			cfg.MethodRPCNames[identifier] = method
			cfg.CustomParameters[identifier] = params
		}
	}

	return cfg, nil
}

// readCorpusFile parses one corpus file. A .json file is a JSON array of
// entries; a .jsonl file is one entry per line.
func readCorpusFile(path string) ([]corpusEntry, error) {
	safePath, err := config.SafeReadPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(safePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read corpus file %s: %w", path, err)
	}

	if strings.HasSuffix(path, ".json") {
		var entries []corpusEntry
		if err := json.Unmarshal(data, &entries); err != nil {
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
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return entries, nil
}

// sampleCalls returns at most n calls, chosen with a seeded shuffle so the
// selection is reproducible, preserving original order within the selection.
func sampleCalls(calls [][]interface{}, n int, rng *rand.Rand) [][]interface{} {
	if n <= 0 || len(calls) <= n {
		return calls
	}
	idx := make([]int, len(calls))
	for i := range idx {
		idx[i] = i
	}
	rng.Shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
	chosen := idx[:n]
	sort.Ints(chosen)
	out := make([][]interface{}, 0, n)
	for _, i := range chosen {
		out = append(out, calls[i])
	}
	return out
}

func isCorpusExcluded(method string, keepPinnable bool) bool {
	if strings.HasPrefix(method, "debug_") {
		return true
	}
	if _, ok := corpusExcluded[method]; ok {
		return true
	}
	if _, ok := corpusPinnable[method]; ok {
		return !keepPinnable
	}
	return false
}
