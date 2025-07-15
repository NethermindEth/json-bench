package generator

import (
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// EnhancedHTMLReportTemplate is the enhanced template for generating HTML reports
const EnhancedHTMLReportTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.TestName}} - JSON-RPC Benchmark Report</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            line-height: 1.6;
            color: #333;
            max-width: 1400px;
            margin: 0 auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            background-color: white;
            border-radius: 8px;
            padding: 30px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            margin-bottom: 20px;
        }
        h1, h2, h3 {
            color: #2c3e50;
        }
        h1 {
            border-bottom: 3px solid #3498db;
            padding-bottom: 10px;
        }
        .summary {
            background-color: #f8f9fa;
            border-radius: 5px;
            padding: 20px;
            margin-bottom: 20px;
            border-left: 4px solid #3498db;
        }
        .summary-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 15px;
            margin-top: 15px;
        }
        .summary-item {
            background-color: white;
            padding: 15px;
            border-radius: 5px;
            text-align: center;
            box-shadow: 0 1px 3px rgba(0,0,0,0.1);
        }
        .summary-value {
            font-size: 24px;
            font-weight: bold;
            color: #3498db;
        }
        .summary-label {
            font-size: 14px;
            color: #666;
            margin-top: 5px;
        }
        table {
            width: 100%;
            border-collapse: collapse;
            margin-bottom: 20px;
        }
        th, td {
            padding: 12px 15px;
            text-align: left;
            border-bottom: 1px solid #ddd;
        }
        th {
            background-color: #f2f2f2;
            font-weight: 600;
        }
        tr:hover {
            background-color: #f5f5f5;
        }
        .metric-cell {
            font-family: 'Courier New', monospace;
        }
        .chart-container {
            height: 400px;
            margin-bottom: 30px;
            background-color: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 1px 3px rgba(0,0,0,0.1);
        }
        .charts-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(500px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        .badge {
            display: inline-block;
            padding: 3px 7px;
            border-radius: 3px;
            font-size: 12px;
            font-weight: bold;
        }
        .badge-success {
            background-color: #28a745;
            color: white;
        }
        .badge-warning {
            background-color: #ffc107;
            color: #212529;
        }
        .badge-danger {
            background-color: #dc3545;
            color: white;
        }
        .client-section {
            margin-bottom: 40px;
            background-color: #f9f9f9;
            border-radius: 8px;
            padding: 20px;
        }
        .client-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 20px;
        }
        .client-name {
            font-size: 20px;
            font-weight: bold;
            color: #2c3e50;
        }
        .percentile-table {
            background-color: white;
            border-radius: 5px;
            overflow: hidden;
            box-shadow: 0 1px 3px rgba(0,0,0,0.1);
        }
        .footer {
            margin-top: 40px;
            padding-top: 20px;
            border-top: 1px solid #eee;
            text-align: center;
            font-size: 14px;
            color: #777;
        }
        .tab-container {
            margin-bottom: 30px;
        }
        .tabs {
            display: flex;
            border-bottom: 2px solid #ddd;
            margin-bottom: 20px;
        }
        .tab {
            padding: 10px 20px;
            cursor: pointer;
            background-color: #f5f5f5;
            border: 1px solid #ddd;
            border-bottom: none;
            margin-right: 5px;
            border-radius: 5px 5px 0 0;
            transition: background-color 0.3s;
        }
        .tab:hover {
            background-color: #e9e9e9;
        }
        .tab.active {
            background-color: white;
            border-bottom: 2px solid white;
            margin-bottom: -2px;
        }
        .tab-content {
            display: none;
            animation: fadeIn 0.3s;
        }
        .tab-content.active {
            display: block;
        }
        @keyframes fadeIn {
            from { opacity: 0; }
            to { opacity: 1; }
        }
    </style>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
