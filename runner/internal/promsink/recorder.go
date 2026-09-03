package promsink

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// WritePath is where Prometheus accepts remote-write requests, and what the
// runner appends to its --prometheus base URL.
const WritePath = "/api/v1/write"

// contractHeaders are the request headers that form the remote-write contract.
// Content-Length is deliberately absent because it varies per push, and
// Authorization is never recorded verbatim — only its scheme, so a capture can
// be committed without leaking a credential.
var contractHeaders = []string{
	"Content-Encoding",
	"Content-Type",
	"User-Agent",
	"X-Prometheus-Remote-Write-Version",
}

// Series is one recorded series identity plus every value pushed for it, in
// arrival order. The value sequence is what distinguishes a cumulative metric
// from a per-window one.
type Series struct {
	Name      string
	LabelKeys []string
	Labels    map[string]string
	Values    []float64
}

// Key identifies a series by name and label names, which is the granularity a
// dashboard query binds to.
func (s Series) Key() string {
	return s.Name + "{" + strings.Join(s.LabelKeys, ",") + "}"
}

// Recorder accumulates what a remote-write client pushed at it.
type Recorder struct {
	mu       sync.Mutex
	series   map[string]*Series
	headers  map[string]string
	authKind string
	pushes   int
	lastErr  error
}

func NewRecorder() *Recorder {
	return &Recorder{
		series:  make(map[string]*Series),
		headers: make(map[string]string),
	}
}

// Handler accepts remote-write pushes. It answers 204 like Prometheus does, and
// 400 on a body it cannot decode so a broken encoder surfaces at the client.
func (rec *Recorder) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(WritePath, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			rec.fail(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		series, err := Decode(body)
		if err != nil {
			rec.fail(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		rec.observe(r, series)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func (rec *Recorder) fail(err error) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.lastErr = err
}

func (rec *Recorder) observe(r *http.Request, series []TimeSeries) {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	rec.pushes++
	for _, name := range contractHeaders {
		if v := r.Header.Get(name); v != "" {
			rec.headers[name] = v
		}
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		if scheme, _, ok := strings.Cut(auth, " "); ok {
			rec.authKind = strings.ToLower(scheme)
		} else {
			rec.authKind = "unknown"
		}
	}

	for _, ts := range series {
		name := ts.Name()
		if name == "" {
			continue
		}
		s := &Series{Name: name, LabelKeys: ts.LabelKeys(), Labels: ts.Labels}
		key := s.Key()
		existing, ok := rec.series[key]
		if !ok {
			rec.series[key] = s
			existing = s
		}
		for _, sample := range ts.Samples {
			existing.Values = append(existing.Values, sample.Value)
		}
	}
}

// Pushes reports how many remote-write requests arrived, which is how the push
// cadence is checked.
func (rec *Recorder) Pushes() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.pushes
}

// Err reports the last decode or read failure, if any.
func (rec *Recorder) Err() error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.lastErr
}

// Headers returns the recorded contract headers, plus an "Authorization" entry
// naming only the scheme that was used.
func (rec *Recorder) Headers() map[string]string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make(map[string]string, len(rec.headers)+1)
	for k, v := range rec.headers {
		out[k] = v
	}
	if rec.authKind != "" {
		out["Authorization"] = rec.authKind + " <redacted>"
	}
	return out
}

// Series returns every recorded series, ordered by key.
func (rec *Recorder) Series() []Series {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]Series, 0, len(rec.series))
	for _, s := range rec.series {
		copied := *s
		copied.Values = append([]float64(nil), s.Values...)
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// Keys returns every recorded series key, sorted. This is the set a golden file
// pins and the granularity at which two engines' Prometheus output is compared.
func (rec *Recorder) Keys() []string {
	series := rec.Series()
	out := make([]string, 0, len(series))
	for _, s := range series {
		out = append(out, s.Key())
	}
	return out
}

// MetricNames returns the distinct metric names recorded, sorted.
func (rec *Recorder) MetricNames() []string {
	series := rec.Series()
	seen := make(map[string]struct{}, len(series))
	out := make([]string, 0, len(series))
	for _, s := range series {
		if _, dup := seen[s.Name]; dup {
			continue
		}
		seen[s.Name] = struct{}{}
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// WriteGolden renders the capture in the golden-file format: a header block of
// the contract headers, then one line per series key. Both are sorted, so the
// output is stable across runs and diffs cleanly when a series is added,
// renamed or lost.
func (rec *Recorder) WriteGolden(w io.Writer, source string) error {
	headers := rec.Headers()
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# remote-write capture\n")
	if source != "" {
		fmt.Fprintf(&b, "# source: %s\n", source)
	}
	b.WriteString("[headers]\n")
	for _, name := range names {
		fmt.Fprintf(&b, "%s: %s\n", name, headers[name])
	}
	b.WriteString("[series]\n")
	for _, key := range rec.Keys() {
		b.WriteString(key)
		b.WriteString("\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// ParseGolden reads back the header and series blocks of a golden file.
func ParseGolden(data []byte) (headers map[string]string, keys []string, err error) {
	headers = make(map[string]string)
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		switch section {
		case "headers":
			name, value, ok := strings.Cut(line, ":")
			if !ok {
				return nil, nil, fmt.Errorf("malformed header line %q", line)
			}
			headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
		case "series":
			keys = append(keys, line)
		default:
			return nil, nil, fmt.Errorf("line %q appears outside any section", line)
		}
	}
	return headers, keys, nil
}
