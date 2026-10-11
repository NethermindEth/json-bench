// Package probe runs the freshness probe for one EL(+CL) pair: it arms
// pre-emptive RPC probes for the next block, records when each first returns a
// locally plausible answer, and writes everything review needs.
package probe

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/freshness/schema"
	"github.com/jsonrpc-bench/runner/types"
)

type Config struct {
	SchemaVersion           int             `yaml:"schema_version" json:"schema_version"`
	Pair                    PairConfig      `yaml:"pair" json:"pair"`
	Chain                   ChainConfig     `yaml:"chain" json:"chain"`
	Start                   StartConfig     `yaml:"start" json:"start"`
	BlockCount              int             `yaml:"block_count" json:"block_count"`
	WarmupBlocks            int             `yaml:"warmup_blocks" json:"warmup_blocks"`
	MaxRunDurationSeconds   int             `yaml:"max_run_duration_seconds" json:"max_run_duration_seconds"`
	StallSlots              int             `yaml:"stall_slots" json:"stall_slots"`
	PollIntervalMs          int             `yaml:"poll_interval_ms" json:"poll_interval_ms"`
	LookaheadPollIntervalMs int             `yaml:"lookahead_poll_interval_ms" json:"lookahead_poll_interval_ms"`
	HeadWatchIntervalMs     int             `yaml:"head_watch_interval_ms" json:"head_watch_interval_ms"`
	RequestTimeoutMs        int             `yaml:"request_timeout_ms" json:"request_timeout_ms"`
	MaxInflightPerProbe     int             `yaml:"max_inflight_per_probe" json:"max_inflight_per_probe"`
	LogsStablePolls         int             `yaml:"logs_stable_polls" json:"logs_stable_polls"`
	Probes                  map[string]bool `yaml:"probes" json:"probes"`
	Clock                   ClockConfig     `yaml:"clock" json:"clock"`
	RTTSamples              int             `yaml:"rtt_samples" json:"rtt_samples"`
	SampleIntervalSeconds   int             `yaml:"sample_interval_seconds" json:"sample_interval_seconds"`
	OutputDirectory         string          `yaml:"output_directory" json:"output_directory"`
}

type PairConfig struct {
	ID     string            `yaml:"id" json:"id"`
	HostID string            `yaml:"host_id" json:"host_id"`
	Labels map[string]string `yaml:"labels" json:"labels,omitempty"`
	EL     ELConfig          `yaml:"el" json:"el"`
	CL     CLConfig          `yaml:"cl" json:"cl"`
}

type ELConfig struct {
	ClientRef string            `yaml:"client_ref" json:"client_ref,omitempty"`
	URL       string            `yaml:"url" json:"url"`
	Headers   map[string]string `yaml:"headers" json:"headers,omitempty"`
}

type CLConfig struct {
	BeaconURL string            `yaml:"beacon_url" json:"beacon_url,omitempty"`
	Headers   map[string]string `yaml:"headers" json:"headers,omitempty"`
	Events    *bool             `yaml:"events" json:"events,omitempty"`
}

type ChainConfig struct {
	SlotDurationSeconds uint64 `yaml:"slot_duration_seconds" json:"slot_duration_seconds"`
}

type StartConfig struct {
	Block uint64 `yaml:"block" json:"block,omitempty"`
	Time  string `yaml:"time" json:"time,omitempty"`
}

type ClockConfig struct {
	Mode            string  `yaml:"mode" json:"mode"`
	ErrorBudgetMs   float64 `yaml:"error_budget_ms" json:"error_budget_ms"`
	StepThresholdMs float64 `yaml:"step_threshold_ms" json:"step_threshold_ms"`
}

const (
	ClockModeAuto   = "auto"
	ClockModeBudget = "budget"
)

// LoadConfig reads a probe config, expanding ${VAR} references.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expanded, err := config.SubstituteEnvVars(string(raw))
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader([]byte(expanded)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.ApplyDefaults()
	return &cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = schema.Version
	}
	if c.Pair.HostID == "" {
		if h, err := os.Hostname(); err == nil {
			c.Pair.HostID = h
		}
	}
	defaultInt(&c.BlockCount, 1000)
	defaultInt(&c.MaxRunDurationSeconds, 28800)
	defaultInt(&c.StallSlots, 4)
	defaultInt(&c.PollIntervalMs, 10)
	defaultInt(&c.LookaheadPollIntervalMs, 100)
	defaultInt(&c.RequestTimeoutMs, 2000)
	defaultInt(&c.MaxInflightPerProbe, 1)
	defaultInt(&c.LogsStablePolls, 3)
	defaultInt(&c.RTTSamples, 50)
	defaultInt(&c.SampleIntervalSeconds, 30)
	if c.Probes == nil {
		c.Probes = map[string]bool{schema.ProbeStateNumber: true, schema.ProbeLogsNumber: true}
	}
	if c.Clock.Mode == "" {
		c.Clock.Mode = ClockModeAuto
	}
	if c.Clock.ErrorBudgetMs == 0 {
		c.Clock.ErrorBudgetMs = 5
	}
	if c.Clock.StepThresholdMs == 0 {
		c.Clock.StepThresholdMs = 5
	}
	if c.OutputDirectory == "" {
		c.OutputDirectory = "results/rpc-freshness"
	}
}

