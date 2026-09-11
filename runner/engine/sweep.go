package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// SweepDimension is a config field a sweep can vary.
type SweepDimension string

const (
	SweepRPS       SweepDimension = "rps"
	SweepVUs       SweepDimension = "vus"
	SweepBatchSize SweepDimension = "batch_size"
	SweepDuration  SweepDimension = "duration"
)

// SweepDimensions lists what can be varied, for error messages and help text.
var SweepDimensions = []SweepDimension{SweepRPS, SweepVUs, SweepBatchSize, SweepDuration}

// ParseSweepDimension resolves the name a user typed.
func ParseSweepDimension(name string) (SweepDimension, error) {
	for _, d := range SweepDimensions {
		if string(d) == name {
			return d, nil
		}
	}
	names := make([]string, 0, len(SweepDimensions))
	for _, d := range SweepDimensions {
		names = append(names, string(d))
	}
	return "", fmt.Errorf("cannot vary %q; the sweepable settings are %s", name, strings.Join(names, ", "))
}

// apply sets the dimension on a config copy, returning the label for the run.
func (d SweepDimension) apply(cfg *config.Config, value string) (string, error) {
	switch d {
	case SweepRPS, SweepVUs, SweepBatchSize:
		n, err := strconv.Atoi(value)
		if err != nil {
			return "", fmt.Errorf("%s must be a whole number, got %q", d, value)
		}
		if n < 0 {
			return "", fmt.Errorf("%s cannot be negative", d)
		}
		switch d {
		case SweepRPS:
			// A rate sweep is a sweep of arrival rates, so the iteration mode
			// has to give way or the rate would be ignored.
			cfg.RPS, cfg.Iterations, cfg.Stages = n, 0, nil
		case SweepVUs:
			cfg.VUs = n
		case SweepBatchSize:
			cfg.BatchSize = n
		}
	case SweepDuration:
		if _, err := time.ParseDuration(value); err != nil {
			return "", fmt.Errorf("duration %q is not a duration: %w", value, err)
		}
		cfg.Duration, cfg.Iterations, cfg.Stages = value, 0, nil
	default:
		return "", fmt.Errorf("unsweepable dimension %q", d)
	}
	return fmt.Sprintf("%s=%s", d, value), nil
}

// SweepOptions configures a sweep.
type SweepOptions struct {
	Dimension SweepDimension
	Values    []string

	// Repeat runs the whole sweep more than once. A single pass cannot tell a
	// real difference between two settings from the drift of the target
	// underneath them, which repeated passes expose as each point's spread.
	Repeat int

	// Settle waits between runs so one run's queue does not spill into the next.
	Settle time.Duration

	OutputDir string
}

// SweepPoint is one setting's outcome, across every pass.
type SweepPoint struct {
	Label string `json:"label"`
	Value string `json:"value"`

	Passes []SweepPass `json:"passes"`

	// Median stats across passes, which is what the table reports.
	P50Ms         float64 `json:"p50_ms"`
	P95Ms         float64 `json:"p95_ms"`
	P99Ms         float64 `json:"p99_ms"`
	ErrorRate     float64 `json:"error_rate_percent"`
	AchievedRPS   float64 `json:"achieved_rps"`
	DeliveredPct  float64 `json:"delivered_percent"`
	Requests      int64   `json:"requests"`
	SpreadP99Pct  float64 `json:"p99_spread_percent"`
	Inconclusive  bool    `json:"inconclusive"`
	InconclusiveB string  `json:"inconclusive_because,omitempty"`
}

// SweepPass is one run of one setting.
type SweepPass struct {
	Pass        int     `json:"pass"`
	Dir         string  `json:"dir"`
	P50Ms       float64 `json:"p50_ms"`
	P95Ms       float64 `json:"p95_ms"`
	P99Ms       float64 `json:"p99_ms"`
	ErrorRate   float64 `json:"error_rate_percent"`
	AchievedRPS float64 `json:"achieved_rps"`
	Delivered   float64 `json:"delivered_percent"`
	Requests    int64   `json:"requests"`
}

// SweepResult is the whole sweep.
type SweepResult struct {
	Dimension string            `json:"dimension"`
	Client    string            `json:"client"`
	Repeat    int               `json:"repeat"`
	Points    []SweepPoint      `json:"points"`
	Manifest  types.RunManifest `json:"manifest"`
	Warnings  []string          `json:"warnings,omitempty"`
}

