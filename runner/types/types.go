package types

// ResponseDiff represents a difference between client responses
type ResponseDiff struct {
	Method       string                 `json:"method"`
	Params       []interface{}          `json:"params"`
	Clients      []string               `json:"clients"`
	Responses    map[string]interface{} `json:"responses"` // Map of client name to response
	Differences  map[string]interface{} `json:"differences"`
	SchemaErrors map[string][]string    `json:"schema_errors,omitempty"`
	// TransportErrors and ErrorClass mirror the comparator's per-client maps so
	// the report can show a call that was never compared.
	TransportErrors map[string]string `json:"transport_errors,omitempty"`
	ErrorClass      map[string]string `json:"error_class,omitempty"`
	HasDiff         bool              `json:"has_diff"`     // Whether there are differences
	ClientNames     []string          `json:"client_names"` // Names of clients for easy access in templates
}

// MetricSummary represents performance metrics for a specific measurement
type MetricSummary struct {
	Count      int64   `json:"count"`
	Min        float64 `json:"min"`
	Max        float64 `json:"max"`
	Avg        float64 `json:"avg"`
	P50        float64 `json:"p50"`
	P75        float64 `json:"p75"`
	P90        float64 `json:"p90"`
	P95        float64 `json:"p95"`
	P99        float64 `json:"p99"`
	P999       float64 `json:"p99.9"`
	StdDev     float64 `json:"std_dev"`
	ErrorRate  float64 `json:"error_rate"`
	ErrorCount int64   `json:"error_count"`
	Throughput float64 `json:"throughput"`

	// Advanced metrics
	Variance         float64 `json:"variance"`
	Skewness         float64 `json:"skewness"`
	Kurtosis         float64 `json:"kurtosis"`
	CoeffVar         float64 `json:"coefficient_of_variation"`
	IQR              float64 `json:"iqr"` // Interquartile range
	MAD              float64 `json:"mad"` // Median absolute deviation
	SuccessRate      float64 `json:"success_rate"`
	SuccessCount     int64   `json:"success_count"`
	TimeoutRate      float64 `json:"timeout_rate"`
	ConnectionErrors int64   `json:"connection_errors"`
}

// TimeSeriesPoint represents a single data point in time series
type TimeSeriesPoint struct {
	Timestamp  int64   `json:"timestamp"`
	Value      float64 `json:"value"`
	Count      int64   `json:"count,omitempty"`
	ErrorCount int64   `json:"error_count,omitempty"`
}

// SystemMetrics represents system resource usage metrics
type SystemMetrics struct {
	CPUUsage         float64 `json:"cpu_usage_percent"`
	MemoryUsage      float64 `json:"memory_usage_mb"`
	MemoryPercent    float64 `json:"memory_percent"`
	NetworkBytesSent int64   `json:"network_bytes_sent"`
	NetworkBytesRecv int64   `json:"network_bytes_recv"`
	DiskIORead       int64   `json:"disk_io_read_bytes"`
	DiskIOWrite      int64   `json:"disk_io_write_bytes"`
	OpenConnections  int64   `json:"open_connections"`
	GoroutineCount   int     `json:"goroutine_count"`
}

// MethodMetrics represents metrics for a specific method with optional name
type MethodMetrics struct {
	MetricSummary
	Name string `json:"name,omitempty"` // Optional custom name

	// Outcomes answers which method is failing, which an aggregate error count
	// cannot.
	Outcomes map[string]int64 `json:"outcomes,omitempty"`
}

// DeliveryMetrics is the load a client was actually offered, as distinct from
// the load the config requested. Without it a run that offered a fraction of
// its target rate is indistinguishable from one that met it, and its latency
// figures describe only the requests that went out.
type DeliveryMetrics struct {
	Scheduled          int64   `json:"scheduled"`
	Sent               int64   `json:"sent"`
	Late               int64   `json:"late"`
	Dropped            int64   `json:"dropped"`
	TargetRPS          float64 `json:"target_rps"`
	AchievedRPS        float64 `json:"achieved_rps"`
	MaxDispatchDelayMs float64 `json:"max_dispatch_delay_ms"`
	InflightPeak       int     `json:"inflight_peak"`
	ElapsedSeconds     float64 `json:"elapsed_seconds"`
}

