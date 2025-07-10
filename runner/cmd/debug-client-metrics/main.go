package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

func main() {
	var (
		runID         string
		storageConfig string
		verbose       bool
	)

	flag.StringVar(&runID, "run-id", "", "Run ID to debug client metrics for (required)")
	flag.StringVar(&storageConfig, "storage-config", "", "Path to storage configuration file")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
	flag.Parse()

	if runID == "" {
		fmt.Fprintf(os.Stderr, "Error: -run-id is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Setup logging
	logger := logrus.New()
	if verbose {
		logger.SetLevel(logrus.DebugLevel)
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	// Debug client metrics
	if err := debugClientMetrics(runID, storageConfig, logger); err != nil {
		logger.WithError(err).Fatal("Failed to debug client metrics")
	}
}

func debugClientMetrics(runID string, storageConfigPath string, logger *logrus.Logger) error {
	// Load storage configuration
	var storageCfg *config.StorageConfig
	if storageConfigPath != "" {
		cfg, err := config.LoadStorageConfig(storageConfigPath, logger)
		if err != nil {
			return fmt.Errorf("failed to load storage config: %w", err)
		}
		storageCfg = cfg
	} else {
		// Use default configuration
		logger.Info("No storage config path provided, using defaults")
		storageCfg = &config.StorageConfig{
			EnableHistoric: true,
			PostgreSQL: config.PostgreSQLConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "jsonrpc_bench",
				User:     "postgres",
				Password: "postgres",
			},
		}
	}

	// Connect to database
	db, err := sql.Open("postgres", storageCfg.PostgreSQL.ConnectionString())
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer db.Close()

	// Ping database
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	fmt.Printf("\nClient Metrics Debug for Run: %s\n", runID)
	fmt.Println("=" + string(make([]byte, 70)) + "=")

	// Get run information
	runInfo, err := getRunInfo(db, runID)
	if err != nil {
		return fmt.Errorf("failed to get run info: %w", err)
	}

	fmt.Printf("\nRun Information:\n")
	fmt.Printf("  ID: %s\n", runInfo.ID)
	fmt.Printf("  Test Name: %s\n", runInfo.TestName)
	fmt.Printf("  Timestamp: %s\n", runInfo.Timestamp)
	fmt.Printf("  Clients: %v\n", runInfo.Clients)
	fmt.Printf("  Total Requests: %d\n", runInfo.TotalRequests)
	fmt.Printf("  Success Rate: %.2f%%\n", runInfo.SuccessRate)

	// Extract and display client metrics from full_results
	clientMetrics, err := extractClientMetricsFromDB(db, runID)
	if err != nil {
		return fmt.Errorf("failed to extract client metrics: %w", err)
	}

	if len(clientMetrics) == 0 {
		fmt.Println("\n⚠️  No client metrics found in full_results")
		fmt.Println("This run may not have per-client data stored.")
	} else {
		fmt.Printf("\nRaw full_results size: %d bytes\n", len(runInfo.FullResults))
		fmt.Printf("Parsed client count: %d\n", len(clientMetrics))

		// Display metrics in table format
		fmt.Println("\nPer-Client Metrics:")
		fmt.Println("-" + string(make([]byte, 70)) + "-")

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "Client\tRequests\tErrors\tSuccess Rate\tAvg Latency\tP95 Latency\tP99 Latency")
		fmt.Fprintln(w, "------\t--------\t------\t------------\t-----------\t-----------\t-----------")

		for clientName, metrics := range clientMetrics {
			fmt.Fprintf(w, "%s\t%d\t%d\t%.2f%%\t%.2fms\t%.2fms\t%.2fms\n",
				clientName,
				metrics.TotalRequests,
				metrics.TotalErrors,
				100.0-metrics.ErrorRate, // Calculate success rate
				metrics.Latency.Avg,
				metrics.Latency.P95,
				metrics.Latency.P99,
			)
		}
		w.Flush()

		// Show method breakdown for each client if verbose
		if logger.GetLevel() >= logrus.DebugLevel {
			for clientName, metrics := range clientMetrics {
				fmt.Printf("\n\nMethods for %s:\n", clientName)
				fmt.Println("-" + string(make([]byte, 50)) + "-")

				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "Method\tCount\tSuccess Rate\tAvg\tP99")
				fmt.Fprintln(w, "------\t-----\t------------\t---\t---")

				for methodName, method := range metrics.Methods {
					fmt.Fprintf(w, "%s\t%d\t%.2f%%\t%.2fms\t%.2fms\n",
						methodName,
						method.Count,
						method.SuccessRate,
						method.Avg,
						method.P99,
					)
				}
				w.Flush()
			}
		}
	}

	// Check if metrics are in benchmark_metrics table
	fmt.Println("\n\nChecking benchmark_metrics table:")
	metricsCount, err := checkBenchmarkMetrics(db, runID)
	if err != nil {
		logger.WithError(err).Warn("Failed to check benchmark_metrics")
	} else {
		fmt.Printf("Found %d metric entries for this run\n", metricsCount)
	}

	// Recommendations
	fmt.Println("\n\nRecommendations:")
	fmt.Println("-" + string(make([]byte, 70)) + "-")
	if len(clientMetrics) == 0 {
		fmt.Println("❌ No per-client metrics found. Possible causes:")
		fmt.Println("   1. The benchmark was run before per-client tracking was implemented")
		fmt.Println("   2. The K6 script is not correctly collecting per-client metrics")
		fmt.Println("   3. The metrics parser failed to extract client-specific data")
		fmt.Println("\n   Action: Re-run the benchmark with the latest version")
	} else {
		fmt.Println("✅ Per-client metrics are available")
		fmt.Println("   - Data is properly stored in the database")
		fmt.Println("   - API should return client_metrics in the response")
		fmt.Println("   - UI should display individual client performance")
	}

	return nil
}

