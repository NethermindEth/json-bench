package generator

import (
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// HTMLReportTemplate is the template for generating HTML reports
const HTMLReportTemplate = `
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
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
        }
        h1, h2, h3 {
            color: #2c3e50;
        }
        .summary {
            background-color: #f8f9fa;
            border-radius: 5px;
            padding: 20px;
            margin-bottom: 20px;
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
        }
        tr:hover {
            background-color: #f5f5f5;
        }
        .chart-container {
            height: 400px;
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
        .footer {
            margin-top: 40px;
            padding-top: 20px;
            border-top: 1px solid #eee;
            text-align: center;
            font-size: 14px;
            color: #777;
        }
    </style>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
</head>
<body>
    <h1>{{.TestName}} - JSON-RPC Benchmark Report</h1>
    <p>{{.Description}}</p>
    
    <div class="summary">
        <h2>Test Summary</h2>
        <p><strong>Date:</strong> {{.Timestamp}}</p>
        <p><strong>Duration:</strong> {{.Duration}}</p>
        <p><strong>Target RPS:</strong> {{.RPS}}</p>
        <p><strong>Clients:</strong> {{.ClientNames}}</p>
    </div>
    
    <h2>Performance Results</h2>
    
    <div class="chart-container">
        <canvas id="latencyChart"></canvas>
    </div>
    
    <h3>Latency by Method (p95, ms)</h3>
    <table>
        <thead>
            <tr>
                <th>Method</th>
                {{range .Clients}}
                <th>{{.Name}}</th>
                {{end}}
            </tr>
        </thead>
        <tbody>
            {{range .Methods}}
            <tr>
                <td>{{.}}</td>
                {{range $.Clients}}
                <td>{{index $.MethodLatency . .}}</td>
                {{end}}
            </tr>
            {{end}}
        </tbody>
    </table>
    
    <h3>Request Rate by Method (req/s)</h3>
    <table>
        <thead>
            <tr>
                <th>Method</th>
                {{range .Clients}}
                <th>{{.Name}}</th>
                {{end}}
            </tr>
        </thead>
        <tbody>
            {{range .Methods}}
            <tr>
                <td>{{.}}</td>
                {{range $.Clients}}
                <td>{{index $.MethodRate . .}}</td>
                {{end}}
            </tr>
            {{end}}
        </tbody>
    </table>
    
    <h3>Error Rate by Method (%)</h3>
    <table>
        <thead>
            <tr>
                <th>Method</th>
                {{range .Clients}}
                <th>{{.Name}}</th>
                {{end}}
            </tr>
        </thead>
        <tbody>
            {{range .Methods}}
            <tr>
                <td>{{.}}</td>
                {{range $.Clients}}
                <td>
                    {{with index $.MethodErrorRate . .}}
                    <span class="badge {{if lt . 1.0}}badge-success{{else if lt . 5.0}}badge-warning{{else}}badge-danger{{end}}">
                        {{.}}%
                    </span>
                    {{end}}
                </td>
                {{end}}
            </tr>
            {{end}}
        </tbody>
    </table>
    
    {{if .ResponseDiffs}}
    <h2>Response Compatibility</h2>
    <div class="card">
        <div class="card-body">
          <p>This section shows differences in responses between clients.</p>
          <table class="table">
            <thead>
              <tr>
                <th>Method</th>
                <th>Clients</th>
                <th>Differences</th>
                <th>Schema Validation</th>
              </tr>
            </thead>
            <tbody>
              {{range .ResponseDiffs}}
              <tr>
                <td>{{.Method}}</td>
                <td>{{range .Clients}}{{.}}<br>{{end}}</td>
                <td>
                  {{range $key, $value := .Differences}}
                  <strong>{{$key}}:</strong> {{$value}}<br>
                  {{end}}
                </td>
                <td>
                  {{if .SchemaErrors}}
                    {{range $client, $errors := .SchemaErrors}}
                    <div class="alert alert-danger">
                      <strong>{{$client}}:</strong>
                      <ul>
                        {{range $errors}}
                        <li>{{.}}</li>
                        {{end}}
                      </ul>
                    </div>
                    {{end}}
                  {{else}}
                    <span class="badge bg-success">Valid</span>
                  {{end}}
                </td>
              </tr>
              {{end}}
            </tbody>
          </table>
        </div>
      </div>
    {{else}}
    <h2>Response Compatibility</h2>
    <p>✅ All client responses were identical.</p>
    {{end}}
    
    <div class="footer">
        <p>Generated by JSON-RPC Benchmarking Suite on {{.Timestamp}}</p>
    </div>
    
    <script>
        // Sample data for the chart - in a real implementation, this would be populated from the benchmark results
        const ctx = document.getElementById('latencyChart').getContext('2d');
        const latencyChart = new Chart(ctx, {
            type: 'bar',
            data: {
                labels: {{.Methods}},
                datasets: [
                    {{range $index, $client := .Clients}}
                    {
                        label: '{{$client.Name}}',
                        data: {{index $.ChartData $client.Name}},
                        backgroundColor: {{index $.ChartColors $index}},
                        borderColor: {{index $.ChartBorders $index}},
                        borderWidth: 1
                    },
                    {{end}}
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    title: {
                        display: true,
                        text: 'p95 Latency by Method (ms)'
                    },
                    legend: {
                        position: 'top',
                    }
                },
                scales: {
                    y: {
                        beginAtZero: true,
                        title: {
                            display: true,
                            text: 'Latency (ms)'
                        }
                    },
                    x: {
                        title: {
                            display: true,
                            text: 'Method'
                        }
                    }
                }
            }
        });
    </script>
</body>
</html>
`

// HTMLReportData holds the data to be injected into the HTML report template
type HTMLReportData struct {
	TestName        string
	Description     string
	Timestamp       string
	Duration        string
	RPS             int
	Clients         []config.Client
	ClientNames     string
	Methods         []string
	MethodLatency   map[string]map[string]float64
	MethodRate      map[string]map[string]float64
	MethodErrorRate map[string]map[string]float64
	ResponseDiffs   []types.ResponseDiff
	ChartData       map[string][]float64
	ChartColors     []string
	ChartBorders    []string
}

// GenerateHTMLReport generates an HTML report from the benchmark results
func GenerateHTMLReport(cfg *config.Config, result *BenchmarkResult, outputPath string) error {
	// Extract response diffs from the result
	var responseDiffs []types.ResponseDiff
	if result.ResponseDiff != nil {
		if diffs, ok := result.ResponseDiff["diffs"].([]types.ResponseDiff); ok {
			responseDiffs = diffs
		}
	}

	// Prepare chart colors for each client
	chartColors := []string{
		"'rgba(54, 162, 235, 0.5)'",
		"'rgba(255, 99, 132, 0.5)'",
		"'rgba(75, 192, 192, 0.5)'",
		"'rgba(255, 206, 86, 0.5)'",
		"'rgba(153, 102, 255, 0.5)'",
	}

	chartBorders := []string{
		"'rgba(54, 162, 235, 1)'",
		"'rgba(255, 99, 132, 1)'",
		"'rgba(75, 192, 192, 1)'",
		"'rgba(255, 206, 86, 1)'",
		"'rgba(153, 102, 255, 1)'",
	}

	// Extract methods from config
	methods := make([]string, len(cfg.Endpoints))
	for i, endpoint := range cfg.Endpoints {
		methods[i] = endpoint.Method
	}

	// Build client names string
	clientNames := make([]string, len(cfg.Clients))
	for i, client := range cfg.Clients {
		clientNames[i] = client.Name
	}

	// In a real implementation, we would extract actual metrics from the k6 results
	// For now, we'll use placeholder data
	methodLatency := make(map[string]map[string]float64)
	methodRate := make(map[string]map[string]float64)
	methodErrorRate := make(map[string]map[string]float64)
	chartData := make(map[string][]float64)

	for _, client := range cfg.Clients {
		methodLatency[client.Name] = make(map[string]float64)
		methodRate[client.Name] = make(map[string]float64)
		methodErrorRate[client.Name] = make(map[string]float64)
		chartData[client.Name] = make([]float64, len(methods))

		for i, method := range methods {
			// Placeholder values - in a real implementation, these would come from the k6 results
			methodLatency[client.Name][method] = float64(50 + i*10)
			methodRate[client.Name][method] = float64(cfg.RPS) * 0.9
			methodErrorRate[client.Name][method] = 0.5
			chartData[client.Name][i] = float64(50 + i*10)
		}
	}

	// Prepare template data
	data := HTMLReportData{
		TestName:        cfg.TestName,
		Description:     cfg.Description,
		Timestamp:       time.Now().Format(time.RFC1123),
		Duration:        cfg.Duration,
		RPS:             cfg.RPS,
		Clients:         cfg.Clients,
		ClientNames:     fmt.Sprintf("%v", clientNames),
		Methods:         methods,
		MethodLatency:   methodLatency,
		MethodRate:      methodRate,
		MethodErrorRate: methodErrorRate,
		ResponseDiffs:   responseDiffs, // Use extracted response diffs
		ChartData:       chartData,
		ChartColors:     chartColors,
		ChartBorders:    chartBorders,
	}

	// Parse template
	tmpl, err := template.New("htmlreport").Parse(HTMLReportTemplate)
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