// Complete reports whether every scheduled request was sent on schedule.
func (d DeliveryMetrics) Complete() bool { return d.Dropped == 0 && d.Late == 0 }

// DeliveryRatio is the share of scheduled requests that were actually sent.
func (d DeliveryMetrics) DeliveryRatio() float64 {
	if d.Scheduled == 0 {
		return 0
	}
	return float64(d.Sent) / float64(d.Scheduled)
}

// ClientMetrics represents metrics for a specific client
type ClientMetrics struct {
	Name          string          `json:"name"`
	TotalRequests int64           `json:"total_requests"`
	TotalErrors   int64           `json:"total_errors"`
	ErrorRate     float64         `json:"error_rate"`
	Delivery      DeliveryMetrics `json:"delivery"`

	// Outcomes counts every response class, successes included, so a reader can
	// see that a run was fast because it returned nothing.
	Outcomes      map[string]int64          `json:"outcomes,omitempty"`
	Latency       MetricSummary             `json:"latency"`
	Methods       map[string]MetricSummary  `json:"methods"`
	MethodDetails map[string]*MethodMetrics `json:"method_details,omitempty"` // Method metrics with names

	// Advanced metrics
	ConnectionMetrics ConnectionMetrics            `json:"connection_metrics"`
	TimeSeries        map[string][]TimeSeriesPoint `json:"time_series"`
	SystemMetrics     []SystemMetrics              `json:"system_metrics"`
	ErrorTypes        map[string]int64             `json:"error_types"`
	StatusCodes       map[int]int64                `json:"status_codes"`
}

// ConnectionMetrics represents connection-related metrics
type ConnectionMetrics struct {
	ActiveConnections  int64   `json:"active_connections"`
	ConnectionsCreated int64   `json:"connections_created"`
	ConnectionsClosed  int64   `json:"connections_closed"`
	ConnectionReuse    float64 `json:"connection_reuse_rate"`
	AvgConnectionAge   float64 `json:"avg_connection_age_ms"`
	ConnectionPoolSize int     `json:"connection_pool_size"`
	ConnectionTimeouts int64   `json:"connection_timeouts"`
	DNSResolutionTime  float64 `json:"dns_resolution_time_ms"`
	TCPHandshakeTime   float64 `json:"tcp_handshake_time_ms"`
	TLSHandshakeTime   float64 `json:"tls_handshake_time_ms"`
}

// BenchmarkResult represents the results of a benchmark run
type BenchmarkResult struct {
	Config        interface{}               `json:"config"`
	Summary       map[string]interface{}    `json:"summary"`
	ClientMetrics map[string]*ClientMetrics `json:"client_metrics"`
	ResponseDiff  map[string]interface{}    `json:"response_diff,omitempty"`
	Timestamp     string                    `json:"timestamp"`
	ResponsesDir  string                    `json:"responses_dir,omitempty"`
	StartTime     string                    `json:"start_time"`
	EndTime       string                    `json:"end_time"`
	Duration      string                    `json:"duration"`

	// Advanced analysis
	Comparison       *ComparisonResult  `json:"comparison,omitempty"`
	PerformanceScore map[string]float64 `json:"performance_score"`
	Recommendations  []string           `json:"recommendations"`
	Environment      EnvironmentInfo    `json:"environment"`
	Manifest         RunManifest        `json:"manifest"`
}

// ComparisonResult represents comparison between clients or runs
type ComparisonResult struct {
	Winner           string             `json:"winner"`
	WinnerScore      float64            `json:"winner_score"`
	RelativePerf     map[string]float64 `json:"relative_performance"`
	SignificantDiffs []string           `json:"significant_differences"`

	// Methods holds a real pairwise test per method, computed from the retained
	// samples. It replaces a p-value matrix that was exp(-relDiff*10): a
	// p-value-shaped number with no test behind it.
	Methods []MethodComparison `json:"method_comparisons,omitempty"`
}

