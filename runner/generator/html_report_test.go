package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

func callMetrics(name, method string, count int64, successRate, p95 float64) *types.MethodMetrics {
	return &types.MethodMetrics{
		MetricSummary: types.MetricSummary{Count: count, SuccessRate: successRate, P95: p95, Min: 1, Max: 9},
		Name:          name,
		Method:        method,
	}
}

func renderReport(t *testing.T, cfg *config.Config, result *types.BenchmarkResult) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.html")
	require.NoError(t, GenerateHTMLReport(cfg, result, path))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

// Several calls driving one RPC method are the case the per-call breakdown
// exists for, and the row has to name which one it is.
func TestReportNamesEveryCallRatherThanCollapsingThemByMethod(t *testing.T) {
	cfg := &config.Config{
		TestName: "archive proofs",
		Calls: []*config.Call{
			{Name: "Vitalik's Balance", Method: "eth_getBalance"},
			{Name: "Burn Address Balance", Method: "eth_getBalance"},
		},
	}
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"nethermind": {
				Name:          "nethermind",
				TotalRequests: 200,
				Methods: map[string]types.MetricSummary{
					"eth_getBalance": {Count: 200, SuccessRate: 100},
				},
				Calls: map[string]*types.MethodMetrics{
					"Vitalik's Balance":    callMetrics("Vitalik's Balance", "eth_getBalance", 100, 100, 12),
					"Burn Address Balance": callMetrics("Burn Address Balance", "eth_getBalance", 100, 99.5, 30),
				},
			},
		},
	}

	body := renderReport(t, cfg, result)

	assert.Contains(t, body, "Burn Address Balance")
	assert.Contains(t, body, "Vitalik&#39;s Balance", "the call name belongs in the Name column")
	assert.Equal(t, 2, strings.Count(body, "<strong>eth_getBalance</strong>"),
		"two calls on one method must be two rows, not one")
}

// A client name is data, and it reaches a JS string literal and an element id.
func TestReportSurvivesAClientNameThatIsNotAnIdentifier(t *testing.T) {
	cfg := &config.Config{TestName: "quoting", Description: "a <script> in the description"}
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"O'Brien-node": {
				Name:          "O'Brien-node",
				TotalRequests: 10,
				Calls:         map[string]*types.MethodMetrics{"call": callMetrics("call", "eth_call", 10, 100, 5)},
			},
		},
	}

	body := renderReport(t, cfg, result)

	assert.NotContains(t, body, "showTab('O'Brien-node'",
		"an apostrophe in a client name must not break out of the JS string literal")
	assert.Regexp(t, `showTab\(\s*0\s*,\s*this\)`, body, "the tab is addressed by position, not by name")
	assert.Contains(t, body, `id="tab-0"`)
	assert.NotContains(t, body, "<script> in the description", "the description must be escaped, not rendered")
}

// The engine distinguishes requests that were dropped from requests that went
// out behind schedule, and the report has to keep them apart: a run that sent
// everything did not offer less load than it was asked to.
func TestReportFlagsDroppedAndLateSeparately(t *testing.T) {
	cfg := &config.Config{TestName: "delivery"}

	late := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:     "geth",
				Delivery: types.DeliveryMetrics{Scheduled: 100, Sent: 100, Late: 7},
			},
		},
	}
	body := renderReport(t, cfg, late)
	assert.NotContains(t, body, "offered less load than requested",
		"every scheduled request was sent, so there was no shortfall")
	assert.Contains(t, body, "went out later than")

	dropped := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:     "geth",
				Delivery: types.DeliveryMetrics{Scheduled: 100, Sent: 90, Dropped: 10},
			},
		},
	}
	body = renderReport(t, cfg, dropped)
	assert.Contains(t, body, "offered less load than requested")
}

// A column nothing in the repo ever fills reads as a measurement of zero.
func TestReportOmitsFiguresNothingRecords(t *testing.T) {
	cfg := &config.Config{TestName: "connections"}
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:              "geth",
				ConnectionMetrics: types.ConnectionMetrics{ConnectionReuse: 99.5},
			},
		},
	}

	body := renderReport(t, cfg, result)

	assert.NotContains(t, body, "Avg Connections")
	assert.Contains(t, body, "Connection Reuse")
	assert.Contains(t, body, "99.5")
}

// The comparison section writes a score into a CSS width, which is the other
// context an escaping template has to get right.
func TestReportRendersTheComparisonSection(t *testing.T) {
	cfg := &config.Config{TestName: "comparison"}
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {Name: "geth", TotalRequests: 10},
		},
		Comparison:       &types.ComparisonResult{Winner: "geth", WinnerScore: 87.5},
		PerformanceScore: map[string]float64{"geth": 87.5},
		Recommendations:  []string{"raise the batch size"},
	}

	body := renderReport(t, cfg, result)

	assert.Contains(t, body, "width: 87.5%")
	assert.Contains(t, body, "Winner")
	assert.Contains(t, body, "raise the batch size")
}

// The report is one self-contained file. It used to pull a charting library off
// a CDN and prepare colour palettes for charts it never drew, which made an
// offline or air-gapped read of the report fetch something it did not need.
func TestReportLoadsNothingFromTheNetwork(t *testing.T) {
	cfg := &config.Config{TestName: "offline"}
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{"geth": {Name: "geth", TotalRequests: 1}},
	}

	body := renderReport(t, cfg, result)

	assert.NotContains(t, body, "http://")
	assert.NotContains(t, body, "https://")
	assert.NotContains(t, body, "<canvas")
}
