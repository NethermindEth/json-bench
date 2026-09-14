package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/types"
)

// DefaultTargetMetricPatterns are the families worth reading from any node's
// own metrics endpoint: what the process spent, and what its runtime did while
// spending it. Client-specific families have to be named explicitly, because a
// built-in list of them would rot as each client renames its own.
//
// Nethermind exposes `nethermind_*` and, with the .NET exporter, `dotnet_*`;
// Geth exposes its own families under `/debug/metrics/prometheus`. Add them
// with --target-metric.
var DefaultTargetMetricPatterns = []string{
	"process_cpu_seconds_total",
	"process_resident_memory_bytes",
	"process_virtual_memory_bytes",
	"process_open_fds",
	"go_goroutines",
	"go_memstats_alloc_bytes",
	"go_gc_duration_seconds",
	"dotnet_total_memory_bytes",
	"dotnet_collection_count_total",
}

// TargetMetricsOptions controls scraping the node's own metrics endpoint.
type TargetMetricsOptions struct {
	// Interval is how often each endpoint is read. Scraping is a cost the node
	// pays during the measurement, so this is deliberately not sub-second.
	Interval time.Duration

	// Patterns select which families to keep. A pattern ending in '*' matches
	// by prefix; anything else must match the family name exactly.
	Patterns []string

	Timeout time.Duration
}

func DefaultTargetMetricsOptions() TargetMetricsOptions {
	return TargetMetricsOptions{
		Interval: 5 * time.Second,
		Patterns: DefaultTargetMetricPatterns,
		Timeout:  5 * time.Second,
	}
}

// matches reports whether a family name is selected.
func (o TargetMetricsOptions) matches(name string) bool {
	for _, pattern := range o.Patterns {
		if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
			if strings.HasPrefix(name, prefix) {
				return true
			}
			continue
		}
		if name == pattern {
			return true
		}
	}
	return false
}

// targetSample is one family's value at one scrape.
type targetSample struct {
	at    time.Time
	value float64
}

// targetSeries accumulates one metric family for one client.
type targetSeries struct {
	name    string
	kind    dto.MetricType
	labels  map[string]string
	samples []targetSample
}

// TargetScraper reads a node's own metrics endpoint alongside the load, so the
// client-side latency can be read next to what the node was doing to produce
// it. Without it the only resource figures a run records are the load
// generator's, which is the wrong host whenever the run is remote.
type TargetScraper struct {
	client string
	url    string
	opts   TargetMetricsOptions
	http   *http.Client
	log    *logrus.Logger

	mu      sync.Mutex
	series  map[string]*targetSeries
	errs    int
	lastErr error
}

func NewTargetScraper(client, url string, opts TargetMetricsOptions, log *logrus.Logger) *TargetScraper {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &TargetScraper{
		client: client,
		url:    url,
		opts:   opts,
		http:   &http.Client{Timeout: timeout},
		log:    log,
		series: make(map[string]*targetSeries),
	}
}

// Run scrapes until the context ends. It takes one sample immediately so a
// short run still has a before-and-after pair to difference.
func (s *TargetScraper) Run(ctx context.Context) {
	interval := s.opts.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	s.scrape(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.scrape(ctx)
		case <-ctx.Done():
			// A final read, so a counter's delta covers the whole run rather
			// than stopping at the last tick.
			s.scrape(context.WithoutCancel(ctx))
			return
		}
	}
}

func (s *TargetScraper) scrape(ctx context.Context) {
	families, err := s.fetch(ctx)
	if err != nil {
		// A read cut short because the run ended is the run ending, not the
		// endpoint failing. Counting it would put a spurious gap warning on
		// every cancelled run.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		s.mu.Lock()
		s.errs++
		s.lastErr = err
		s.mu.Unlock()
		return
	}

	at := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, family := range families {
		if !s.opts.matches(name) {
			continue
		}
		for _, metric := range family.GetMetric() {
			value, ok := metricValue(family.GetType(), metric)
			if !ok {
				continue
			}
			key := seriesID(name, metric)
			series, exists := s.series[key]
			if !exists {
				series = &targetSeries{name: name, kind: family.GetType(), labels: labelsOf(metric)}
				s.series[key] = series
			}
			series.samples = append(series.samples, targetSample{at: at, value: value})
		}
	}
}

func (s *TargetScraper) fetch(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("metrics endpoint returned HTTP %d", resp.StatusCode)
	}

	return parseExposition(resp.Body)
}

