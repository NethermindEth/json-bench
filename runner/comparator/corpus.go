package comparator

import (
	"bufio"
	"encoding/json"
	"fmt"
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
// namespace is excluded by prefix.
var corpusExcluded = map[string]struct{}{
	"eth_getProof":             {},
	"eth_gasPrice":             {},
	"eth_syncing":              {},
	"eth_blockNumber":          {},
	"eth_maxPriorityFeePerGas": {},
	"eth_feeHistory":           {},
}

type corpusEntry struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// LoadCorpusConfig builds a ComparisonConfig by ingesting rpc-calls/*.jsonl
// files under dir. Each line is a {method, params} object. When sample > 0 at
// most that many calls per method are kept, chosen deterministically from seed.
// Excluded methods (see corpusExcluded and the debug_ prefix) are dropped.
func LoadCorpusConfig(dir string, sample int, seed int64) (*ComparisonConfig, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to scan corpus dir: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no .jsonl files found in %s", dir)
	}
	sort.Strings(matches)

	byMethod := make(map[string][][]interface{})
	order := make([]string, 0)
	for _, file := range matches {
		safePath, err := config.SafeReadPath(file)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(safePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open corpus file %s: %w", file, err)
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var entry corpusEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				f.Close()
				return nil, fmt.Errorf("failed to parse %s: %w", file, err)
			}
			if entry.Method == "" || isCorpusExcluded(entry.Method) {
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
		if err := scanner.Err(); err != nil {
			f.Close()
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}
		f.Close()
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

func isCorpusExcluded(method string) bool {
	if strings.HasPrefix(method, "debug_") {
		return true
	}
	_, ok := corpusExcluded[method]
	return ok
}