</head>
<body>
    <div class="container">
        <h1>{{.TestName}} - JSON-RPC Benchmark Report</h1>
        <p>{{.Description}}</p>
        
        <div class="summary">
            <h2>Test Summary</h2>
            <div class="summary-grid">
                <div class="summary-item">
                    <div class="summary-value">{{.StartTime}}</div>
                    <div class="summary-label">Start Time</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value">{{.Duration}}</div>
                    <div class="summary-label">Duration</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value">{{.RPS}}</div>
                    <div class="summary-label">Target RPS</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value">{{.TotalRequests}}</div>
                    <div class="summary-label">Total Requests</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value">{{.OverallErrorRate}}%</div>
                    <div class="summary-label">Overall Error Rate</div>
                </div>
            </div>
        </div>
    </div>

    <div class="container">
        <h2>Performance Overview</h2>
        
        <div class="charts-grid">
            <div class="chart-container">
                <canvas id="latencyChart"></canvas>
            </div>
            <div class="chart-container">
                <canvas id="throughputChart"></canvas>
            </div>
        </div>
        
        <div class="charts-grid">
            <div class="chart-container">
                <canvas id="errorRateChart"></canvas>
            </div>
            <div class="chart-container">
                <canvas id="percentileChart"></canvas>
            </div>
        </div>
    </div>

    <div class="container">
        <h2>Client Performance Details</h2>
        
        <div class="tab-container">
            <div class="tabs">
                {{range $index, $client := .ClientMetrics}}
                <div class="tab {{if eq $index 0}}active{{end}}" onclick="showTab('client-{{$client.Name}}', this)">
                    {{$client.Name}}
                </div>
                {{end}}
            </div>
            
            {{range $index, $client := .ClientMetrics}}
            <div id="client-{{$client.Name}}" class="tab-content {{if eq $index 0}}active{{end}}">
                <div class="client-section">
                    <div class="client-header">
                        <div class="client-name">{{$client.Name}}</div>
                        <div>
                            Total Requests: <strong>{{$client.TotalRequests}}</strong> | 
                            Error Rate: <strong>{{printf "%.2f" $client.ErrorRate}}%</strong>
                        </div>
                    </div>
                    
                    <h3>Method Performance</h3>
                    <table class="percentile-table">
                        <thead>
                            <tr>
                                <th>Method</th>
                                <th>Count</th>
                                <th>Min (ms)</th>
                                <th>P50 (ms)</th>
                                <th>P90 (ms)</th>
                                <th>P95 (ms)</th>
                                <th>P99 (ms)</th>
                                <th>Max (ms)</th>
                                <th>Avg (ms)</th>
                                <th>Error Rate</th>
                                <th>Throughput (req/s)</th>
                            </tr>
                        </thead>
                        <tbody>
                            {{range $method, $metrics := $client.Methods}}
                            <tr>
                                <td><strong>{{$method}}</strong></td>
                                <td class="metric-cell">{{$metrics.Count}}</td>
                                <td class="metric-cell">{{printf "%.2f" $metrics.Min}}</td>
                                <td class="metric-cell">{{printf "%.2f" $metrics.P50}}</td>
                                <td class="metric-cell">{{printf "%.2f" $metrics.P90}}</td>
                                <td class="metric-cell">{{printf "%.2f" $metrics.P95}}</td>
                                <td class="metric-cell">{{printf "%.2f" $metrics.P99}}</td>
                                <td class="metric-cell">{{printf "%.2f" $metrics.Max}}</td>
                                <td class="metric-cell">{{printf "%.2f" $metrics.Avg}}</td>
                                <td>
                                    {{if eq $metrics.ErrorRate 0.0}}
                                    <span class="badge badge-success">0%</span>
                                    {{else if lt $metrics.ErrorRate 1.0}}
                                    <span class="badge badge-warning">{{printf "%.2f" $metrics.ErrorRate}}%</span>
                                    {{else}}
                                    <span class="badge badge-danger">{{printf "%.2f" $metrics.ErrorRate}}%</span>
                                    {{end}}
                                </td>
                                <td class="metric-cell">{{printf "%.2f" $metrics.Throughput}}</td>
                            </tr>
                            {{end}}
                        </tbody>
                    </table>
                </div>
            </div>
            {{end}}
        </div>
    </div>

    <div class="footer">
        <p>Generated by JSON-RPC Benchmarking Suite on {{.Timestamp}}</p>
    </div>
    
    <script>
        // Tab switching function
        function showTab(tabId, tabElement) {
            // Hide all tab contents
            const allContents = document.querySelectorAll('.tab-content');
            allContents.forEach(content => {
                content.classList.remove('active');
            });
            
            // Remove active class from all tabs
            const allTabs = document.querySelectorAll('.tab');
            allTabs.forEach(tab => {
                tab.classList.remove('active');
            });
            
            // Show selected tab content
            document.getElementById(tabId).classList.add('active');
            tabElement.classList.add('active');
        }
        
        // Chart configurations
        const chartOptions = {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: {
                    position: 'top',
                }
            }
        };

        // Latency comparison chart
        const latencyCtx = document.getElementById('latencyChart').getContext('2d');
        new Chart(latencyCtx, {
            type: 'bar',
            data: {
                labels: {{.MethodNames}},
                datasets: [
                    {{range $index, $client := .ClientMetrics}}
                    {
                        label: '{{$client.Name}} P95',
                        data: {{index $.LatencyChartData $client.Name}},
                        backgroundColor: {{index $.ChartColors $index}},
                        borderColor: {{index $.ChartBorders $index}},
                        borderWidth: 1
                    },
                    {{end}}
                ]
            },
            options: {
                ...chartOptions,
                plugins: {
                    ...chartOptions.plugins,
                    title: {
                        display: true,
                        text: 'Method Latency Comparison (P95)'
                    }
                },
                scales: {
                    y: {
                        beginAtZero: true,
                        title: {
                            display: true,
                            text: 'Latency (ms)'
                        }
                    }
                }
            }
        });

        // Throughput chart
        const throughputCtx = document.getElementById('throughputChart').getContext('2d');
        new Chart(throughputCtx, {
            type: 'line',
            data: {
                labels: {{.MethodNames}},
                datasets: [
                    {{range $index, $client := .ClientMetrics}}
                    {
                        label: '{{$client.Name}}',
                        data: {{index $.ThroughputChartData $client.Name}},
                        borderColor: {{index $.ChartColors $index}},
                        backgroundColor: {{index $.ChartColors $index}},
                        fill: false,
                        tension: 0.1
                    },
                    {{end}}
                ]
            },
            options: {
                ...chartOptions,
                plugins: {
                    ...chartOptions.plugins,
                    title: {
                        display: true,
                        text: 'Method Throughput (req/s)'
                    }
                },
                scales: {
                    y: {
                        beginAtZero: true,
                        title: {
                            display: true,
                            text: 'Throughput (req/s)'
                        }
                    }
                }
            }
        });

        // Error rate chart
        const errorRateCtx = document.getElementById('errorRateChart').getContext('2d');
        new Chart(errorRateCtx, {
            type: 'radar',
            data: {
                labels: {{.MethodNames}},
                datasets: [
                    {{range $index, $client := .ClientMetrics}}
                    {
                        label: '{{$client.Name}}',
                        data: {{index $.ErrorRateChartData $client.Name}},
                        borderColor: {{index $.ChartColors $index}},
                        backgroundColor: {{index $.ChartColorsAlpha $index}},
                        fill: true
                    },
                    {{end}}
                ]
            },
            options: {
                ...chartOptions,
                plugins: {
                    ...chartOptions.plugins,
                    title: {
                        display: true,
                        text: 'Method Error Rates (%)'
                    }
                },
                scales: {
                    r: {
                        beginAtZero: true,
                        max: 5
                    }
                }
            }
        });

        // Percentile comparison chart
        const percentileCtx = document.getElementById('percentileChart').getContext('2d');
        new Chart(percentileCtx, {
            type: 'line',
            data: {
                labels: ['P50', 'P90', 'P95', 'P99'],
                datasets: [
                    {{range $index, $client := .ClientMetrics}}
                    {
                        label: '{{$client.Name}}',
                        data: {{index $.PercentileChartData $client.Name}},
                        borderColor: {{index $.ChartColors $index}},
                        backgroundColor: {{index $.ChartColors $index}},
                        fill: false,
                        tension: 0.3
                    },
                    {{end}}
                ]
            },
            options: {
                ...chartOptions,
                plugins: {
                    ...chartOptions.plugins,
                    title: {
                        display: true,
                        text: 'Overall Latency Percentiles (all methods)'
                    }
                },
                scales: {
                    y: {
                        beginAtZero: true,
                        title: {
                            display: true,
                            text: 'Latency (ms)'
                        }
                    }
                }
            }
        });
    </script>
