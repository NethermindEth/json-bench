package types

// ResponseDiff represents a difference between client responses
type ResponseDiff struct {
	Method       string                       `json:"method"`
	Params       []interface{}                `json:"params"`
	Clients      []string                     `json:"clients"`
	Responses    map[string]interface{}       `json:"responses"`      // Map of client name to response
	Differences  map[string]interface{}       `json:"differences"`
	SchemaErrors map[string][]string          `json:"schema_errors,omitempty"`
	HasDiff      bool                         `json:"has_diff"`        // Whether there are differences
	ClientNames  []string                     `json:"client_names"`    // Names of clients for easy access in templates
}

// BenchmarkResult represents the results of a benchmark run
type BenchmarkResult struct {
	Config       interface{}
	Summary      map[string]interface{}
	ResponseDiff map[string]interface{}
	Timestamp    string
	ResponsesDir string
}
