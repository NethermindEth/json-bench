package storage

import (
	"database/sql"
	"fmt"

	"github.com/sirupsen/logrus"
)

// Migration represents a database migration
type Migration struct {
	Version int
	SQL     string
}

// RunMigrations runs all database migrations
func RunMigrations(db *sql.DB) error {
	log := logrus.WithField("component", "migration")

	// Create migration tracking table
	if err := createMigrationTable(db); err != nil {
		return err
	}

	// Execute migrations
	for _, migration := range migrations {
		applied, err := isMigrationApplied(db, migration.Version)
		if err != nil {
			return err
		}

		if applied {
			log.WithField("version", migration.Version).Debug("Migration already applied")
			continue
		}

		log.WithField("version", migration.Version).Info("Applying migration")
		if err := applyMigration(db, migration); err != nil {
			return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
		}
	}

	return nil
}

// migrations defines all database migrations
var migrations = []Migration{
	{Version: 1, SQL: GrafanaRunsTable},
	{Version: 2, SQL: GrafanaMetricsTable},
	{Version: 3, SQL: CreateIndices()},
	{Version: 4, SQL: CreateHypertable}, // Optional: for TimescaleDB
}

// CreateIndices returns SQL for creating performance indices
func CreateIndices() string {
	return `
	CREATE INDEX IF NOT EXISTS idx_runs_timestamp ON benchmark_runs(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_runs_git_branch ON benchmark_runs(git_branch);
	CREATE INDEX IF NOT EXISTS idx_metrics_time ON benchmark_metrics(time DESC);
	CREATE INDEX IF NOT EXISTS idx_metrics_client_method ON benchmark_metrics(client, method);
	CREATE INDEX IF NOT EXISTS idx_metrics_run_id ON benchmark_metrics(run_id);
	`
}

// createMigrationTable creates the migration tracking table
func createMigrationTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	)`

	_, err := db.Exec(query)
	return err
}

// isMigrationApplied checks if a migration version has been applied
func isMigrationApplied(db *sql.DB, version int) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM schema_migrations WHERE version = $1`
	err := db.QueryRow(query, version).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// applyMigration applies a single migration
func applyMigration(db *sql.DB, migration Migration) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Special handling for TimescaleDB migration (version 4)
	if migration.Version == 4 {
		// Check if TimescaleDB extension is available
		var extensionExists bool
		err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'timescaledb')").Scan(&extensionExists)
		if err != nil {
			logrus.WithField("component", "migration").Warn("Could not check for TimescaleDB extension, skipping hypertable creation")
		} else if !extensionExists {
			logrus.WithField("component", "migration").Info("TimescaleDB not available, skipping hypertable creation")
		} else {
			// TimescaleDB is available, execute the migration
			if _, err := tx.Exec(migration.SQL); err != nil {
				logrus.WithField("component", "migration").WithError(err).Warn("Failed to create hypertable, continuing without it")
			}
		}
	} else {
		// Execute normal migration SQL
		if _, err := tx.Exec(migration.SQL); err != nil {
			return fmt.Errorf("failed to execute migration SQL: %w", err)
		}
	}

	// Record migration
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, migration.Version); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}