</body>
</html>
`

// EnhancedReportData holds the data for the enhanced HTML report
type EnhancedReportData struct {
	TestName            string
	Description         string
	Timestamp           string
	StartTime           string
	EndTime             string
	Duration            string
	RPS                 int
	TotalRequests       int64
	OverallErrorRate    float64
	ClientMetrics       []*types.ClientMetrics
	MethodNames         []string
	LatencyChartData    map[string][]float64
	ThroughputChartData map[string][]float64
	ErrorRateChartData  map[string][]float64
	PercentileChartData map[string][]float64
	ChartColors         []string
	ChartBorders        []string
	ChartColorsAlpha    []string
}

// GenerateEnhancedHTMLReport generates an enhanced HTML report
func GenerateEnhancedHTMLReport(cfg *config.Config, result *types.BenchmarkResult, outputPath string) error {
	// Extract unique method names
	methodMap := make(map[string]bool)
	for _, metrics := range result.ClientMetrics {
		for method := range metrics.Methods {
			methodMap[method] = true
		}
	}

	var methodNames []string
	for method := range methodMap {
		methodNames = append(methodNames, method)
	}

	// Prepare chart data
	latencyData := make(map[string][]float64)
	throughputData := make(map[string][]float64)
	errorRateData := make(map[string][]float64)
	percentileData := make(map[string][]float64)

	// Convert client metrics map to slice
	var clientMetrics []*types.ClientMetrics
	for _, cm := range result.ClientMetrics {
		clientMetrics = append(clientMetrics, cm)

		// Prepare chart data for this client
		var latencies, throughputs, errorRates []float64
		for _, method := range methodNames {
			if methodMetric, exists := cm.Methods[method]; exists {
				latencies = append(latencies, methodMetric.P95)
				throughputs = append(throughputs, methodMetric.Throughput)
				errorRates = append(errorRates, methodMetric.ErrorRate)
			} else {
				latencies = append(latencies, 0)
				throughputs = append(throughputs, 0)
				errorRates = append(errorRates, 0)
			}
		}

		latencyData[cm.Name] = latencies
		throughputData[cm.Name] = throughputs
		errorRateData[cm.Name] = errorRates

		// Calculate overall percentiles
		var totalP50, totalP90, totalP95, totalP99 float64
		var count int

		for _, methodMetric := range cm.Methods {
			totalP50 += methodMetric.P50
			totalP90 += methodMetric.P90
			totalP95 += methodMetric.P95
			totalP99 += methodMetric.P99
			count++
		}

		if count > 0 {
			percentileData[cm.Name] = []float64{
				totalP50 / float64(count),
				totalP90 / float64(count),
				totalP95 / float64(count),
				totalP99 / float64(count),
			}
		}
	}

	// Calculate total requests and overall error rate
	var totalRequests, totalErrors int64
	for _, cm := range result.ClientMetrics {
		totalRequests += cm.TotalRequests
		totalErrors += cm.TotalErrors
	}

	overallErrorRate := 0.0
	if totalRequests > 0 {
		overallErrorRate = float64(totalErrors) / float64(totalRequests) * 100
	}

	// Chart colors
	chartColors := []string{
		"'rgba(54, 162, 235, 0.8)'",  // Blue
		"'rgba(255, 99, 132, 0.8)'",  // Red
		"'rgba(75, 192, 192, 0.8)'",  // Teal
		"'rgba(255, 205, 86, 0.8)'",  // Yellow
		"'rgba(153, 102, 255, 0.8)'", // Purple
		"'rgba(255, 159, 64, 0.8)'",  // Orange
	}

	chartBorders := []string{
		"'rgba(54, 162, 235, 1)'",
		"'rgba(255, 99, 132, 1)'",
		"'rgba(75, 192, 192, 1)'",
		"'rgba(255, 205, 86, 1)'",
		"'rgba(153, 102, 255, 1)'",
		"'rgba(255, 159, 64, 1)'",
	}

	chartColorsAlpha := []string{
		"'rgba(54, 162, 235, 0.2)'",
		"'rgba(255, 99, 132, 0.2)'",
		"'rgba(75, 192, 192, 0.2)'",
		"'rgba(255, 205, 86, 0.2)'",
		"'rgba(153, 102, 255, 0.2)'",
		"'rgba(255, 159, 64, 0.2)'",
	}

	// Prepare template data
	data := EnhancedReportData{
		TestName:            cfg.TestName,
		Description:         cfg.Description,
		Timestamp:           time.Now().Format("2006-01-02 15:04:05"),
		StartTime:           result.StartTime,
		Duration:            result.Duration,
		RPS:                 cfg.RPS,
		TotalRequests:       totalRequests,
		OverallErrorRate:    overallErrorRate,
		ClientMetrics:       clientMetrics,
		MethodNames:         methodNames,
		LatencyChartData:    latencyData,
		ThroughputChartData: throughputData,
		ErrorRateChartData:  errorRateData,
		PercentileChartData: percentileData,
		ChartColors:         chartColors,
		ChartBorders:        chartBorders,
		ChartColorsAlpha:    chartColorsAlpha,
	}

	// Create template with custom functions
	funcMap := template.FuncMap{
		"printf": fmt.Sprintf,
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(EnhancedHTMLReportTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	// Execute template
	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}