func defaultInt(v *int, d int) {
	if *v == 0 {
		*v = d
	}
}

// ResolveEL fills the EL URL and headers from a clients.yaml entry when
// client_ref is set. Inline url/headers take precedence over the registry.
func (c *Config) ResolveEL(registry *config.ClientRegistry) error {
	ref := c.Pair.EL.ClientRef
	if ref == "" {
		return nil
	}
	if registry == nil {
		return fmt.Errorf("pair.el.client_ref %q needs --clients", ref)
	}
	client, ok := registry.Get(ref)
	if !ok {
		return fmt.Errorf("pair.el.client_ref %q not found in the clients registry", ref)
	}
	if c.Pair.EL.URL == "" {
		c.Pair.EL.URL = client.GetBasicAuthURL()
	}
	headers := clientHeaders(client)
	for k, v := range c.Pair.EL.Headers {
		headers[k] = v
	}
	c.Pair.EL.Headers = headers
	return nil
}

func clientHeaders(client *types.ClientConfig) map[string]string {
	h := map[string]string{}
	for k, v := range client.Headers {
		h[k] = v
	}
	if client.Auth != nil {
		switch client.Auth.Type {
		case "bearer":
			h["Authorization"] = "Bearer " + client.Auth.Token
		case "api_key":
			h["X-API-Key"] = client.Auth.APIKey
		}
	}
	return h
}

func (c *Config) Validate() error {
	if c.SchemaVersion != schema.Version {
		return fmt.Errorf("schema_version %d is not supported (want %d)", c.SchemaVersion, schema.Version)
	}
	if c.Pair.ID == "" {
		return fmt.Errorf("pair.id is required")
	}
	if c.Pair.HostID == "" {
		return fmt.Errorf("pair.host_id is required (hostname lookup failed)")
	}
	if c.Pair.EL.URL == "" {
		return fmt.Errorf("pair.el.url (or pair.el.client_ref) is required")
	}
	if _, err := url.ParseRequestURI(c.Pair.EL.URL); err != nil {
		return fmt.Errorf("pair.el.url: %w", err)
	}
	if c.Pair.CL.BeaconURL != "" {
		if _, err := url.ParseRequestURI(c.Pair.CL.BeaconURL); err != nil {
			return fmt.Errorf("pair.cl.beacon_url: %w", err)
		}
	}
	known := map[string]bool{}
	for _, p := range schema.AllProbes {
		known[p] = true
	}
	enabled := 0
	for name, on := range c.Probes {
		if !known[name] {
			return fmt.Errorf("unknown probe %q", name)
		}
		if on {
			enabled++
		}
	}
	if enabled == 0 {
		return fmt.Errorf("at least one probe must be enabled")
	}
	if c.BlockCount < 1 || c.WarmupBlocks < 0 {
		return fmt.Errorf("block_count must be >= 1 and warmup_blocks >= 0")
	}
	if c.PollIntervalMs < 1 || c.LookaheadPollIntervalMs < 1 || c.RequestTimeoutMs < 1 || c.MaxInflightPerProbe < 1 {
		return fmt.Errorf("poll intervals, request timeout and max_inflight_per_probe must be positive")
	}
	if c.LogsStablePolls < 1 {
		return fmt.Errorf("logs_stable_polls must be >= 1")
	}
	if c.Clock.Mode != ClockModeAuto && c.Clock.Mode != ClockModeBudget {
		return fmt.Errorf("clock.mode must be %q or %q", ClockModeAuto, ClockModeBudget)
	}
	if c.Start.Time != "" {
		if _, err := time.Parse(time.RFC3339, c.Start.Time); err != nil {
			return fmt.Errorf("start.time must be RFC3339: %w", err)
		}
	}
	if c.Start.Block != 0 && c.Start.Time != "" {
		return fmt.Errorf("set start.block or start.time, not both")
	}
	return nil
}

// EnabledProbes returns enabled probes in report order.
func (c *Config) EnabledProbes() []string {
	var out []string
	for _, p := range schema.AllProbes {
		if c.Probes[p] {
			out = append(out, p)
		}
	}
	return out
}

// Redacted returns a copy safe to embed in artifacts: URLs keep only scheme
// and host, header values are dropped.
func (c *Config) Redacted() Config {
	r := *c
	r.Pair.Labels = copyMap(c.Pair.Labels)
	r.Probes = map[string]bool{}
	for k, v := range c.Probes {
		r.Probes[k] = v
	}
	r.Pair.EL.URL = RedactURL(c.Pair.EL.URL)
	r.Pair.EL.Headers = redactHeaders(c.Pair.EL.Headers)
	r.Pair.CL.BeaconURL = RedactURL(c.Pair.CL.BeaconURL)
	r.Pair.CL.Headers = redactHeaders(c.Pair.CL.Headers)
	return r
}

// RedactURL drops credentials, path and query, which is where API keys live
// in hosted endpoints.
func RedactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<redacted>"
	}
	out := u.Scheme + "://" + u.Host
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		out += "/<redacted>"
	}
	return out
}

func redactHeaders(h map[string]string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(h))
	for _, k := range keys {
		out[k] = "<redacted>"
	}
	return out
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
