package metrics

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"

	prometheus "github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

func CollectClientsMetrics(cfg *config.Config, timestamp time.Time, summaryPath string) (map[string]*types.ClientMetrics, error) {
	// Gather results from prometheus
	if cfg.Outputs.PrometheusRW != nil {
		return collectPrometheusClientsMetrics(cfg, timestamp, summaryPath)
	}
	// No valid outputs configured
	return nil, fmt.Errorf("no outputs configured")
}

func collectPrometheusClientsMetrics(cfg *config.Config, timestamp time.Time, summaryPath string) (map[string]*types.ClientMetrics, error) {
	clientsMetrics := make(map[string]*types.ClientMetrics, len(cfg.Calls))

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

	// Parse prometheus endpoint
	prometheusURL, err := url.Parse(cfg.Outputs.PrometheusRW.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid prometheus endpoint: %w", err)
	}
	// Set basic auth if provided
	if cfg.Outputs.PrometheusRW.BasicAuth.Username != "" && cfg.Outputs.PrometheusRW.BasicAuth.Password != "" {
		prometheusURL.User = url.UserPassword(cfg.Outputs.PrometheusRW.BasicAuth.Username, cfg.Outputs.PrometheusRW.BasicAuth.Password)
	}

	// Create prometheus http api client
	client, err := prometheus.NewClient(prometheus.Config{
		Address: prometheusURL.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus client: %w", err)
	}
	api := v1.NewAPI(client)

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
		metricMethod, ok := sample.Metric["req_name"]
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
			metricsParts := strings.Split(strings.TrimPrefix(string(metricName), "k6_http_req_"), "_")
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
				case "p90":
					method.P90 = milliseconds
				case "p95":
					method.P95 = milliseconds
				case "p99":
					method.P99 = milliseconds
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
			_, isError := sample.Metric["error_code"]
			method.Count += int64(metricValue)
			if isError {
				method.ErrorCount += int64(metricValue)
				// Update rates with latest available values
				method.ErrorRate = (float64(method.ErrorCount) / float64(method.Count)) * 100
			} else {
				method.SuccessCount += int64(metricValue)
				// Update rates with latest available values
				method.SuccessRate = (float64(method.SuccessCount) / float64(method.Count)) * 100
			}
		}
		// Update method metrics
		client.Methods[string(metricMethod)] = method
	}

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

			// Calculate throughput for each method
			if method.Avg > 0 {
				// FIXME: Should throughput use directly cfg.RPS?
				method.Throughput = 1000.0 / method.Avg // requests per second
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
			// Calculate overall throughput based on average latency
			if client.Latency.Avg > 0 {
				client.Latency.Throughput = 1000.0 / client.Latency.Avg // requests per second
			}
		}
	}

	return clientsMetrics, nil
}

func calculateStdDev(values types.MetricSummary) float64 {
	return (values.Max - values.Min) / 4
}
