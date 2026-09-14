package exporter

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/types"
)

func writeMethodMetrics(t *testing.T, result *types.BenchmarkResult) ([]string, [][]string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "method_metrics.csv")
	if err := NewDataExporter(t.TempDir()).ExportMethodMetricsCSV(result, path); err != nil {
		t.Fatalf("ExportMethodMetricsCSV: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open export: %v", err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("parse export: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("export has no header")
	}
	return records[0], records[1:]
}

func columnValue(t *testing.T, header, row []string, name string) string {
	t.Helper()
	for i, h := range header {
		if h == name {
			return row[i]
		}
	}
	t.Fatalf("no %q column in %v", name, header)
	return ""
}

func TestExportMethodMetricsCSV_ReportsEveryStatistic(t *testing.T) {
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"geth": {
				Name:          "geth",
				TotalRequests: 3600,
				Methods: map[string]types.MetricSummary{
					"eth_call": {
						Count: 3600, Avg: 57.8, Min: 5, Max: 500, P50: 50, P75: 75, P90: 90,
						P95: 95, P99: 99, P999: 400, StdDev: 40.5, Variance: 1640.25,
						IQR: 45, MAD: 22.5, ErrorCount: 12, SuccessRate: 99.67,
						Throughput: 20,
					},
				},
			},
		},
	}

	header, rows := writeMethodMetrics(t, result)
	if len(rows) != 1 {
		t.Fatalf("expected one data row, got %d", len(rows))
	}
	row := rows[0]

	// Every one of these needs per-sample data. The engine retains samples, so
	// they carry real values rather than the placeholder the k6 pipeline had to
	// print because its aggregates could not supply them.
	for col, want := range map[string]string{
		"P75 (ms)":   "75.00",
		"P99.9 (ms)": "400.00",
		"Variance":   "1640.25",
		"IQR":        "45.00",
		"MAD":        "22.50",
	} {
		if got := columnValue(t, header, row, col); got != want {
			t.Errorf("%s = %q, want %q", col, got, want)
		}
	}

	// A genuine zero means none were observed, and must not read as unknown.
	for _, col := range []string{"Timeout Rate (%)", "Connection Errors"} {
		if got := columnValue(t, header, row, col); got != "0.00" && got != "0" {
			t.Errorf("%s = %q, want a measured zero", col, got)
		}
	}

	// Error Count is the measured count, not a re-derivation from the rate.
	if got := columnValue(t, header, row, "Error Count"); got != "12" {
		t.Errorf("Error Count = %q, want 12", got)
	}
	if got := columnValue(t, header, row, "Throughput (req/s)"); got != "20.00" {
		t.Errorf("Throughput = %q, want 20.00", got)
	}
}

func TestExportMethodMetricsCSV_EmptyMetricsWritesHeaderOnly(t *testing.T) {
	header, rows := writeMethodMetrics(t, &types.BenchmarkResult{})
	if len(header) == 0 {
		t.Error("expected a header even with no metrics")
	}
	if len(rows) != 0 {
		t.Errorf("expected no data rows, got %d", len(rows))
	}
}

func TestExportClientComparisonCSV_ShowsUnderDeliveredLoad(t *testing.T) {
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"nethermind": {
				Name:          "nethermind",
				TotalRequests: 181,
				TotalErrors:   28,
				ErrorRate:     15.47,
				Delivery: types.DeliveryMetrics{
					Scheduled: 240, Sent: 181, Late: 0, Dropped: 59,
					TargetRPS: 30, AchievedRPS: 22.6, MaxDispatchDelayMs: 0,
				},
				Outcomes: map[string]int64{
					"ok": 153, "rpc_error": 21, "http_error": 6, "truncated": 1,
				},
			},
		},
	}

	header, rows := writeClientComparison(t, result)
	require.Len(t, rows, 1)
	row := rows[0]

	// The shortfall has to be readable without opening anything else: the
	// latency columns on this row describe only the 181 requests that went out.
	for col, want := range map[string]string{
		"Scheduled":     "240",
		"Sent":          "181",
		"Dropped":       "59",
		"Delivered (%)": "75.42",
		"Target RPS":    "30.0",
		"Achieved RPS":  "22.6",
		"RPC Errors":    "21",
		"HTTP Errors":   "6",
		"Truncated":     "1",
		"OK":            "153",
	} {
		if got := columnValue(t, header, row, col); got != want {
			t.Errorf("%s = %q, want %q", col, got, want)
		}
	}
}

// The score normalises across the clients in a run, so with one client there is
// nothing to normalise against and a numeric 0 would read as the worst possible
// result rather than as not applicable.
func TestExportClientComparisonCSV_MarksTheScoreNotApplicable(t *testing.T) {
	result := &types.BenchmarkResult{
		ClientMetrics: map[string]*types.ClientMetrics{
			"only": {Name: "only", TotalRequests: 10},
		},
	}

	header, rows := writeClientComparison(t, result)
	require.Len(t, rows, 1)
	if got := columnValue(t, header, rows[0], "Performance Score"); got != "NA" {
		t.Errorf("Performance Score = %q, want NA", got)
	}
}

func writeClientComparison(t *testing.T, result *types.BenchmarkResult) ([]string, [][]string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "client_comparison.csv")
	if err := NewDataExporter(dir).ExportClientComparisonCSV(result, path); err != nil {
		t.Fatalf("export: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("no records written")
	}
	return records[0], records[1:]
}
