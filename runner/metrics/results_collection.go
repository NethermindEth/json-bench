package metrics

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"

	prometheus "github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// CollectClientsMetrics builds per-client, per-method metrics from Prometheus
// when it is configured, and from k6's summary.json otherwise. A Prometheus
// failure is not fatal: the run falls back to summary.json rather than
// returning nothing, because empty exports read as a successful run with no
// traffic. It never returns a nil map.
func CollectClientsMetrics(cfg *config.Config, timestamp time.Time, summaryPath string, logger *logrus.Logger) (map[string]*types.ClientMetrics, error) {
	if cfg.Outputs == nil || cfg.Outputs.PrometheusRW == nil {
		return collectSummaryClientsMetrics(cfg, summaryPath, logger)
	}

	clientsMetrics, err := collectPrometheusClientsMetrics(cfg, timestamp, summaryPath, logger)
	if err == nil {
		return clientsMetrics, nil
	}
	logger.WithError(err).Warnf("Prometheus was configured but could not be queried; per-client metrics come from %s instead, and no time series will be available in Grafana", summaryPath)
	return collectSummaryClientsMetrics(cfg, summaryPath, logger)
}

// collectSummaryClientsMetrics builds client metrics without Prometheus by
// filling every client/method pair from k6's summary.json.
func collectSummaryClientsMetrics(cfg *config.Config, summaryPath string, logger *logrus.Logger) (map[string]*types.ClientMetrics, error) {
	clientsMetrics := newClientsMetricsSkeleton(cfg)
	applySummaryFallback(clientsMetrics, cfg, summaryPath, logger)
	finalizeClientMetrics(clientsMetrics, runElapsedSeconds(cfg, summaryPath, logger))
	return clientsMetrics, nil
}

func newClientsMetricsSkeleton(cfg *config.Config) map[string]*types.ClientMetrics {
	clientsMetrics := make(map[string]*types.ClientMetrics, len(cfg.ResolvedClients))
	for _, client := range cfg.ResolvedClients {
		clientsMetrics[client.Name] = &types.ClientMetrics{
			Name:              client.Name,
			Methods:           make(map[string]types.MetricSummary, len(cfg.Calls)),
			ConnectionMetrics: types.ConnectionMetrics{},
			ErrorTypes:        make(map[string]int64),
			StatusCodes:       make(map[int]int64),
			TotalRequests:     0,
			TotalErrors:       0,
			Latency: types.MetricSummary{
				Min: 9999999999,
				Max: 0,
			},
		}
	}
	return clientsMetrics
}

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
	// Set basic auth if provided
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

func collectPrometheusClientsMetrics(cfg *config.Config, timestamp time.Time, summaryPath string, logger *logrus.Logger) (map[string]*types.ClientMetrics, error) {
	clientsMetrics := newClientsMetricsSkeleton(cfg)

	api, err := newPrometheusAPI(cfg)
	if err != nil {
		return nil, err
	}

	methodTag, _ := cfg.MethodKeys()

	// Get benchmark metrics
	query, _, err := api.Query(context.Background(),
		fmt.Sprintf(`{__name__=~"k6_http_req.+",testid="%s"}`, cfg.TestName),
		timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query prometheus: %w", err)
	} else if query.Type() != model.ValVector {
		return nil, fmt.Errorf("expected vector type, got %s", query.Type())
	}

	vector := query.(model.Vector)

	// Parse prometheus metrics samples
	for _, sample := range vector {
		// Get client name
		clientName, ok := sample.Metric["scenario"]
		if !ok {
			continue
		}
		// Get metric general information
		metricName, ok := sample.Metric["__name__"]
		if !ok {
			continue
		}
		// Key on the same tag the k6 thresholds were registered under, so the
		// Prometheus and summary.json paths produce the same method rows.
		metricMethod, ok := sample.Metric[model.LabelName(methodTag)]
		if !ok {
			continue
		}
		metricValue := sample.Value
		client, ok := clientsMetrics[string(clientName)]
		if !ok { // Skip if the client is not found
			continue
		}
		method, ok := client.Methods[string(metricMethod)]
		if !ok {
			method = types.MetricSummary{}
		}
		testID, ok := sample.Metric["testid"]
		if !ok || string(testID) != cfg.TestName { // Skip if the test ID is not found or is not the current benchmark test
			continue
		}

		// Parse duration(latency) http metrics
		// Metrics named: k6_http_req_<type>_<indicator> will be parsed here
		if strings.HasPrefix(string(metricName), "k6_http_req_") {
			// Split once only: an indicator can itself contain an underscore
			// (k6 sanitizes p(99.9) to p99_9), and splitting on every "_" would
			// truncate it to "p99" and overwrite the real p99.
			metricsParts := strings.SplitN(strings.TrimPrefix(string(metricName), "k6_http_req_"), "_", 2)
			if len(metricsParts) < 2 {
				continue
			}
			metricType := metricsParts[0]
			metricIndicator := metricsParts[1]
			milliseconds := float64(metricValue) * 1000 // Prometheus return seconds and we need milliseconds

			// Parse duration(latency) http metrics
			if metricType == "duration" {
				// Parse metric indicator
				switch metricIndicator {
				case "avg":
					method.Avg = milliseconds
				case "min":
					method.Min = milliseconds
				case "med":
					method.P50 = milliseconds
				case "max":
					method.Max = milliseconds
				case "p75":
					method.P75 = milliseconds
				case "p90":
					method.P90 = milliseconds
				case "p95":
					method.P95 = milliseconds
				case "p99":
					method.P99 = milliseconds
				case "p99_9", "p99.9":
					method.P999 = milliseconds
				default: // Skip unknown metrics indicators
					continue
				}
				// Update standard deviation
				method.StdDev = calculateStdDev(method)
				if method.Avg > 0 {
					method.CoeffVar = (method.StdDev / method.Avg) * 100
				}
			} else if metricType == "blocked" || metricType == "connecting" {
				client.ConnectionMetrics.TCPHandshakeTime += milliseconds
			}
		} else if strings.EqualFold(string(metricName), "k6_http_reqs_total") { // Parse total requests metrics per tags
			errorCode, isError := sample.Metric["error_code"]
			method.Count += int64(metricValue)
			if isError {
				method.ErrorCount += int64(metricValue)
				method.ErrorRate = (float64(method.ErrorCount) / float64(method.Count)) * 100
				if code := strings.TrimSpace(string(errorCode)); code != "" {
					client.ErrorTypes[code] += int64(metricValue)
				}
			} else {
				method.SuccessCount += int64(metricValue)
				method.SuccessRate = (float64(method.SuccessCount) / float64(method.Count)) * 100
			}
			if statusLabel, ok := sample.Metric["status"]; ok {
				if code, err := strconv.Atoi(string(statusLabel)); err == nil {
					client.StatusCodes[code] += int64(metricValue)
				}
			}
		}
		// Update method metrics
		client.Methods[string(metricMethod)] = method
	}

	applySummaryFallback(clientsMetrics, cfg, summaryPath, logger)

	finalizeClientMetrics(clientsMetrics, runElapsedSeconds(cfg, summaryPath, logger))

	return clientsMetrics, nil
}

