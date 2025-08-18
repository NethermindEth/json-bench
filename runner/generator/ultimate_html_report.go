package generator

import (
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// UltimateHTMLReportTemplate is the enhanced template with advanced visualizations
const UltimateHTMLReportTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.TestName}} - Advanced JSON-RPC Benchmark Report</title>
    <style>
        :root {
            --primary-color: #3498db;
            --secondary-color: #2ecc71;
            --danger-color: #e74c3c;
            --warning-color: #f39c12;
            --dark-color: #2c3e50;
            --light-bg: #ecf0f1;
            --card-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }
        
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            line-height: 1.6;
            color: #333;
            background-color: #f5f7fa;
        }
        
        .container {
            max-width: 1600px;
            margin: 0 auto;
            padding: 20px;
        }
        
        .header {
            background: linear-gradient(135deg, var(--primary-color), var(--secondary-color));
            color: white;
            padding: 40px 0;
            margin-bottom: 30px;
            border-radius: 10px;
            box-shadow: var(--card-shadow);
        }
        
        .header-content {
            max-width: 1200px;
            margin: 0 auto;
            padding: 0 20px;
        }
        
        h1 {
            font-size: 2.5em;
            margin-bottom: 10px;
        }
        
        .subtitle {
            font-size: 1.2em;
            opacity: 0.9;
        }
        
        .dashboard-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        
        .metric-card {
            background: white;
            padding: 20px;
            border-radius: 10px;
            box-shadow: var(--card-shadow);
            transition: transform 0.2s;
        }
        
        .metric-card:hover {
            transform: translateY(-2px);
            box-shadow: 0 6px 12px rgba(0, 0, 0, 0.15);
        }
        
        .metric-icon {
            font-size: 1.5em;
            margin-bottom: 8px;
        }
        
        .metric-value {
            font-size: 1.8em;
            font-weight: bold;
            color: var(--primary-color);
            margin-bottom: 3px;
        }
        
        .metric-label {
            color: #666;
            font-size: 0.85em;
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }
        
        .metric-trend {
            font-size: 0.75em;
            color: var(--secondary-color);
            margin-top: 3px;
        }
        
        .chart-section {
            background: white;
            padding: 30px;
            border-radius: 10px;
            box-shadow: var(--card-shadow);
            margin-bottom: 30px;
        }
        
        .chart-title {
            font-size: 1.5em;
            margin-bottom: 20px;
            color: var(--dark-color);
        }
        
        .chart-container {
            position: relative;
            height: 400px;
            margin-bottom: 20px;
        }
        
        .chart-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(500px, 1fr));
            gap: 30px;
        }
        
        .time-series-container {
            height: 500px;
        }
        
        .table-section {
            background: white;
            padding: 30px;
            border-radius: 10px;
            box-shadow: var(--card-shadow);
            margin-bottom: 30px;
            overflow-x: auto;
        }
        
        table {
            width: 100%;
            border-collapse: collapse;
        }
        
        th, td {
            padding: 12px 15px;
            text-align: left;
            border-bottom: 1px solid #ddd;
        }
        
        th {
            background-color: var(--light-bg);
            font-weight: 600;
            color: var(--dark-color);
            position: sticky;
            top: 0;
            z-index: 10;
        }
        
        tr:hover {
            background-color: #f8f9fa;
        }
        
        .badge {
            display: inline-block;
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 0.85em;
            font-weight: bold;
            text-align: center;
        }
        
        .badge-success {
            background-color: var(--secondary-color);
            color: white;
        }
        
        .badge-warning {
            background-color: var(--warning-color);
            color: white;
        }
        
        .badge-danger {
            background-color: var(--danger-color);
            color: white;
        }
        
        .tabs {
            display: flex;
            border-bottom: 2px solid #e0e0e0;
            margin-bottom: 20px;
        }
        
        .tab {
            padding: 12px 24px;
            cursor: pointer;
            background: none;
            border: none;
            font-size: 1em;
            color: #666;
            transition: all 0.3s;
            position: relative;
        }
        
        .tab:hover {
            color: var(--primary-color);
        }
        
        .tab.active {
            color: var(--primary-color);
            font-weight: 600;
        }
        
        .tab.active::after {
            content: '';
            position: absolute;
            bottom: -2px;
            left: 0;
            right: 0;
            height: 2px;
            background: var(--primary-color);
        }
        
        .tab-content {
            display: none;
        }
        
        .tab-content.active {
            display: block;
            animation: fadeIn 0.3s;
        }
        
        @keyframes fadeIn {
            from { opacity: 0; transform: translateY(10px); }
            to { opacity: 1; transform: translateY(0); }
        }
        
        .environment-info {
            background: var(--light-bg);
            padding: 20px;
            border-radius: 8px;
            margin-bottom: 20px;
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 15px;
        }
        
        .env-item {
            display: flex;
            flex-direction: column;
        }
        
        .env-label {
            font-size: 0.85em;
            color: #666;
            margin-bottom: 3px;
        }
        
        .env-value {
            font-weight: 600;
            color: var(--dark-color);
        }
        
        .heatmap-container {
            overflow-x: auto;
            margin-bottom: 20px;
        }
        
        .comparison-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(350px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        
        .comparison-card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: var(--card-shadow);
        }
        
        .winner-badge {
            background: linear-gradient(135deg, #f39c12, #e74c3c);
            color: white;
            padding: 8px 16px;
            border-radius: 20px;
            font-weight: bold;
            display: inline-block;
            margin-bottom: 10px;
        }
        
        .tooltip {
            position: relative;
            display: inline-block;
            cursor: help;
        }
        
        .tooltip .tooltiptext {
            visibility: hidden;
            width: 200px;
            background-color: #333;
            color: #fff;
            text-align: center;
            border-radius: 6px;
            padding: 8px;
            position: absolute;
            z-index: 1;
            bottom: 125%;
            left: 50%;
            margin-left: -100px;
            opacity: 0;
            transition: opacity 0.3s;
            font-size: 0.85em;
        }
        
        .tooltip:hover .tooltiptext {
            visibility: visible;
            opacity: 1;
        }
        
        .footer {
            margin-top: 50px;
            padding: 30px 0;
            text-align: center;
            color: #666;
            font-size: 0.9em;
        }
        
        @media (max-width: 768px) {
            .dashboard-grid {
                grid-template-columns: 1fr;
            }
            
            .chart-grid {
                grid-template-columns: 1fr;
            }
            
            .comparison-grid {
                grid-template-columns: 1fr;
            }
        }
    </style>
    <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0"></script>
</head>
<body>
    <div class="header">
        <div class="header-content">
            <h1>{{.TestName}}</h1>
            <p class="subtitle">{{.Description}}</p>
        </div>
    </div>
    
    <div class="container">
        <!-- Key Metrics Dashboard (smaller) -->
        <div class="dashboard-grid" style="grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); margin-bottom: 20px;">
            <div class="metric-card">
                <div class="metric-icon">📊</div>
                <div class="metric-value">{{.TotalRequests}}</div>
                <div class="metric-label">Total Requests</div>
                <div class="metric-trend">{{.ActualRPS}} req/s</div>
            </div>
            
            <div class="metric-card">
                <div class="metric-icon">✅</div>
                <div class="metric-value">{{printf "%.2f" .OverallSuccessRate}}%</div>
                <div class="metric-label">Success Rate</div>
                <div class="metric-trend">{{.TotalSuccess}} successful</div>
            </div>
            
            <div class="metric-card">
                <div class="metric-icon">⚡</div>
                <div class="metric-value">{{printf "%.1f" .OverallP95}}ms</div>
                <div class="metric-label">P95 Latency</div>
                <div class="metric-trend">95th percentile</div>
            </div>
        </div>
        
        <!-- Detailed Results Tabs -->
        <div class="table-section">
            <h2 class="chart-title">📋 Detailed Results</h2>
            <div class="tabs">
                {{range $index, $client := .ClientMetrics}}
                <button class="tab {{if eq $index 0}}active{{end}}" onclick="showTab('{{$client.Name}}', this)">
                    {{$client.Name}}
                </button>
                {{end}}
            </div>
            
            {{range $index, $client := .ClientMetrics}}
            <div id="tab-{{$client.Name}}" class="tab-content {{if eq $index 0}}active{{end}}">
                <h3>{{$client.Name}} Performance Metrics</h3>
                
                <!-- Client Summary -->
                <div class="environment-info" style="margin: 20px 0;">
                    <div class="env-item">
                        <span class="env-label">Total Requests</span>
                        <span class="env-value">{{$client.TotalRequests}}</span>
                    </div>
                    <div class="env-item">
                        <span class="env-label">Success Rate</span>
                        <span class="env-value">{{printf "%.2f" (sub 100.0 $client.ErrorRate)}}%</span>
                    </div>
                    <div class="env-item">
                        <span class="env-label">Avg Connections</span>
                        <span class="env-value">{{$client.ConnectionMetrics.ActiveConnections}}</span>
                    </div>
                    <div class="env-item">
                        <span class="env-label">Connection Reuse</span>
                        <span class="env-value">{{printf "%.1f" $client.ConnectionMetrics.ConnectionReuse}}%</span>
                    </div>
                </div>
                
                <!-- Method Performance Table -->
                <table>
                    <thead>
                        <tr>
                            <th>Name</th>
                            <th>Method</th>
                            <th>Count</th>
                            <th>Success Rate</th>
                            <th>Min</th>
                            <th>P50</th>
                            <th>P75</th>
                            <th>P90</th>
                            <th>P95</th>
                            <th>P99</th>
                            <th>P99.9</th>
                            <th>Max</th>
                            <th>Std Dev</th>
                            <th class="tooltip">
                                CV
                                <span class="tooltiptext">Coefficient of Variation - Lower is better</span>
                            </th>
                        </tr>
                    </thead>
                    <tbody>
                        {{range $method, $metrics := $client.Methods}}
                        <tr>
                            <td>{{if index $.EndpointNames $method}}{{index $.EndpointNames $method}}{{end}}</td>
                            <td><strong>{{$method}}</strong></td>
                            <td>{{$metrics.Count}}</td>
                            <td>
                                {{if ge $metrics.SuccessRate 99.0}}
                                <span class="badge badge-success">{{printf "%.1f" $metrics.SuccessRate}}%</span>
                                {{else if ge $metrics.SuccessRate 95.0}}
                                <span class="badge badge-warning">{{printf "%.1f" $metrics.SuccessRate}}%</span>
                                {{else}}
                                <span class="badge badge-danger">{{printf "%.1f" $metrics.SuccessRate}}%</span>
                                {{end}}
                            </td>
                            <td>{{printf "%.1f" $metrics.Min}}</td>
                            <td>{{printf "%.1f" $metrics.P50}}</td>
                            <td>{{printf "%.1f" $metrics.P75}}</td>
                            <td>{{printf "%.1f" $metrics.P90}}</td>
                            <td>{{printf "%.1f" $metrics.P95}}</td>
                            <td>{{printf "%.1f" $metrics.P99}}</td>
                            <td>{{printf "%.1f" $metrics.P999}}</td>
                            <td>{{printf "%.1f" $metrics.Max}}</td>
                            <td>{{printf "%.1f" $metrics.StdDev}}</td>
                            <td>{{printf "%.1f" $metrics.CoeffVar}}%</td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
            {{end}}
        </div>
        
        <!-- Environment Information -->
        <div class="chart-section">
            <h2 class="chart-title">🖥️ Test Environment</h2>
            <div class="environment-info">
                <div class="env-item">
                    <span class="env-label">Operating System</span>
                    <span class="env-value">{{.Environment.OS}} {{.Environment.Architecture}}</span>
                </div>
                <div class="env-item">
                    <span class="env-label">CPU</span>
                    <span class="env-value">{{.Environment.CPUModel}} ({{.Environment.CPUCores}} cores)</span>
                </div>
                <div class="env-item">
                    <span class="env-label">Memory</span>
                    <span class="env-value">{{printf "%.1f" .Environment.TotalMemoryGB}} GB</span>
                </div>
                <div class="env-item">
                    <span class="env-label">Duration</span>
                    <span class="env-value">{{.Duration}}</span>
                </div>
                <div class="env-item">
                    <span class="env-label">Target RPS</span>
                    <span class="env-value">{{.RPS}}</span>
                </div>
                <div class="env-item">
                    <span class="env-label">Test Started</span>
                    <span class="env-value">{{.StartTime}}</span>
                </div>
            </div>
        </div>
        
        <!-- Performance Comparison -->
        {{if .Comparison}}
        <div class="chart-section">
            <h2 class="chart-title">🏁 Performance Comparison</h2>
            <div class="comparison-grid">
                {{range $client, $score := .PerformanceScore}}
                <div class="comparison-card">
                    <h3>{{$client}}</h3>
                    {{if eq $client $.Comparison.Winner}}
                    <span class="winner-badge">🥇 Winner</span>
                    {{end}}
                    <div style="margin-top: 10px;">
                        <strong>Performance Score:</strong> {{printf "%.1f" $score}}/100
                    </div>
                    <div style="margin-top: 10px;">
                        <div style="background: #e0e0e0; height: 20px; border-radius: 10px; overflow: hidden;">
                            <div style="background: linear-gradient(90deg, #2ecc71, #3498db); height: 100%; width: {{$score}}%; transition: width 0.5s;"></div>
                        </div>
                    </div>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
        
        <!-- Recommendations -->
        {{if .Recommendations}}
        <div class="chart-section">
            <h2 class="chart-title">💡 Performance Recommendations</h2>
            <ul style="padding-left: 20px;">
                {{range .Recommendations}}
                <li style="margin-bottom: 10px;">{{.}}</li>
                {{end}}
            </ul>
        </div>
        {{end}}
    </div>
    
    <div class="footer">
        <p>Generated by Advanced JSON-RPC Benchmarking Suite</p>
        <p>{{.Timestamp}}</p>
    </div>
    
    <script>
        // Chart default options
        Chart.defaults.font.family = '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif';
        Chart.defaults.color = '#666';
        
        // Tab switching
        function showTab(tabName, element) {
            document.querySelectorAll('.tab-content').forEach(content => {
                content.classList.remove('active');
            });
            document.querySelectorAll('.tab').forEach(tab => {
                tab.classList.remove('active');
            });
            
            document.getElementById('tab-' + tabName).classList.add('active');
            element.classList.add('active');
        }
    </script>
</body>
</html>
`

// UltimateReportData holds all data for the ultimate HTML report
type UltimateReportData struct {
	// Basic info
	TestName    string
	Description string
	Timestamp   string
	StartTime   string
	Duration    string
	RPS         int

	// Summary metrics
	TotalRequests      int64
	TotalSuccess       int64
	OverallSuccessRate float64
	OverallP95         float64
	ActualRPS          float64
	BestClient         string
	BestScore          float64

	// Environment
	Environment types.EnvironmentInfo

	// Comparison and scoring
	Comparison       *types.ComparisonResult
	PerformanceScore map[string]float64
	Recommendations  []string

	// Client data
	ClientMetrics []*types.ClientMetrics
	ClientNames   []string
	MethodNames   []string
	EndpointNames map[string]string // Map of method to custom names

	// Colors
	ChartColors      []string
	ChartColorsAlpha []string
}

// GenerateUltimateHTMLReport generates the ultimate HTML report with all advanced features
func GenerateUltimateHTMLReport(cfg *config.Config, result *types.BenchmarkResult, outputPath string) error {
	// Prepare report data
	data := prepareUltimateReportData(cfg, result)

	// Create template with custom functions
	funcMap := template.FuncMap{
		"printf": fmt.Sprintf,
		"sub": func(a, b float64) float64 {
			return a - b
		},
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(UltimateHTMLReportTemplate)
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

func prepareUltimateReportData(cfg *config.Config, result *types.BenchmarkResult) *UltimateReportData {
	data := &UltimateReportData{
		TestName:         cfg.TestName,
		Description:      cfg.Description,
		Timestamp:        time.Now().Format("2006-01-02 15:04:05"),
		StartTime:        result.StartTime,
		Duration:         result.Duration,
		RPS:              cfg.RPS,
		Environment:      result.Environment,
		Comparison:       result.Comparison,
		PerformanceScore: result.PerformanceScore,
		Recommendations:  result.Recommendations,
		EndpointNames:    make(map[string]string),
	}

	// Populate methods names from config
	for _, method := range cfg.Methods {
		if method.Name != "" {
			data.EndpointNames[method.Name] = method.Name
		}
	}

	// Calculate summary metrics
	var totalRequests, totalSuccess int64
	var totalP95 float64
	var clientCount int

	for _, client := range result.ClientMetrics {
		totalRequests += client.TotalRequests
		totalSuccess += client.TotalRequests - client.TotalErrors
		totalP95 += client.Latency.P95
		clientCount++
		data.ClientMetrics = append(data.ClientMetrics, client)
		data.ClientNames = append(data.ClientNames, client.Name)
	}

	data.TotalRequests = totalRequests
	data.TotalSuccess = totalSuccess
	data.OverallSuccessRate = float64(totalSuccess) / float64(totalRequests) * 100
	data.OverallP95 = totalP95 / float64(clientCount)

	// Calculate actual RPS
	duration, _ := time.ParseDuration(result.Duration)
	data.ActualRPS = float64(totalRequests) / duration.Seconds()

	// Find best performer
	if result.Comparison != nil {
		data.BestClient = result.Comparison.Winner
		data.BestScore = result.Comparison.WinnerScore
	}

	// Extract method names
	methodMap := make(map[string]bool)
	for _, client := range result.ClientMetrics {
		for method := range client.Methods {
			methodMap[method] = true
		}
	}
	for method := range methodMap {
		data.MethodNames = append(data.MethodNames, method)
	}

	// Chart colors
	data.ChartColors = []string{
		"'rgb(54, 162, 235)'",
		"'rgb(255, 99, 132)'",
		"'rgb(75, 192, 192)'",
		"'rgb(255, 205, 86)'",
		"'rgb(153, 102, 255)'",
		"'rgb(255, 159, 64)'",
	}

	data.ChartColorsAlpha = []string{
		"'rgba(54, 162, 235, 0.2)'",
		"'rgba(255, 99, 132, 0.2)'",
		"'rgba(75, 192, 192, 0.2)'",
		"'rgba(255, 205, 86, 0.2)'",
		"'rgba(153, 102, 255, 0.2)'",
		"'rgba(255, 159, 64, 0.2)'",
	}

	return data
}
