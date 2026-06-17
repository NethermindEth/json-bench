package config

// BasicAuth represents the basic authentication credentials for a server endpoint
type BasicAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// PrometheusRW represents the remote write endpoint of a prometheus server
// together with the base URL used to issue read queries against the same
// instance after a benchmark run.
type PrometheusRW struct {
	Endpoint  string    `yaml:"endpoint"`
	QueryURL  string    `yaml:"query_url"`
	BasicAuth BasicAuth `yaml:"basic_auth"`
}

// Outputs represents the outputs to be used by the benchmarks
type Outputs struct {
	PrometheusRW *PrometheusRW `yaml:"prometheus_rw,omitempty"`
}
