package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jsonrpc-bench/runner/types"
)

// GenerateHistoricAnalysisReport generates an HTML report for historic analysis
func GenerateHistoricAnalysisReport(summary *types.HistoricSummary, trends []*types.TrendData, recentRuns []*types.HistoricRun, outputDir string) error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	reportPath := filepath.Join(outputDir, "historic-analysis.html")

	// Simple HTML template for historic analysis
	htmlContent := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>Historic Analysis Report - %s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background-color: #f4f4f4; padding: 20px; border-radius: 5px; }
        .section { margin: 20px 0; }
        .metrics { display: flex; flex-wrap: wrap; gap: 20px; }
        .metric-card { border: 1px solid #ddd; padding: 15px; border-radius: 5px; min-width: 200px; }
        .trend-improving { color: green; }
        .trend-degrading { color: red; }
        .trend-stable { color: orange; }
        table { border-collapse: collapse; width: 100%%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #f2f2f2; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Historic Analysis Report</h1>
        <h2>Test: %s</h2>
        <p><strong>Total Runs:</strong> %d</p>
        <p><strong>Period:</strong> %s to %s</p>
    </div>
    
    <div class="section">
        <h3>Performance Trends (Last 30 Days)</h3>
        <div class="metrics">
`, summary.TestName, summary.TestName, summary.TotalRuns,
		summary.FirstRun.Format("2006-01-02"), summary.LastRun.Format("2006-01-02"))

	// Add trend cards
	for i, trend := range trends {
		trendClass := "trend-" + trend.Direction
		htmlContent += fmt.Sprintf(`
            <div class="metric-card">
                <h4>Trend %d - %s</h4>
                <p class="%s"><strong>Direction:</strong> %s</p>
                <p><strong>Data Points:</strong> %d</p>
                <p><strong>Change:</strong> %.2f%%</p>
            </div>
`, i+1, trend.Period, trendClass, trend.Direction, len(trend.TrendPoints), trend.PercentChange)
	}

	htmlContent += `
        </div>
    </div>
    
    <div class="section">
        <h3>Recent Runs</h3>
        <table>
            <tr>
                <th>Run ID</th>
                <th>Timestamp</th>
                <th>Git Commit</th>
                <th>Best Client</th>
                <th>Avg Latency (ms)</th>
                <th>Error Rate (%)</th>
                <th>Total Requests</th>
            </tr>
`

	// Add recent runs
	for _, run := range recentRuns {
		htmlContent += fmt.Sprintf(`
            <tr>
                <td>%s</td>
                <td>%s</td>
                <td>%s</td>
                <td>%s</td>
                <td>%.2f</td>
                <td>%.2f</td>
                <td>%d</td>
            </tr>
`, run.ID, run.Timestamp.Format("2006-01-02 15:04:05"),
			run.GitCommit, run.BestClient, run.AvgLatencyMs,
			run.OverallErrorRate*100, run.TotalRequests)
	}

	htmlContent += `
        </table>
    </div>
    
    <div class="section">
        <h3>Best and Worst Performance</h3>
        <div class="metrics">
            <div class="metric-card">
                <h4>Best Run</h4>
                <p><strong>Run ID:</strong> ` + summary.BestRun.ID + `</p>
                <p><strong>Timestamp:</strong> ` + summary.BestRun.Timestamp.Format("2006-01-02 15:04:05") + `</p>
                <p><strong>Avg Latency:</strong> ` + fmt.Sprintf("%.2f ms", summary.BestRun.AvgLatency) + `</p>
                <p><strong>Error Rate:</strong> ` + fmt.Sprintf("%.2f%%", summary.BestRun.OverallErrorRate) + `</p>
            </div>
            <div class="metric-card">
                <h4>Worst Run</h4>
                <p><strong>Run ID:</strong> ` + summary.WorstRun.ID + `</p>
                <p><strong>Timestamp:</strong> ` + summary.WorstRun.Timestamp.Format("2006-01-02 15:04:05") + `</p>
                <p><strong>Avg Latency:</strong> ` + fmt.Sprintf("%.2f ms", summary.WorstRun.AvgLatency) + `</p>
                <p><strong>Error Rate:</strong> ` + fmt.Sprintf("%.2f%%", summary.WorstRun.OverallErrorRate) + `</p>
            </div>
        </div>
    </div>
    
    <div class="section">
        <p><em>Report generated on ` + time.Now().Format("2006-01-02 15:04:05") + `</em></p>
    </div>
</body>
</html>
`

	// Write the report
	if err := os.WriteFile(reportPath, []byte(htmlContent), 0644); err != nil {
		return fmt.Errorf("failed to write historic analysis report: %w", err)
	}

	fmt.Printf("Historic analysis report generated at: %s\n", reportPath)
	return nil
}
