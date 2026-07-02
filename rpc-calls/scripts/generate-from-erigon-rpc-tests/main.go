package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fixture struct {
	Request struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	} `json:"request"`
	Metadata struct {
		Latest bool `json:"latest"`
	} `json:"metadata"`
}

type outRecord struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func main() {
	source := flag.String("source", "rpc-calls/sources/erigon-rpc-tests/integration/mainnet", "root directory containing per-method subdirectories of upstream test_*.json files")
	outputDir := flag.String("output-dir", "rpc-calls/", "directory to write <method>-mainnet.jsonl files into")
	methodsCSV := flag.String("methods", "", "optional comma-separated whitelist of method directory names (defaults to all subdirs of --source)")
	maxPerMethod := flag.Int("max-per-method", 0, "if > 0, cap emitted records per method (applied per bucket after shuffle)")
	flag.Parse()

	methodDirs, err := resolveMethodDirs(*source, *methodsCSV)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "fatal: mkdir output-dir:", err)
		os.Exit(1)
	}

	for _, method := range methodDirs {
		methodPath := filepath.Join(*source, method)
		regular, latest, err := collect(methodPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}

		rand.Shuffle(len(regular), func(i, j int) { regular[i], regular[j] = regular[j], regular[i] })
		rand.Shuffle(len(latest), func(i, j int) { latest[i], latest[j] = latest[j], latest[i] })

		if *maxPerMethod > 0 {
			if len(regular) > *maxPerMethod {
				regular = regular[:*maxPerMethod]
			}
			if len(latest) > *maxPerMethod {
				latest = latest[:*maxPerMethod]
			}
		}

		regOut := filepath.Join(*outputDir, fmt.Sprintf("%s-mainnet.jsonl", method))
		latOut := filepath.Join(*outputDir, fmt.Sprintf("%s-mainnet-latest.jsonl", method))

		if err := writeBucket(regOut, regular); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
		if err := writeBucket(latOut, latest); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}

		fmt.Printf("%s: %d regular, %d latest\n", method, len(regular), len(latest))
	}
}

func resolveMethodDirs(source, whitelist string) ([]string, error) {
	entries, err := os.ReadDir(source)
	if err != nil {
		return nil, fmt.Errorf("read source dir %q: %w", source, err)
	}

	available := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			available[e.Name()] = true
		}
	}

	if whitelist == "" {
		names := make([]string, 0, len(available))
		for n := range available {
			names = append(names, n)
		}
		sort.Strings(names)
		return names, nil
	}

	var picked []string
	for _, raw := range strings.Split(whitelist, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if !available[name] {
			return nil, fmt.Errorf("method %q not found under %s", name, source)
		}
		picked = append(picked, name)
	}
	return picked, nil
}

func collect(methodDir string) (regular, latest []outRecord, err error) {
	entries, err := os.ReadDir(methodDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read method dir %q: %w", methodDir, err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(methodDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}

		var arr []fixture
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}

		for i, fx := range arr {
			if fx.Request.Method == "" {
				return nil, nil, fmt.Errorf("%s entry %d: missing request.method", path, i)
			}
			if len(fx.Request.Params) == 0 {
				return nil, nil, fmt.Errorf("%s entry %d: missing request.params", path, i)
			}
			rec := outRecord{Method: fx.Request.Method, Params: fx.Request.Params}
			if fx.Metadata.Latest {
				latest = append(latest, rec)
			} else {
				regular = append(regular, rec)
			}
		}
	}
	return regular, latest, nil
}

func writeBucket(path string, records []outRecord) (err error) {
	if len(records) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, cerr)
		}
	}()
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}
