package types

// ResponseDiff represents a difference between client responses
type ResponseDiff struct {
	Method       string                 `json:"method"`
	Params       []interface{}          `json:"params"`
	Clients      []string               `json:"clients"`
	Differences  map[string]interface{} `json:"differences"`
	SchemaErrors map[string][]string    `json:"schema_errors,omitempty"`
}

// BenchmarkResult represents the results of a benchmark run
type BenchmarkResult struct {
	Config       interface{}
	Summary      map[string]interface{}
	ResponseDiff map[string]interface{}
	Timestamp    string
	ResponsesDir string
}