// Sweep runs one config at several settings of one dimension and reports them
// side by side.
//
// Every run replays the same request sequence: the sweep resolves it once up
// front and pins it, because otherwise each setting would be measured against
// different traffic and the comparison would be of workloads rather than
// settings.
func Sweep(ctx context.Context, cfg *config.Config, opts Options, sweep SweepOptions) (*SweepResult, error) {
	if len(sweep.Values) < 2 {
		return nil, fmt.Errorf("a sweep needs at least two values to compare")
	}
	if sweep.Repeat <= 0 {
		sweep.Repeat = 1
	}
	log := opts.Logger
	if log == nil {
		log = logrus.New()
	}

	variants, err := sweepVariants(cfg, sweep)
	if err != nil {
		return nil, err
	}
	callsFile, err := pinSequence(cfg, variants, sweep.OutputDir)
	if err != nil {
		return nil, err
	}

	result := &SweepResult{
		Dimension: string(sweep.Dimension),
		Repeat:    sweep.Repeat,
	}
	if len(cfg.ResolvedClients) > 0 {
		result.Client = cfg.ResolvedClients[0].Name
	}
	if sweep.Repeat == 1 && len(sweep.Values) > 1 {
		result.Warnings = append(result.Warnings,
			"a single pass cannot separate a difference between settings from the target drifting "+
				"underneath them; --repeat runs the sweep again and reports each point's spread")
	}

	points := make(map[string]*SweepPoint, len(sweep.Values))
	order := make([]string, 0, len(sweep.Values))

	for pass := 1; pass <= sweep.Repeat; pass++ {
		for _, value := range sweep.Values {
			variant := variants[value]
			runCfg := *variant.cfg
			runCfg.CallsFile = callsFile
			label := variant.label

			point, ok := points[value]
			if !ok {
				point = &SweepPoint{Label: label, Value: value}
				points[value] = point
				order = append(order, value)
			}

			runOpts := opts
			runOpts.OutputDir = filepath.Join(sweep.OutputDir, sweepRunDir(label, pass, sweep.Repeat))

			log.Infof("sweep %s (pass %d of %d)", label, pass, sweep.Repeat)
			runResult, _, err := Run(ctx, &runCfg, runOpts)
			if err != nil {
				return result, fmt.Errorf("the run at %s failed: %w", label, err)
			}
			if result.Manifest.Engine == "" {
				result.Manifest = runResult.Manifest
			} else if changed := targetChanged(result.Manifest, runResult.Manifest); changed != "" {
				// A sweep compares settings against one target. If the target
				// changed underneath it, the differences between points are not
				// the settings.
				result.Warnings = append(result.Warnings, changed)
				log.Warn(changed)
			}
			point.Passes = append(point.Passes, summarisePass(pass, runOpts.OutputDir, runResult, result.Client))

			if sweep.Settle > 0 {
				select {
				case <-ctx.Done():
					return result, ctx.Err()
				case <-time.After(sweep.Settle):
				}
			}
		}
	}

	for _, value := range order {
		point := points[value]
		finalisePoint(point)
		result.Points = append(result.Points, *point)
	}
	return result, nil
}

type sweepVariant struct {
	cfg   *config.Config
	label string
}

func sweepVariants(cfg *config.Config, sweep SweepOptions) (map[string]sweepVariant, error) {
	out := make(map[string]sweepVariant, len(sweep.Values))
	for _, value := range sweep.Values {
		variant := *cfg
		label, err := sweep.Dimension.apply(&variant, value)
		if err != nil {
			return nil, err
		}
		copied := variant
		out[value] = sweepVariant{cfg: &copied, label: label}
	}
	return out, nil
}

// pinSequence gives every run in the sweep the same requests. Drawing a fresh
// sequence per setting would measure the settings against different traffic, so
// the sweep would compare workloads rather than settings.
//
// The sequence is sized for whichever setting needs the most requests: a run
// that exhausts it would quietly stop short, which is indistinguishable in the
// output from a setting that was simply faster.
func pinSequence(cfg *config.Config, variants map[string]sweepVariant, outputDir string) (string, error) {
	if cfg.CallsFile != "" {
		return cfg.CallsFile, nil
	}

	var widest *config.Config
	longest := -1
	for _, variant := range variants {
		n, err := sequenceLength(variant.cfg)
		if err != nil {
			return "", err
		}
		if n > longest {
			longest, widest = n, variant.cfg
		}
	}
	if widest == nil {
		return "", fmt.Errorf("the sweep has no settings to run")
	}

	requests, err := BuildSequence(widest)
	if err != nil {
		return "", fmt.Errorf("failed to build the shared request sequence: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create the sweep output directory: %w", err)
	}
	path := filepath.Join(outputDir, "requests.csv")
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("failed to write the shared request sequence: %w", err)
	}
	defer file.Close()
	if err := WriteSequenceCSV(file, requests); err != nil {
		return "", fmt.Errorf("failed to write the shared request sequence: %w", err)
	}
	return path, nil
}

