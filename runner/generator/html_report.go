package generator

import (
	"fmt"
	"html/template"
	"os"
	"sort"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// htmlReportTemplate is the enhanced template with advanced visualizations
const htmlReportTemplate = `
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
            
            .comparison-grid {
                grid-template-columns: 1fr;
            }
        }
    </style>
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
                <div class="metric-icon">REQ</div>
                <div class="metric-value">{{.TotalRequests}}</div>
                <div class="metric-label">Total Requests</div>
                <div class="metric-trend">{{.ActualRPS}} req/s</div>
            </div>
            
            <div class="metric-card">
                <div class="metric-icon">OK</div>
                <div class="metric-value">{{printf "%.2f" .OverallSuccessRate}}%</div>
                <div class="metric-label">Success Rate</div>
                <div class="metric-trend">{{.TotalSuccess}} successful</div>
            </div>
            
            <div class="metric-card">
                <div class="metric-icon">P95</div>
                <div class="metric-value">{{printf "%.1f" .MeanClientP95}}ms</div>
                <div class="metric-label">Mean client P95</div>
                <div class="metric-trend">average of per-client p95, not a pooled percentile</div>
            </div>
        </div>

        <!-- Offered load. A shortfall must be read before the latencies above. -->
        <div class="table-section">
            <h2 class="section-title">Load delivery</h2>
            {{if .AnyShortfall}}
            <p style="color:#9c3520;font-weight:600;">
                At least one client was offered less load than requested: requests were dropped
                rather than sent. The latency figures above describe only the requests that were
                actually sent.
            </p>
            {{end}}
            {{if .AnyLate}}
            <p style="color:#8a5300;font-weight:600;">
                At least one client sent every scheduled request, but some went out later than
                scheduled: the endpoint was slower than the requested rate. See Max dispatch delay.
            </p>
            {{end}}
            <table>
                <thead>
                    <tr>
                        <th>Client</th><th>Scheduled</th><th>Sent</th><th>Late</th><th>Dropped</th>
                        <th>Delivered</th><th>Target RPS</th><th>Achieved RPS</th><th>Max dispatch delay</th>
                    </tr>
                </thead>
                <tbody>
                {{range .Delivery}}
                    <tr>
                        <td>{{.Name}}</td>
                        <td>{{.Scheduled}}</td>
                        <td>{{.Sent}}</td>
                        <td>{{.Late}}</td>
                        <td>{{.Dropped}}</td>
                        <td>{{printf "%.2f" .DeliveredPct}}%</td>
                        <td>{{printf "%.1f" .TargetRPS}}</td>
                        <td>{{printf "%.1f" .AchievedRPS}}</td>
                        <td>{{printf "%.2f" .MaxDispatchDelay}}ms</td>
                    </tr>
                {{end}}
                </tbody>
            </table>
        </div>
        
        <!-- Detailed Results Tabs -->
        <div class="table-section">
            <h2 class="chart-title">Detailed Results</h2>
            <div class="tabs">
                {{range $index, $client := .Clients}}
                <button class="tab {{if eq $index 0}}active{{end}}" onclick="showTab({{$index}}, this)">
                    {{$client.Name}}
                </button>
                {{end}}
            </div>
            
            {{range $index, $client := .Clients}}
            <div id="tab-{{$index}}" class="tab-content {{if eq $index 0}}active{{end}}">
                <h3>{{$client.Name}} Performance Metrics</h3>

                <!-- Client Summary -->
                <div class="environment-info" style="margin: 20px 0;">
                    <div class="env-item">
                        <span class="env-label">Total Requests</span>
                        <span class="env-value">{{$client.TotalRequests}}</span>
                    </div>
                    <div class="env-item">
                        <span class="env-label">Success Rate</span>
                        <span class="env-value">{{printf "%.2f" $client.SuccessRate}}%</span>
                    </div>
                    <div class="env-item">
                        <span class="env-label">Connection Reuse</span>
                        <span class="env-value">{{printf "%.1f" $client.ConnectionReuse}}%</span>
                    </div>
                </div>

                <!-- One row per call, not per method: several calls can drive one
                     method, and the method key alone hides what they measured. -->
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
                        {{range $call := $client.Calls}}
                        <tr>
                            <td>{{$call.Name}}</td>
                            <td><strong>{{$call.Method}}</strong></td>
                            <td>{{$call.Count}}</td>
                            <td>
                                {{if ge $call.SuccessRate 99.0}}
                                <span class="badge badge-success">{{printf "%.1f" $call.SuccessRate}}%</span>
                                {{else if ge $call.SuccessRate 95.0}}
                                <span class="badge badge-warning">{{printf "%.1f" $call.SuccessRate}}%</span>
                                {{else}}
                                <span class="badge badge-danger">{{printf "%.1f" $call.SuccessRate}}%</span>
                                {{end}}
                            </td>
                            <td>{{printf "%.1f" $call.Min}}</td>
                            <td>{{printf "%.1f" $call.P50}}</td>
                            <td>{{printf "%.1f" $call.P75}}</td>
                            <td>{{printf "%.1f" $call.P90}}</td>
                            <td>{{printf "%.1f" $call.P95}}</td>
                            <td>{{printf "%.1f" $call.P99}}</td>
                            <td>{{printf "%.1f" $call.P999}}</td>
                            <td>{{printf "%.1f" $call.Max}}</td>
                            <td>{{printf "%.1f" $call.StdDev}}</td>
                            <td>{{printf "%.1f" $call.CoeffVar}}%</td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
            {{end}}
        </div>
        
        <!-- Environment Information -->
        <div class="chart-section">
            <h2 class="chart-title">Test Environment</h2>
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
            <h2 class="chart-title">Performance Comparison</h2>
            <div class="comparison-grid">
                {{range $client, $score := .PerformanceScore}}
                <div class="comparison-card">
                    <h3>{{$client}}</h3>
                    {{if eq $client $.Comparison.Winner}}
                    <span class="winner-badge">Winner</span>
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
            <h2 class="chart-title">Performance Recommendations</h2>
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
        function showTab(index, element) {
            document.querySelectorAll('.tab-content').forEach(content => {
                content.classList.remove('active');
            });
            document.querySelectorAll('.tab').forEach(tab => {
                tab.classList.remove('active');
            });
            
            document.getElementById('tab-' + index).classList.add('active');
            element.classList.add('active');
        }
    </script>
</body>
</html>
`

// reportData holds all data for the ultimate HTML report
type reportData struct {
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

	// MeanClientP95 is the mean of the clients' p95 latencies, which is a
	// summary of the clients rather than a percentile of the requests. A pooled
	// percentile across clients would need their samples, not their aggregates.
	MeanClientP95 float64

	// ActualRPS sums the rate each client was actually offered, so with three
	// clients it is roughly three times the configured rps.
	ActualRPS float64

	// Delivery is the per-client offered-load accounting. A run that could not
	// offer its requested rate has to say so here, because the latency figures
	// beside it describe only the requests that went out.
	Delivery []clientDelivery

	// AnyShortfall is set when a client was offered less than it was asked to
	// offer: requests were dropped rather than sent. AnyLate is the separate,
	// milder case of every request going out but some of them behind schedule,
	// which the engine also reports separately — reading it as a shortfall put
	// the red banner above a table showing Scheduled == Sent.
	AnyShortfall bool
	AnyLate      bool

	BestClient string
	BestScore  float64

	// Environment
	Environment types.EnvironmentInfo

	// Comparison and scoring
	Comparison       *types.ComparisonResult
	PerformanceScore map[string]float64
	Recommendations  []string

	Clients []clientReport
}

// GenerateUltimateHTMLReport generates the ultimate HTML report with all advanced features
// GenerateHTMLReport renders the run's report.
func GenerateHTMLReport(cfg *config.Config, result *types.BenchmarkResult, outputPath string) error {
	// Prepare report data
	data := preparereportData(cfg, result)

	// Create template with custom functions
	funcMap := template.FuncMap{
		"printf": fmt.Sprintf,
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(htmlReportTemplate)
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

func preparereportData(cfg *config.Config, result *types.BenchmarkResult) *reportData {
	data := &reportData{
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
		data.Clients = append(data.Clients, clientReportFor(client))
	}
	sort.Slice(data.Clients, func(i, j int) bool { return data.Clients[i].Name < data.Clients[j].Name })

	data.TotalRequests = totalRequests
	data.TotalSuccess = totalSuccess
	if totalRequests > 0 {
		data.OverallSuccessRate = float64(totalSuccess) / float64(totalRequests) * 100
	}
	if clientCount > 0 {
		data.MeanClientP95 = totalP95 / float64(clientCount)
	}

	for _, client := range result.ClientMetrics {
		d := client.Delivery
		data.ActualRPS += d.AchievedRPS
		if d.Dropped > 0 {
			data.AnyShortfall = true
		}
		if d.Late > 0 {
			data.AnyLate = true
		}
		data.Delivery = append(data.Delivery, clientDelivery{
			Name:             client.Name,
			Scheduled:        d.Scheduled,
			Sent:             d.Sent,
			Late:             d.Late,
			Dropped:          d.Dropped,
			DeliveredPct:     d.DeliveryRatio() * 100,
			TargetRPS:        d.TargetRPS,
			AchievedRPS:      d.AchievedRPS,
			MaxDispatchDelay: d.MaxDispatchDelayMs,
			Complete:         d.Complete(),
		})
	}
	sort.Slice(data.Delivery, func(i, j int) bool { return data.Delivery[i].Name < data.Delivery[j].Name })

	// Find best performer
	if result.Comparison != nil {
		data.BestClient = result.Comparison.Winner
		data.BestScore = result.Comparison.WinnerScore
	}

	return data
}

// clientReport is one client's tab in the report.
type clientReport struct {
	Name            string
	TotalRequests   int64
	SuccessRate     float64
	ConnectionReuse float64
	Calls           []callRow
}

// callRow is one row of a client's breakdown. It is keyed on the call the
// config declared rather than on the RPC method: a run can drive one method
// through several parameter shapes, and then the method alone collapses the
// only dimension it was measuring.
type callRow struct {
	Name   string
	Method string
	types.MetricSummary
}

func clientReportFor(client *types.ClientMetrics) clientReport {
	report := clientReport{
		Name:            client.Name,
		TotalRequests:   client.TotalRequests,
		SuccessRate:     100 - client.ErrorRate,
		ConnectionReuse: client.ConnectionMetrics.ConnectionReuse,
	}

	// The per-method breakdown is the fallback for a result recorded without
	// the per-call one, where the method is all there is to name a row by.
	breakdown := client.Calls
	if len(breakdown) == 0 {
		breakdown = client.MethodDetails
	}
	for key, metrics := range breakdown {
		if metrics == nil {
			continue
		}
		row := callRow{Name: metrics.Name, Method: metrics.Method, MetricSummary: metrics.MetricSummary}
		if row.Name == "" {
			row.Name = key
		}
		report.Calls = append(report.Calls, row)
	}
	sort.Slice(report.Calls, func(i, j int) bool {
		if report.Calls[i].Name != report.Calls[j].Name {
			return report.Calls[i].Name < report.Calls[j].Name
		}
		return report.Calls[i].Method < report.Calls[j].Method
	})

	return report
}

// clientDelivery is one client's offered-load accounting, for the report table.
type clientDelivery struct {
	Name             string
	Scheduled        int64
	Sent             int64
	Late             int64
	Dropped          int64
	DeliveredPct     float64
	TargetRPS        float64
	AchievedRPS      float64
	MaxDispatchDelay float64
	Complete         bool
}