// finalizeClientMetrics recomputes per-client totals, aggregate latency and
// throughput from the collected per-method data, regardless of whether that
// data came from Prometheus or the summary.json fallback. elapsedSeconds is how
// long the run actually took; throughput is left at zero when it is unknown.
func finalizeClientMetrics(clientsMetrics map[string]*types.ClientMetrics, elapsedSeconds float64) {
	for _, client := range clientsMetrics {
		// Recalculate totals based on method data to ensure accuracy
		var totalRequests int64
		var totalErrors int64
		var totalSuccess int64

		for _, method := range client.Methods {
			totalRequests += method.Count
			totalErrors += method.ErrorCount
			totalSuccess += method.SuccessCount
		}

		// Update client totals
		if totalRequests > 0 {
			client.TotalRequests = totalRequests
			client.TotalErrors = totalErrors
			client.ErrorRate = float64(totalErrors) / float64(totalRequests) * 100
		}

		// Calculate overall latency from method latencies
		var totalLatency float64
		var totalCount int64
		var minLatency, maxLatency float64 = 999999, 0
		var p50Sum, p90Sum, p95Sum, p99Sum float64
		var methodCount int

		for methodName, method := range client.Methods {
			totalLatency += method.Avg * float64(method.Count)
			totalCount += method.Count
			p50Sum += method.P50
			p90Sum += method.P90
			p95Sum += method.P95
			p99Sum += method.P99
			methodCount++

			if method.Min < minLatency {
				minLatency = method.Min
			}
			if method.Max > maxLatency {
				maxLatency = method.Max
			}

			// Throughput is requests over elapsed time. k6's own per-method
			// rate is preferred (set by the summary fallback); this covers the
			// Prometheus path, which carries counters but no rate.
			if method.Throughput == 0 && method.Count > 0 && elapsedSeconds > 0 {
				method.Throughput = float64(method.Count) / elapsedSeconds
				client.Methods[methodName] = method
			}
		}

		if totalCount > 0 {
			client.Latency.Avg = totalLatency / float64(totalCount)
			client.Latency.Min = minLatency
			client.Latency.Max = maxLatency
			if methodCount > 0 {
				client.Latency.P50 = p50Sum / float64(methodCount)
				client.Latency.P90 = p90Sum / float64(methodCount)
				client.Latency.P95 = p95Sum / float64(methodCount)
				client.Latency.P99 = p99Sum / float64(methodCount)
			}
			if elapsedSeconds > 0 {
				client.Latency.Throughput = float64(totalCount) / elapsedSeconds
			}
		}
	}
}

// calculateStdDev is a crude range/4 stand-in: Prometheus stores k6's
// aggregates, not the sample values a real standard deviation needs.
func calculateStdDev(values types.MetricSummary) float64 {
	return (values.Max - values.Min) / 4
}
