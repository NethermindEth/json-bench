package types

import (
	"time"
)

// TimeSeriesMetric represents a time-series metric for Grafana compatibility
type TimeSeriesMetric struct {
	Time       time.Time         `json:"time" db:"time"`
	RunID      string            `json:"run_id" db:"run_id"`
	Client     string            `json:"client" db:"client"`
	Method     string            `json:"method" db:"method"`
	MetricName string            `json:"metric_name" db:"metric_name"`
	Value      float64           `json:"value" db:"value"`
	Tags       map[string]string `json:"tags" db:"tags"`
}

// Metric types for Grafana time-series storage
const (
	MetricLatencyP50  = "latency_p50"
	MetricLatencyP75  = "latency_p75"
	MetricLatencyP90  = "latency_p90"
	MetricLatencyP95  = "latency_p95"
	MetricLatencyP99  = "latency_p99"
	MetricLatencyP999 = "latency_p999"
	MetricLatencyMin  = "latency_min"
	MetricLatencyMax  = "latency_max"
	MetricLatencyAvg  = "latency_avg"
	MetricSuccessRate = "success_rate"
	MetricErrorRate   = "error_rate"
	MetricThroughput  = "throughput"
	MetricCPUUsage    = "cpu_usage"
	MetricMemoryUsage = "memory_usage"
)

// GrafanaResponse represents the response format for Grafana queries
type GrafanaResponse struct {
	Target     string                 `json:"target"`
	DataPoints []GrafanaDataPoint     `json:"datapoints"`
	Columns    []GrafanaColumn        `json:"columns,omitempty"`
	Rows       [][]interface{}        `json:"rows,omitempty"`
	Type       string                 `json:"type,omitempty"`
	Tags       map[string]string      `json:"tags,omitempty"`
	Text       string                 `json:"text,omitempty"`
	Title      string                 `json:"title,omitempty"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
}

// GrafanaDataPoint represents a single data point for time series
type GrafanaDataPoint [2]interface{} // [value, timestamp_ms]

// GrafanaColumn represents a column definition for table format
type GrafanaColumn struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

// GrafanaTableResponse represents table format response for Grafana
type GrafanaTableResponse struct {
	Columns []GrafanaColumn `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	Type    string          `json:"type"`
}

// GrafanaQuery represents a query from Grafana
type GrafanaQuery struct {
	Target    string                 `json:"target"`
	RefID     string                 `json:"refId"`
	Hide      bool                   `json:"hide"`
	Type      string                 `json:"type"`
	MaxPoints int                    `json:"maxDataPoints"`
	Interval  string                 `json:"interval"`
	Format    string                 `json:"format"`
	RawSQL    string                 `json:"rawSql"`
	Filters   []GrafanaFilter        `json:"adhocFilters"`
	Payload   map[string]interface{} `json:"payload"`
}

// GrafanaFilter represents an ad-hoc filter from Grafana
type GrafanaFilter struct {
	Key      string `json:"key"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// GrafanaTimeRange represents the time range for a query
type GrafanaTimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
	Raw  struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"raw"`
}

// GrafanaSearchRequest represents a search request for available metrics
type GrafanaSearchRequest struct {
	Target string `json:"target"`
	Type   string `json:"type"`
}

// GrafanaAnnotationRequest represents a request for annotations
type GrafanaAnnotationRequest struct {
	Range      GrafanaTimeRange `json:"range"`
	Annotation struct {
		Name       string `json:"name"`
		Datasource string `json:"datasource"`
		Enable     bool   `json:"enable"`
		IconColor  string `json:"iconColor"`
		Query      string `json:"query"`
	} `json:"annotation"`
}

// GrafanaAnnotation represents an annotation for marking events
type GrafanaAnnotation struct {
	Time     int64             `json:"time"`
	TimeEnd  int64             `json:"timeEnd,omitempty"`
	Title    string            `json:"title"`
	Text     string            `json:"text"`
	Tags     []string          `json:"tags"`
	IsRegion bool              `json:"isRegion"`
	Source   map[string]string `json:"source,omitempty"`
}

