package types

// ResponseDiff represents a difference between client responses
type ResponseDiff struct {
	Method       string                 `json:"method"`
	Params       []interface{}          `json:"params"`
	Clients      []string               `json:"clients"`
	Responses    map[string]interface{} `json:"responses"` // Map of client name to response
	Differences  map[string]interface{} `json:"differences"`
	SchemaErrors map[string][]string    `json:"schema_errors,omitempty"`
	HasDiff      bool                   `json:"has_diff"`     // Whether there are differences
	ClientNames  []string               `json:"client_names"` // Names of clients for easy access in templates
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
}

// ClientMetrics represents metrics for a specific client
type ClientMetrics struct {
	Name          string                    `json:"name"`
	TotalRequests int64                     `json:"total_requests"`
	TotalErrors   int64                     `json:"total_errors"`
	ErrorRate     float64                   `json:"error_rate"`
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
}

// ComparisonResult represents comparison between clients or runs
type ComparisonResult struct {
	Winner           string                        `json:"winner"`
	WinnerScore      float64                       `json:"winner_score"`
	RelativePerf     map[string]float64            `json:"relative_performance"`
	SignificantDiffs []string                      `json:"significant_differences"`
	PValueMatrix     map[string]map[string]float64 `json:"p_value_matrix"`
}

// EnvironmentInfo captures system environment details
type EnvironmentInfo struct {
	OS            string  `json:"os"`
	Architecture  string  `json:"architecture"`
	CPUModel      string  `json:"cpu_model"`
	CPUCores      int     `json:"cpu_cores"`
	TotalMemoryGB float64 `json:"total_memory_gb"`
	GoVersion     string  `json:"go_version"`
	K6Version     string  `json:"k6_version"`
	NetworkType   string  `json:"network_type"`
}
