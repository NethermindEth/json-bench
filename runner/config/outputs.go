package config

// BasicAuth represents the basic authentication credentials for a server endpoint
type BasicAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// PrometheusRW represents the remote write endpoint of a prometheus server
type PrometheusRW struct {
	Endpoint  string    `yaml:"endpoint"`
	BasicAuth BasicAuth `yaml:"basic_auth"`
}

// Outputs represents the outputs to be used by the benchmarks
type Outputs struct {
	PrometheusRW *PrometheusRW `yaml:"prometheus_rw,omitempty"`
}