// MethodComparison is a two-sided Mann-Whitney U test of one method's latency
// between two clients, plus the effect size.
//
// At benchmark sample sizes almost any difference reaches significance, so
// PValue answers "is this difference real" and MedianShiftPercent answers "is
// it worth anything". Read the second first.
type MethodComparison struct {
	Method             string  `json:"method"`
	ClientA            string  `json:"client_a"`
	ClientB            string  `json:"client_b"`
	CountA             int     `json:"count_a"`
	CountB             int     `json:"count_b"`
	MedianAMs          float64 `json:"median_a_ms"`
	MedianBMs          float64 `json:"median_b_ms"`
	P99AMs             float64 `json:"p99_a_ms"`
	P99BMs             float64 `json:"p99_b_ms"`
	MedianShiftMs      float64 `json:"median_shift_ms"`
	MedianShiftPercent float64 `json:"median_shift_percent"`
	U                  float64 `json:"u"`
	Z                  float64 `json:"z"`
	PValue             float64 `json:"p_value"`
	Faster             string  `json:"faster,omitempty"`
}

// EnvironmentInfo captures system environment details. These describe the host
// the load generator ran on, which is not the host under test unless the run
// was co-located.
type EnvironmentInfo struct {
	OS            string  `json:"os"`
	Architecture  string  `json:"architecture"`
	CPUModel      string  `json:"cpu_model"`
	CPUCores      int     `json:"cpu_cores"`
	TotalMemoryGB float64 `json:"total_memory_gb"`
	GoVersion     string  `json:"go_version"`
	NetworkType   string  `json:"network_type"`
}

// RunManifest records how a run was produced. It exists so a later comparison
// can tell whether two runs are comparable at all: the error rate counts
// JSON-RPC errors, which an earlier pipeline could not see, so the same node
// measured under different semantics looks worse rather than different.
type RunManifest struct {
	Engine        string `json:"engine"`
	EngineVersion string `json:"engine_version"`

	// ErrorRateSemantics names what the error rate counts. A regression
	// detector must refuse to compare runs whose semantics differ.
	ErrorRateSemantics string `json:"error_rate_semantics"`

	TestName   string `json:"test_name"`
	ConfigPath string `json:"config_path,omitempty"`
	Seed       int64  `json:"seed"`
	Saturation string `json:"saturation_policy"`

	TargetRPS   int    `json:"target_rps,omitempty"`
	Iterations  int    `json:"iterations,omitempty"`
	Concurrency int    `json:"concurrency"`
	Duration    string `json:"duration"`

	AcceptCompression bool `json:"accept_compression"`
	ReuseConnections  bool `json:"reuse_connections"`
	HTTP2             bool `json:"http2"`

	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`

	// PreflightSkipped records that the targets were never identified, so a
	// reader knows the provenance below is only what the config claimed.
	PreflightSkipped bool `json:"preflight_skipped,omitempty"`

	Clients []ClientProvenance `json:"clients"`
}

// ClientProvenance identifies what a client actually was during the run. A
// measurement is not citable without it: "slower on eth_getLogs" says nothing
// without the version, the chain, and whether the node was synced when asked.
type ClientProvenance struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	URL  string `json:"url"`

	ClientVersion  string  `json:"client_version,omitempty"`
	ChainID        string  `json:"chain_id,omitempty"`
	HeadBlock      uint64  `json:"head_block,omitempty"`
	HeadTimestamp  string  `json:"head_timestamp,omitempty"`
	HeadAgeSeconds float64 `json:"head_age_seconds,omitempty"`
	Syncing        bool    `json:"syncing,omitempty"`

	// ProbeErrors records identity probes that failed, so a partially answered
	// target is visible rather than silently blank.
	ProbeErrors []string `json:"probe_errors,omitempty"`
}
