package storage

import (
	"database/sql"
	"fmt"
	"log"
)

// EnsureBenchmarkMetricsTable checks if the benchmark_metrics table exists and creates it if necessary
func EnsureBenchmarkMetricsTable(db *sql.DB) error {
	// Check if table exists
	var exists bool
	checkQuery := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'benchmark_metrics'
		);`

	err := db.QueryRow(checkQuery).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if benchmark_metrics table exists: %w", err)
	}

	if exists {
		log.Println("benchmark_metrics table already exists")
		return nil
	}

	// Create table if it doesn't exist
	log.Println("Creating benchmark_metrics table...")
	_, err = db.Exec(GrafanaMetricsTable)
	if err != nil {
		return fmt.Errorf("failed to create benchmark_metrics table: %w", err)
	}

	// Try to create hypertable if TimescaleDB is available
	// This will fail gracefully if TimescaleDB is not installed
	_, err = db.Exec(CreateHypertable)
	if err != nil {
		log.Printf("Note: Could not create hypertable (TimescaleDB might not be installed): %v", err)
		// This is not a fatal error, regular PostgreSQL table will work fine
	} else {
		log.Println("Created TimescaleDB hypertable for benchmark_metrics")
	}

	// Create indexes for better query performance
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_benchmark_metrics_run_id ON benchmark_metrics(run_id);",
		"CREATE INDEX IF NOT EXISTS idx_benchmark_metrics_method ON benchmark_metrics(method);",
		"CREATE INDEX IF NOT EXISTS idx_benchmark_metrics_metric_name ON benchmark_metrics(metric_name);",
		"CREATE INDEX IF NOT EXISTS idx_benchmark_metrics_time_run_id ON benchmark_metrics(time, run_id);",
	}

	for _, idx := range indexes {
		_, err = db.Exec(idx)
		if err != nil {
			log.Printf("Warning: Failed to create index: %v", err)
			// Non-fatal, continue
		}
	}

	log.Println("Successfully created benchmark_metrics table and indexes")
	return nil
}