// MetricAggregation represents aggregated metric data
type MetricAggregation struct {
	MetricName string    `json:"metric_name"`
	Client     string    `json:"client"`
	Method     string    `json:"method,omitempty"`
	Period     string    `json:"period"` // hour, day, week, month
	Timestamp  time.Time `json:"timestamp"`
	Count      int64     `json:"count"`
	Sum        float64   `json:"sum"`
	Min        float64   `json:"min"`
	Max        float64   `json:"max"`
	Avg        float64   `json:"avg"`
	P50        float64   `json:"p50"`
	P90        float64   `json:"p90"`
	P95        float64   `json:"p95"`
	P99        float64   `json:"p99"`
	StdDev     float64   `json:"stddev"`
}

// MetricTags represents common tags for metrics
type MetricTags struct {
	GitCommit     string `json:"git_commit,omitempty"`
	GitBranch     string `json:"git_branch,omitempty"`
	TestName      string `json:"test_name,omitempty"`
	Environment   string `json:"environment,omitempty"`
	Version       string `json:"version,omitempty"`
	Configuration string `json:"configuration,omitempty"`
}

// ToMap converts MetricTags to map[string]string
func (mt MetricTags) ToMap() map[string]string {
	tags := make(map[string]string)
	if mt.GitCommit != "" {
		tags["git_commit"] = mt.GitCommit
	}
	if mt.GitBranch != "" {
		tags["git_branch"] = mt.GitBranch
	}
	if mt.TestName != "" {
		tags["test_name"] = mt.TestName
	}
	if mt.Environment != "" {
		tags["environment"] = mt.Environment
	}
	if mt.Version != "" {
		tags["version"] = mt.Version
	}
	if mt.Configuration != "" {
		tags["configuration"] = mt.Configuration
	}
	return tags
}

// NewTimeSeriesMetric creates a new time-series metric
func NewTimeSeriesMetric(runID, client, method, metricName string, value float64, tags MetricTags) TimeSeriesMetric {
	return TimeSeriesMetric{
		Time:       time.Now(),
		RunID:      runID,
		Client:     client,
		Method:     method,
		MetricName: metricName,
		Value:      value,
		Tags:       tags.ToMap(),
	}
}

// CreateGrafanaDataPoint creates a Grafana-compatible data point
func CreateGrafanaDataPoint(value float64, timestamp time.Time) GrafanaDataPoint {
	return GrafanaDataPoint{value, timestamp.UnixMilli()}
}

// CreateGrafanaTimeSeries creates a Grafana time series response
func CreateGrafanaTimeSeries(target string, metrics []TimeSeriesMetric) GrafanaResponse {
	dataPoints := make([]GrafanaDataPoint, len(metrics))
	for i, metric := range metrics {
		dataPoints[i] = CreateGrafanaDataPoint(metric.Value, metric.Time)
	}

	return GrafanaResponse{
		Target:     target,
		DataPoints: dataPoints,
		Type:       "timeseries",
	}
}

// CreateGrafanaTable creates a Grafana table response
func CreateGrafanaTable(columns []string, rows [][]interface{}) GrafanaTableResponse {
	grafanaColumns := make([]GrafanaColumn, len(columns))
	for i, col := range columns {
		grafanaColumns[i] = GrafanaColumn{
			Text: col,
			Type: "string", // Default to string, can be enhanced
		}
	}

	return GrafanaTableResponse{
		Columns: grafanaColumns,
		Rows:    rows,
		Type:    "table",
	}
}

// CreateBaselineAnnotation creates an annotation for baseline markers
func CreateBaselineAnnotation(timestamp time.Time, baselineName, runID string) GrafanaAnnotation {
	return GrafanaAnnotation{
		Time:  timestamp.UnixMilli(),
		Title: "Baseline: " + baselineName,
		Text:  "Baseline set for run " + runID,
		Tags:  []string{"baseline", baselineName},
		Source: map[string]string{
			"type":   "baseline",
			"run_id": runID,
			"name":   baselineName,
		},
	}
}

// CreateRegressionAnnotation creates an annotation for regression markers
func CreateRegressionAnnotation(timestamp time.Time, severity, metric, client string) GrafanaAnnotation {
	return GrafanaAnnotation{
		Time:  timestamp.UnixMilli(),
		Title: "Regression: " + severity,
		Text:  "Performance regression detected in " + metric + " for " + client,
		Tags:  []string{"regression", severity, client},
		Source: map[string]string{
			"type":     "regression",
			"severity": severity,
			"metric":   metric,
			"client":   client,
		},
	}
}
