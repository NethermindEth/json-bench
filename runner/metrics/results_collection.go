package metrics

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jsonrpc-bench/runner/config"

	prometheus "github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// newPrometheusAPI builds the query API client for the configured endpoint. The
// query API lives at the base URL; the remote-write target on
// cfg.Outputs.PrometheusRW.Endpoint already has the write path appended and
// would 404 when the Prometheus client composes <base>/api/v1/query on top of
// it.
func newPrometheusAPI(cfg *config.Config) (v1.API, error) {
	queryAddr := cfg.Outputs.PrometheusRW.QueryURL
	if queryAddr == "" {
		queryAddr = cfg.Outputs.PrometheusRW.Endpoint
	}
	prometheusURL, err := url.Parse(queryAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid prometheus endpoint: %w", err)
	}
	if cfg.Outputs.PrometheusRW.BasicAuth.Username != "" && cfg.Outputs.PrometheusRW.BasicAuth.Password != "" {
		prometheusURL.User = url.UserPassword(cfg.Outputs.PrometheusRW.BasicAuth.Username, cfg.Outputs.PrometheusRW.BasicAuth.Password)
	}

	client, err := prometheus.NewClient(prometheus.Config{
		Address: prometheusURL.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus client: %w", err)
	}
	return v1.NewAPI(client), nil
}

// CheckPrometheus probes the configured Prometheus so an unusable endpoint is
// reported before the benchmark runs rather than after it. It returns nil when
// Prometheus is not configured.
func CheckPrometheus(cfg *config.Config) error {
	if cfg.Outputs == nil || cfg.Outputs.PrometheusRW == nil {
		return nil
	}
	api, err := newPrometheusAPI(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := api.Query(ctx, "vector(1)", time.Now()); err != nil {
		return fmt.Errorf("failed to query prometheus: %w", err)
	}
	return nil
}