// targetChanged reports whether the endpoint identified itself differently than
// it did at the start of the sweep.
func targetChanged(first, current types.RunManifest) string {
	was := map[string]types.ClientProvenance{}
	for _, c := range first.Clients {
		was[c.Name] = c
	}
	for _, c := range current.Clients {
		before, ok := was[c.Name]
		if !ok {
			continue
		}
		if before.ClientVersion != "" && c.ClientVersion != "" && before.ClientVersion != c.ClientVersion {
			return fmt.Sprintf("%s reported %s at the start of the sweep and %s now; the points are "+
				"not all measuring the same target", c.Name, before.ClientVersion, c.ClientVersion)
		}
		if before.ChainID != "" && c.ChainID != "" && before.ChainID != c.ChainID {
			return fmt.Sprintf("%s changed chain (%s to %s) during the sweep", c.Name, before.ChainID, c.ChainID)
		}
	}
	return ""
}

func sweepRunDir(label string, pass, repeat int) string {
	dir := strings.ReplaceAll(label, "=", "-")
	if repeat > 1 {
		return fmt.Sprintf("%s/pass-%d", dir, pass)
	}
	return dir
}

func summarisePass(pass int, dir string, result *types.BenchmarkResult, client string) SweepPass {
	out := SweepPass{Pass: pass, Dir: dir}
	metrics := pickClient(result, client)
	if metrics == nil {
		return out
	}
	out.P50Ms, out.P95Ms, out.P99Ms = metrics.Latency.P50, metrics.Latency.P95, metrics.Latency.P99
	out.ErrorRate = metrics.ErrorRate
	out.Requests = metrics.TotalRequests
	out.Delivered = metrics.Delivery.DeliveryRatio() * 100

	// Requests per second, not dispatches per second. Delivery counts arrivals,
	// and a batched arrival carries several calls — reporting those would make
	// batching look like a throughput collapse when it is the opposite.
	if offered := metrics.Delivery.OfferedSeconds; offered > 0 {
		out.AchievedRPS = float64(metrics.TotalRequests) / offered
	} else {
		out.AchievedRPS = metrics.Delivery.AchievedRPS
	}
	return out
}

func pickClient(result *types.BenchmarkResult, client string) *types.ClientMetrics {
	if metrics, ok := result.ClientMetrics[client]; ok {
		return metrics
	}
	for _, metrics := range result.ClientMetrics {
		return metrics
	}
	return nil
}

// finalisePoint reduces a point's passes to the median, and records the spread
// so a reader can see whether the differences between settings are larger than
// the noise within one.
func finalisePoint(point *SweepPoint) {
	if len(point.Passes) == 0 {
		return
	}
	point.P50Ms = medianOf(point.Passes, func(p SweepPass) float64 { return p.P50Ms })
	point.P95Ms = medianOf(point.Passes, func(p SweepPass) float64 { return p.P95Ms })
	point.P99Ms = medianOf(point.Passes, func(p SweepPass) float64 { return p.P99Ms })
	point.ErrorRate = medianOf(point.Passes, func(p SweepPass) float64 { return p.ErrorRate })
	point.AchievedRPS = medianOf(point.Passes, func(p SweepPass) float64 { return p.AchievedRPS })
	point.DeliveredPct = medianOf(point.Passes, func(p SweepPass) float64 { return p.Delivered })
	for _, pass := range point.Passes {
		point.Requests += pass.Requests
	}

	if len(point.Passes) > 1 && point.P99Ms > 0 {
		lo, hi := point.Passes[0].P99Ms, point.Passes[0].P99Ms
		for _, pass := range point.Passes {
			lo = min(lo, pass.P99Ms)
			hi = max(hi, pass.P99Ms)
		}
		point.SpreadP99Pct = (hi - lo) / point.P99Ms * 100
	}

	// A setting the generator could not actually offer measures the generator,
	// not the target, which is the same trap find-max-rps guards against.
	if point.DeliveredPct < 99 {
		point.Inconclusive = true
		point.InconclusiveB = fmt.Sprintf("only %.1f%% of the requested load was delivered, so this "+
			"point measures the generator rather than the target", point.DeliveredPct)
	}
}

func medianOf(passes []SweepPass, pick func(SweepPass) float64) float64 {
	values := make([]float64, 0, len(passes))
	for _, p := range passes {
		values = append(values, pick(p))
	}
	sort.Float64s(values)
	n := len(values)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return values[n/2]
	}
	return (values[n/2-1] + values[n/2]) / 2
}