// parseExposition reads the Prometheus text format.
//
// The parser must be built with a validation scheme: its zero value leaves the
// scheme unset and panics on the first metric name. UTF-8 is the permissive
// choice, so a node publishing a legal name is never rejected.
//
// The panic guard is not about that bug. A metrics endpoint is arbitrary text
// from another process, and a parser that panics on it must not take down a
// benchmark that is otherwise measuring fine.
func parseExposition(r io.Reader) (families map[string]*dto.MetricFamily, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			families, err = nil, fmt.Errorf("the metrics endpoint could not be parsed: %v", recovered)
		}
	}()

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err = parser.TextToMetricFamilies(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the metrics endpoint: %w", err)
	}
	return families, nil
}

// Summarize reduces the scrapes to what a report needs. A counter is reported
// as its delta over the run, because its absolute value counts from when the
// node started and says nothing about this benchmark; a gauge is reported as
// its range and mean.
func (s *TargetScraper) Summarize() []types.TargetMetric {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]types.TargetMetric, 0, len(s.series))
	for _, series := range s.series {
		if len(series.samples) == 0 {
			continue
		}

		summary := types.TargetMetric{
			Name:    series.name,
			Labels:  series.labels,
			Scrapes: len(series.samples),
		}

		first := series.samples[0].value
		last := series.samples[len(series.samples)-1].value
		min, max, sum := first, first, 0.0
		for _, sample := range series.samples {
			if sample.value < min {
				min = sample.value
			}
			if sample.value > max {
				max = sample.value
			}
			sum += sample.value
		}
		summary.Min = min
		summary.Max = max
		summary.Mean = sum / float64(len(series.samples))
		summary.First = first
		summary.Last = last

		if isCounter(series.kind) {
			summary.Kind = "counter"
			summary.Delta = last - first
			if elapsed := series.samples[len(series.samples)-1].at.Sub(series.samples[0].at).Seconds(); elapsed > 0 {
				summary.PerSecond = summary.Delta / elapsed
			}
		} else {
			summary.Kind = "gauge"
		}

		out = append(out, summary)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return labelKey(out[i].Labels) < labelKey(out[j].Labels)
	})
	return out
}

// Err reports the last scrape failure and how many there were, so a report can
// say that the node-side figures are incomplete rather than presenting a gap as
// a measurement.
func (s *TargetScraper) Err() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errs, s.lastErr
}

// Series exposes the raw scrapes for republishing.
func (s *TargetScraper) Series() []types.TargetMetricPoint {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []types.TargetMetricPoint
	for _, series := range s.series {
		if len(series.samples) == 0 {
			continue
		}
		latest := series.samples[len(series.samples)-1]
		out = append(out, types.TargetMetricPoint{
			Name:   series.name,
			Kind:   kindName(series.kind),
			Labels: series.labels,
			Value:  latest.value,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return labelKey(out[i].Labels) < labelKey(out[j].Labels)
	})
	return out
}

// metricValue reduces a metric to the single number worth tracking. A histogram
// or summary is reported by its running total, which is the part that behaves
// like a counter.
func metricValue(kind dto.MetricType, metric *dto.Metric) (float64, bool) {
	switch kind {
	case dto.MetricType_COUNTER:
		if metric.GetCounter() != nil {
			return metric.GetCounter().GetValue(), true
		}
	case dto.MetricType_GAUGE:
		if metric.GetGauge() != nil {
			return metric.GetGauge().GetValue(), true
		}
	case dto.MetricType_UNTYPED:
		if metric.GetUntyped() != nil {
			return metric.GetUntyped().GetValue(), true
		}
	case dto.MetricType_SUMMARY:
		if metric.GetSummary() != nil {
			return metric.GetSummary().GetSampleSum(), true
		}
	case dto.MetricType_HISTOGRAM:
		if metric.GetHistogram() != nil {
			return metric.GetHistogram().GetSampleSum(), true
		}
	}
	return 0, false
}

func isCounter(kind dto.MetricType) bool {
	switch kind {
	case dto.MetricType_COUNTER, dto.MetricType_SUMMARY, dto.MetricType_HISTOGRAM:
		return true
	}
	return false
}

func kindName(kind dto.MetricType) string {
	if isCounter(kind) {
		return "counter"
	}
	return "gauge"
}

func labelsOf(metric *dto.Metric) map[string]string {
	if len(metric.GetLabel()) == 0 {
		return nil
	}
	out := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		out[label.GetName()] = label.GetValue()
	}
	return out
}

func seriesID(name string, metric *dto.Metric) string {
	return name + "{" + labelKey(labelsOf(metric)) + "}"
}

func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(labels[name])
	}
	return b.String()
}
