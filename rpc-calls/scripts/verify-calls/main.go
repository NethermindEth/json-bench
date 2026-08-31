// verify-calls replays generated rpc-calls JSONL fixtures against a live
// endpoint and reports every request that errors or comes back empty. It is the
// acceptance gate for a corpus: a fixture that a node cannot answer is not a
// benchmark input, it is a benchmark of the error path.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type failure struct {
	File   string
	Line   int
	Method string
	Params string
	Reason string
}

type result struct {
	file     string
	total    int
	failures []failure
}

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:8545", "JSON-RPC endpoint to replay against")
	inputs := flag.String("input", "rpc-calls/contracts", "comma-separated files, directories or globs of JSONL fixtures")
	suffix := flag.String("suffix", "", "only consider files whose name contains this substring (e.g. -gnosis)")
	concurrency := flag.Int("concurrency", 4, "in-flight requests")
	attempts := flag.Int("attempts", 3, "attempts per request; only transport and decode faults are retried")
	timeout := flag.Duration("timeout", 60*time.Second, "per-request timeout")
	allowEmpty := flag.Bool("allow-empty", false, "treat an empty eth_call result (0x) as a pass")
	maxReport := flag.Int("max-report", 25, "cap the number of failures printed per file")
	flag.Parse()

	files, err := collect(*inputs, *suffix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collect inputs: %v\n", err)
		os.Exit(2)
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no fixture files matched %q\n", *inputs)
		os.Exit(2)
	}

	client := &http.Client{Timeout: *timeout, Transport: freshConnTransport()}
	var totalCalls, totalFailures int

	for _, f := range files {
		res, err := verifyFile(client, *rpcURL, f, *concurrency, *attempts, *allowEmpty)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			os.Exit(2)
		}
		totalCalls += res.total
		totalFailures += len(res.failures)

		status := "ok"
		if len(res.failures) > 0 {
			status = fmt.Sprintf("%d FAILED", len(res.failures))
		}
		fmt.Printf("%-52s %4d calls  %s\n", filepath.Base(res.file), res.total, status)
		for i, fail := range res.failures {
			if i == *maxReport {
				fmt.Printf("      … %d more\n", len(res.failures)-*maxReport)
				break
			}
			fmt.Printf("      L%-4d %s %s\n            %s\n", fail.Line, fail.Method, truncate(fail.Params, 120), fail.Reason)
		}
	}

	fmt.Printf("\n%d calls across %d files, %d failures\n", totalCalls, len(files), totalFailures)
	if totalFailures > 0 {
		os.Exit(1)
	}
}

// freshConnTransport keeps pooled connections short-lived. Nethermind closes
// idle keep-alive connections on its own schedule; Go will happily hand a
// half-closed one to the next POST, and POST is the one method it will not
// silently retry — the symptom is a truncated response body that looks like a
// broken fixture. A short idle timeout retires those before they are reused.
func freshConnTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.IdleConnTimeout = 5 * time.Second
	return t
}

func collect(spec, suffix string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !strings.HasSuffix(p, ".jsonl") && !strings.HasSuffix(p, ".json") {
			return
		}
		if suffix != "" && !strings.Contains(filepath.Base(p), suffix) {
			return
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	for _, raw := range strings.Split(spec, ",") {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		info, err := os.Stat(item)
		switch {
		case err == nil && info.IsDir():
			entries, err := os.ReadDir(item)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() {
					add(filepath.Join(item, e.Name()))
				}
			}
		case err == nil:
			add(item)
		default:
			matches, err := filepath.Glob(item)
			if err != nil {
				return nil, err
			}
			for _, m := range matches {
				add(m)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func verifyFile(client *http.Client, url, path string, concurrency, attempts int, allowEmpty bool) (result, error) {
	f, err := os.Open(path)
	if err != nil {
		return result{}, err
	}
	defer f.Close()

	type job struct {
		line int
		req  request
		raw  string
	}
	jobs := make(chan job)
	var (
		mu    sync.Mutex
		fails []failure
		wg    sync.WaitGroup
	)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if reason := checkWithRetry(client, url, j.req, attempts, allowEmpty); reason != "" {
					mu.Lock()
					fails = append(fails, failure{
						File: path, Line: j.line, Method: j.req.Method,
						Params: string(j.req.Params), Reason: reason,
					})
					mu.Unlock()
				}
			}
		}()
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	total, line := 0, 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(text), &req); err != nil {
			mu.Lock()
			fails = append(fails, failure{File: path, Line: line, Reason: "malformed JSON: " + err.Error()})
			mu.Unlock()
			continue
		}
		total++
		jobs <- job{line: line, req: req, raw: text}
	}
	close(jobs)
	wg.Wait()
	if err := sc.Err(); err != nil {
		return result{}, err
	}

	sort.Slice(fails, func(i, j int) bool { return fails[i].Line < fails[j].Line })
	return result{file: path, total: total, failures: fails}, nil
}

// checkWithRetry retries transport and decode faults — a node behind an SSH
// tunnel or a proxy will occasionally hand back a truncated body under
// concurrency, and reporting that as a bad fixture would be a false positive.
// RPC-level errors are the fixture's own fault and are never retried.
func checkWithRetry(client *http.Client, url string, req request, attempts int, allowEmpty bool) string {
	var reason string
	for i := 0; i < attempts; i++ {
		reason = check(client, url, req, allowEmpty)
		if reason == "" || !retryable(reason) {
			return reason
		}
		time.Sleep(time.Duration(i+1) * 250 * time.Millisecond)
	}
	return reason
}

func retryable(reason string) bool {
	return strings.HasPrefix(reason, "transport:") ||
		strings.HasPrefix(reason, "decode:") ||
		strings.HasPrefix(reason, "read body:")
}

func check(client *http.Client, url string, req request, allowEmpty bool) string {
	params := req.Params
	if len(params) == 0 {
		params = json.RawMessage("[]")
	}
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":%s}`, req.Method, params)

	resp, err := client.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		return "transport: " + err.Error()
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "read body: " + err.Error()
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 160))
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "decode: " + truncate(string(raw), 160)
	}
	if envelope.Error != nil {
		return fmt.Sprintf("rpc error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return "null result"
	}
	if !allowEmpty && string(envelope.Result) == `"0x"` {
		return "empty result (0x) — the call reverted silently or the address has no code"
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
