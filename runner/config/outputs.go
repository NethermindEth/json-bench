package config

import (
	"fmt"
	"strings"
)

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

func (p *PrometheusRW) GetRWEndpoint() string {
	endpoint := strings.TrimSuffix(p.Endpoint, "/")
	return fmt.Sprintf("%s/api/v1/write", endpoint)
}

// Outputs represents the outputs to be used by the benchmarks
type Outputs struct {
	PrometheusRW *PrometheusRW `yaml:"prometheus_rw,omitempty"`
}