// Helper types and functions
type runInfo struct {
	ID            string
	TestName      string
	Timestamp     string
	Clients       []string
	TotalRequests int
	SuccessRate   float64
	FullResults   []byte
}

func getRunInfo(db *sql.DB, runID string) (*runInfo, error) {
	// First try benchmark_runs table (runner schema)
	query := `
		SELECT id, test_name, timestamp, clients, total_requests, success_rate, metadata
		FROM benchmark_runs
		WHERE id = $1
	`

	var info runInfo
	var clientsJSON []byte
	var metadataJSON []byte

	err := db.QueryRow(query, runID).Scan(
		&info.ID,
		&info.TestName,
		&info.Timestamp,
		&clientsJSON,
		&info.TotalRequests,
		&info.SuccessRate,
		&metadataJSON,
	)

	if err == nil {
		// Parse clients array
		if err := json.Unmarshal(clientsJSON, &info.Clients); err != nil {
			return nil, fmt.Errorf("failed to parse clients: %w", err)
		}
		// Use metadata as full_results for now
		info.FullResults = metadataJSON
		return &info, nil
	}

	// If not found, try historic_runs table (metrics schema)
	if err == sql.ErrNoRows {
		query = `
			SELECT id, test_name, timestamp, client_metrics, total_requests, 
				   COALESCE(100 - overall_error_rate, 100) as success_rate
			FROM historic_runs
			WHERE id = $1
		`

		var clientMetricsJSON []byte
		err = db.QueryRow(query, runID).Scan(
			&info.ID,
			&info.TestName,
			&info.Timestamp,
			&clientMetricsJSON,
			&info.TotalRequests,
			&info.SuccessRate,
		)

		if err != nil {
			return nil, fmt.Errorf("run not found in either benchmark_runs or historic_runs table: %w", err)
		}

		// Extract client names from client_metrics JSON
		if len(clientMetricsJSON) > 0 {
			var clientMetrics map[string]interface{}
			if err := json.Unmarshal(clientMetricsJSON, &clientMetrics); err == nil {
				info.Clients = make([]string, 0, len(clientMetrics))
				for client := range clientMetrics {
					info.Clients = append(info.Clients, client)
				}
			}
		}

		// For historic_runs, we'll need to get full_results separately
		info.FullResults = nil
	}

	return &info, nil
}

func extractClientMetricsFromDB(db *sql.DB, runID string) (map[string]*types.ClientMetrics, error) {
	// First try benchmark_runs table with metadata column
	var metadataJSON []byte
	query := `SELECT metadata FROM benchmark_runs WHERE id = $1`

	err := db.QueryRow(query, runID).Scan(&metadataJSON)
	if err == nil && len(metadataJSON) > 0 {
		// Try to extract client metrics from metadata
		var metadata map[string]interface{}
		if err := json.Unmarshal(metadataJSON, &metadata); err == nil {
			if fullResults, ok := metadata["full_results"]; ok {
				// Marshal back to JSON to parse as BenchmarkResult
				fullResultsBytes, _ := json.Marshal(fullResults)
				var benchmarkResult types.BenchmarkResult
				if err := json.Unmarshal(fullResultsBytes, &benchmarkResult); err == nil {
					return benchmarkResult.ClientMetrics, nil
				}
			}
		}
	}

	// Try historic_runs table with client_metrics column
	if err == sql.ErrNoRows {
		var clientMetricsJSON []byte
		query = `SELECT client_metrics FROM historic_runs WHERE id = $1`

		err = db.QueryRow(query, runID).Scan(&clientMetricsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to query client metrics: %w", err)
		}

		if len(clientMetricsJSON) == 0 {
			return nil, nil
		}

		// Parse directly as client metrics map
		var clientMetrics map[string]*types.ClientMetrics
		if err := json.Unmarshal(clientMetricsJSON, &clientMetrics); err != nil {
			return nil, fmt.Errorf("failed to parse client_metrics JSON: %w", err)
		}

		return clientMetrics, nil
	}

	return nil, nil
}

func checkBenchmarkMetrics(db *sql.DB, runID string) (int, error) {
	query := `SELECT COUNT(*) FROM benchmark_metrics WHERE run_id = $1`

	var count int
	err := db.QueryRow(query, runID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}
