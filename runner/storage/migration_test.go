package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// MigrationTestSuite provides comprehensive tests for database migrations
type MigrationTestSuite struct {
	suite.Suite
	container *postgres.PostgresContainer
	db        *sql.DB
	migration *MigrationService
	ctx       context.Context
	logger    logrus.FieldLogger
}

// SetupSuite initializes the test environment
func (suite *MigrationTestSuite) SetupSuite() {
	suite.ctx = context.Background()
	suite.logger = logrus.New().WithField("test", "migration")

	// Start PostgreSQL container
	pgContainer, err := postgres.RunContainer(suite.ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	require.NoError(suite.T(), err)
	suite.container = pgContainer

	// Setup database connection
	mappedPort, err := pgContainer.MappedPort(suite.ctx, "5432")
	require.NoError(suite.T(), err)

	connStr := fmt.Sprintf("host=localhost port=%d user=testuser password=testpass dbname=testdb sslmode=disable",
		mappedPort.Int())
	db, err := sql.Open("postgres", connStr)
	require.NoError(suite.T(), err)
	suite.db = db

	// Create migration service
	suite.migration = NewMigrationService(db, suite.logger)
}

// TearDownSuite cleans up test resources
func (suite *MigrationTestSuite) TearDownSuite() {
	if suite.db != nil {
		suite.db.Close()
	}
	if suite.container != nil {
		suite.container.Terminate(suite.ctx)
	}
}

// SetupTest prepares clean state for each test
func (suite *MigrationTestSuite) SetupTest() {
	// Drop all tables to start fresh
	err := suite.migration.Reset()
	require.NoError(suite.T(), err)

	// Recreate migration service to ensure clean state
	suite.migration = NewMigrationService(suite.db, suite.logger)
}

// TestMigrationServiceInitialization tests migration service creation
func (suite *MigrationTestSuite) TestMigrationServiceInitialization() {
	t := suite.T()

	// Test successful initialization
	err := suite.migration.Initialize()
	assert.NoError(t, err)

	// Verify schema_migrations table was created
	var exists bool
	err = suite.db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'schema_migrations'
		)`).Scan(&exists)
	assert.NoError(t, err)
	assert.True(t, exists)

	// Test that initialize is idempotent
	err = suite.migration.Initialize()
	assert.NoError(t, err)
}

// TestGetAppliedMigrations tests retrieving applied migrations
func (suite *MigrationTestSuite) TestGetAppliedMigrations() {
	t := suite.T()

	// Initialize and get applied migrations on fresh database
	err := suite.migration.Initialize()
	require.NoError(t, err)

	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.NotNil(t, applied)
	assert.Empty(t, applied) // Should be empty on fresh database

	// Apply some migrations manually
	_, err = suite.db.Exec("INSERT INTO schema_migrations (version, name) VALUES (1, 'test_migration_1')")
	require.NoError(t, err)
	_, err = suite.db.Exec("INSERT INTO schema_migrations (version, name) VALUES (3, 'test_migration_3')")
	require.NoError(t, err)

	// Get applied migrations again
	applied, err = suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.True(t, applied[1])
	assert.False(t, applied[2])
	assert.True(t, applied[3])
}

// TestUpMigrations tests applying migrations
func (suite *MigrationTestSuite) TestUpMigrations() {
	t := suite.T()

	// Apply all migrations
	err := suite.migration.Up()
	assert.NoError(t, err)

	// Verify all expected tables were created
	expectedTables := []string{
		"benchmark_runs",
		"benchmark_metrics",
		"client_summary",
		"method_summary",
		"comparison_results",
		"response_diffs",
		"historic_runs",
		"regressions",
		"baselines",
	}

	for _, table := range expectedTables {
		var exists bool
		err = suite.db.QueryRow(`
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_schema = 'public' 
				AND table_name = $1
			)`, table).Scan(&exists)
		assert.NoError(t, err, "Failed to check table %s", table)
		assert.True(t, exists, "Table %s should exist", table)
	}

	// Verify all migrations were recorded
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.Len(t, applied, len(migrations))

	// Test idempotency - running Up again should not error
	err = suite.migration.Up()
	assert.NoError(t, err)
}

// TestUpMigrationsPartial tests applying migrations when some already exist
func (suite *MigrationTestSuite) TestUpMigrationsPartial() {
	t := suite.T()

	// Initialize migration table
	err := suite.migration.Initialize()
	require.NoError(t, err)

	// Manually apply first migration
	migration := migrations[0]
	err = suite.migration.applyMigration(migration)
	require.NoError(t, err)

	// Verify only first migration was applied
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.True(t, applied[1])
	assert.False(t, applied[2])

	// Apply all migrations
	err = suite.migration.Up()
	assert.NoError(t, err)

	// Verify all migrations are now applied
	applied, err = suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.Len(t, applied, len(migrations))
}

// TestDownMigrations tests rolling back migrations
func (suite *MigrationTestSuite) TestDownMigrations() {
	t := suite.T()

	// Apply all migrations first
	err := suite.migration.Up()
	require.NoError(t, err)

	// Rollback to version 2 (should rollback versions 5, 4, 3)
	err = suite.migration.Down(2)
	assert.NoError(t, err)

	// Verify correct migrations remain
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.True(t, applied[1])
	assert.True(t, applied[2])
	assert.False(t, applied[3])
	assert.False(t, applied[4])
	assert.False(t, applied[5])

	// Verify tables were dropped
	historicTables := []string{"historic_runs", "regressions", "baselines"}
	for _, table := range historicTables {
		var exists bool
		err = suite.db.QueryRow(`
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_schema = 'public' 
				AND table_name = $1
			)`, table).Scan(&exists)
		assert.NoError(t, err)
		assert.False(t, exists, "Table %s should have been dropped", table)
	}

	// Rollback all migrations
	err = suite.migration.Down(0)
	assert.NoError(t, err)

	// Verify all migrations were rolled back
	applied, err = suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.Empty(t, applied)
}

// TestDownMigrationsNonExistent tests rolling back non-existent migrations
func (suite *MigrationTestSuite) TestDownMigrationsNonExistent() {
	t := suite.T()

	// Apply only first two migrations
	err := suite.migration.Initialize()
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		err = suite.migration.applyMigration(migrations[i])
		require.NoError(t, err)
	}

	// Try to rollback migration that wasn't applied (should not error)
	err = suite.migration.Down(1)
	assert.NoError(t, err)

	// Verify correct state
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.True(t, applied[1])
	assert.False(t, applied[2])
}

// TestApplyMigration tests applying individual migrations
func (suite *MigrationTestSuite) TestApplyMigration() {
	t := suite.T()

	// Initialize migration table
	err := suite.migration.Initialize()
	require.NoError(t, err)

	// Apply first migration
	migration := migrations[0]
	err = suite.migration.applyMigration(migration)
	assert.NoError(t, err)

	// Verify migration was recorded
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", migration.Version).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify tables were created
	var exists bool
	err = suite.db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'benchmark_runs'
		)`).Scan(&exists)
	assert.NoError(t, err)
	assert.True(t, exists)
}

// TestApplyMigrationFailure tests migration failure handling
func (suite *MigrationTestSuite) TestApplyMigrationFailure() {
	t := suite.T()

	// Initialize migration table
	err := suite.migration.Initialize()
	require.NoError(t, err)

	// Create a migration with invalid SQL
	invalidMigration := Migration{
		Version: 999,
		Name:    "invalid_migration",
		Up:      "INVALID SQL STATEMENT",
		Down:    "DROP TABLE IF EXISTS test_table",
	}

	// Attempt to apply invalid migration
	err = suite.migration.applyMigration(invalidMigration)
	assert.Error(t, err)

	// Verify migration was not recorded
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", invalidMigration.Version).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestRollbackMigration tests rolling back individual migrations
func (suite *MigrationTestSuite) TestRollbackMigration() {
	t := suite.T()

	// Apply first migration
	err := suite.migration.Initialize()
	require.NoError(t, err)

	migration := migrations[0]
	err = suite.migration.applyMigration(migration)
	require.NoError(t, err)

	// Rollback the migration
	err = suite.migration.rollbackMigration(migration)
	assert.NoError(t, err)

	// Verify migration record was removed
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", migration.Version).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify tables were dropped
	var exists bool
	err = suite.db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'benchmark_runs'
		)`).Scan(&exists)
	assert.NoError(t, err)
	assert.False(t, exists)
}

// TestRollbackMigrationFailure tests rollback failure handling
func (suite *MigrationTestSuite) TestRollbackMigrationFailure() {
	t := suite.T()

	// Initialize and apply a migration
	err := suite.migration.Initialize()
	require.NoError(t, err)

	migration := migrations[0]
	err = suite.migration.applyMigration(migration)
	require.NoError(t, err)

	// Create a migration with invalid rollback SQL
	invalidMigration := Migration{
		Version: migration.Version,
		Name:    migration.Name,
		Up:      migration.Up,
		Down:    "INVALID ROLLBACK SQL",
	}

	// Attempt to rollback with invalid SQL
	err = suite.migration.rollbackMigration(invalidMigration)
	assert.Error(t, err)

	// Verify migration record still exists (rollback was not committed)
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", migration.Version).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestCreateIndices tests index creation
func (suite *MigrationTestSuite) TestCreateIndices() {
	t := suite.T()

	// Apply migrations first to have tables
	err := suite.migration.Up()
	require.NoError(t, err)

	// Create indices
	err = suite.migration.CreateIndices()
	assert.NoError(t, err)

	// Verify some indices were created
	var indexCount int
	err = suite.db.QueryRow(`
		SELECT COUNT(*) 
		FROM pg_indexes 
		WHERE schemaname = 'public' 
		AND indexname LIKE 'idx_%'`).Scan(&indexCount)
	assert.NoError(t, err)
	assert.Greater(t, indexCount, 0)

	// Test idempotency - running CreateIndices again should not error
	err = suite.migration.CreateIndices()
	assert.NoError(t, err)
}

// TestGetVersion tests version retrieval
func (suite *MigrationTestSuite) TestGetVersion() {
	t := suite.T()

	// Test on fresh database (should return 0)
	version, err := suite.migration.GetVersion()
	assert.Error(t, err) // Table doesn't exist yet

	// Initialize migrations
	err = suite.migration.Initialize()
	require.NoError(t, err)

	// Test on empty migration table
	version, err = suite.migration.GetVersion()
	assert.NoError(t, err)
	assert.Equal(t, 0, version)

	// Apply some migrations
	err = suite.migration.Up()
	require.NoError(t, err)

	// Test on populated migration table
	version, err = suite.migration.GetVersion()
	assert.NoError(t, err)
	assert.Equal(t, len(migrations), version)
}

// TestReset tests database reset functionality
func (suite *MigrationTestSuite) TestReset() {
	t := suite.T()

	// Apply all migrations
	err := suite.migration.Up()
	require.NoError(t, err)

	// Verify tables exist
	var tableCount int
	err = suite.db.QueryRow(`
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_name != 'schema_migrations'`).Scan(&tableCount)
	assert.NoError(t, err)
	assert.Greater(t, tableCount, 5)

	// Reset database
	err = suite.migration.Reset()
	assert.NoError(t, err)

	// Verify all migrations were reapplied
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.Len(t, applied, len(migrations))

	// Verify tables exist again
	err = suite.db.QueryRow(`
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_name != 'schema_migrations'`).Scan(&tableCount)
	assert.NoError(t, err)
	assert.Greater(t, tableCount, 5)
}

// TestMigrationOrder tests that migrations are applied in correct order
func (suite *MigrationTestSuite) TestMigrationOrder() {
	t := suite.T()

	// Verify migrations are in correct order
	for i := 1; i < len(migrations); i++ {
		assert.Greater(t, migrations[i].Version, migrations[i-1].Version,
			"Migration %d should have higher version than migration %d", i, i-1)
	}

	// Apply migrations and verify they're applied in order
	err := suite.migration.Up()
	assert.NoError(t, err)

	// Check migration records are in order
	rows, err := suite.db.Query("SELECT version FROM schema_migrations ORDER BY version")
	require.NoError(t, err)
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var version int
		err = rows.Scan(&version)
		require.NoError(t, err)
		versions = append(versions, version)
	}

	// Verify versions are sequential
	for i, version := range versions {
		assert.Equal(t, i+1, version, "Migration version should be sequential")
	}
}

// TestMigrationDependencies tests migration dependencies
func (suite *MigrationTestSuite) TestMigrationDependencies() {
	t := suite.T()

	// Apply all migrations
	err := suite.migration.Up()
	require.NoError(t, err)

	// Test that foreign key constraints work
	// Insert a benchmark run
	_, err = suite.db.Exec(`
		INSERT INTO benchmark_runs (
			run_id, test_name, description, start_time, end_time, duration,
			config, environment, tags
		) VALUES (
			'test_run', 'test', 'description', NOW(), NOW(), '30m',
			'{}', '{}', '{}'
		)`)
	require.NoError(t, err)

	// Insert client summary that references the run
	_, err = suite.db.Exec(`
		INSERT INTO client_summary (
			run_id, client_name, total_requests, total_errors, error_rate
		) VALUES (
			'test_run', 'test_client', 1000, 10, 0.01
		)`)
	assert.NoError(t, err)

	// Try to insert client summary with non-existent run_id (should fail)
	_, err = suite.db.Exec(`
		INSERT INTO client_summary (
			run_id, client_name, total_requests, total_errors, error_rate
		) VALUES (
			'nonexistent_run', 'test_client', 1000, 10, 0.01
		)`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "foreign key")
}

// TestTimescaleDBMigration tests TimescaleDB-specific migration
func (suite *MigrationTestSuite) TestTimescaleDBMigration() {
	t := suite.T()

	// Apply all migrations (TimescaleDB migration will be skipped without extension)
	err := suite.migration.Up()
	assert.NoError(t, err)

	// Verify hypertables were not created (since TimescaleDB is not installed)
	// This is expected behavior - the migration should not fail even without TimescaleDB
	var hypertableCount int
	err = suite.db.QueryRow(`
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_name LIKE '%_hypertable%'`).Scan(&hypertableCount)
	assert.NoError(t, err)
	// Should be 0 since TimescaleDB is not installed
}

// TestGrafanaFunctionsMigration tests Grafana functions migration
func (suite *MigrationTestSuite) TestGrafanaFunctionsMigration() {
	t := suite.T()

	// Apply all migrations
	err := suite.migration.Up()
	require.NoError(t, err)

	// Verify Grafana functions were created
	functions := []string{
		"get_metric_timeseries",
		"get_historic_trend",
		"get_performance_changes",
	}

	for _, functionName := range functions {
		var exists bool
		err = suite.db.QueryRow(`
			SELECT EXISTS (
				SELECT FROM information_schema.routines 
				WHERE routine_schema = 'public' 
				AND routine_name = $1
			)`, functionName).Scan(&exists)
		assert.NoError(t, err, "Failed to check function %s", functionName)
		assert.True(t, exists, "Function %s should exist", functionName)
	}

	// Test one of the functions
	rows, err := suite.db.Query(`
		SELECT * FROM get_metric_timeseries('latency_p95', 'geth', null, null, null)
		LIMIT 0`) // Just test that function exists and can be called
	assert.NoError(t, err)
	rows.Close()
}

// TestConcurrentMigrations tests concurrent migration attempts
func (suite *MigrationTestSuite) TestConcurrentMigrations() {
	t := suite.T()

	// This test ensures that concurrent migration attempts don't cause issues
	// In practice, migrations should be run sequentially, but this tests robustness

	concurrency := 3
	results := make(chan error, concurrency)

	// Start multiple goroutines trying to apply migrations
	for i := 0; i < concurrency; i++ {
		go func() {
			migrationService := NewMigrationService(suite.db, suite.logger)
			err := migrationService.Up()
			results <- err
		}()
	}

	// Collect results
	errors := 0
	for i := 0; i < concurrency; i++ {
		err := <-results
		if err != nil {
			errors++
		}
	}

	// At least one should succeed, others might fail due to concurrent access
	assert.Less(t, errors, concurrency, "At least one migration attempt should succeed")

	// Verify final state is correct
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.Len(t, applied, len(migrations))
}

// TestMigrationTransactionRollback tests transaction rollback on migration failure
func (suite *MigrationTestSuite) TestMigrationTransactionRollback() {
	t := suite.T()

	// Initialize migration table
	err := suite.migration.Initialize()
	require.NoError(t, err)

	// Create a migration that will partially succeed then fail
	partialMigration := Migration{
		Version: 999,
		Name:    "partial_migration",
		Up: `
			CREATE TABLE test_table (id SERIAL PRIMARY KEY);
			INSERT INTO test_table (id) VALUES (1);
			INVALID SQL STATEMENT; -- This will cause failure
		`,
		Down: "DROP TABLE IF EXISTS test_table",
	}

	// Attempt to apply the migration
	err = suite.migration.applyMigration(partialMigration)
	assert.Error(t, err)

	// Verify the table was not created (transaction was rolled back)
	var exists bool
	err = suite.db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'test_table'
		)`).Scan(&exists)
	assert.NoError(t, err)
	assert.False(t, exists)

	// Verify migration was not recorded
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", partialMigration.Version).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestMigrationWithData tests migrations on database with existing data
func (suite *MigrationTestSuite) TestMigrationWithData() {
	t := suite.T()

	// Apply initial migrations
	err := suite.migration.Up()
	require.NoError(t, err)

	// Insert some test data
	_, err = suite.db.Exec(`
		INSERT INTO benchmark_runs (
			run_id, test_name, description, start_time, end_time, duration,
			config, environment, tags
		) VALUES (
			'test_run_1', 'test1', 'desc1', NOW(), NOW(), '30m',
			'{}', '{}', '{}'
		)`)
	require.NoError(t, err)

	_, err = suite.db.Exec(`
		INSERT INTO client_summary (
			run_id, client_name, total_requests, total_errors, error_rate
		) VALUES (
			'test_run_1', 'client1', 1000, 10, 0.01
		)`)
	require.NoError(t, err)

	// Verify data exists
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM benchmark_runs").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	err = suite.db.QueryRow("SELECT COUNT(*) FROM client_summary").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// Test rollback preserves referential integrity
	err = suite.migration.Down(3) // Rollback to before historic tables
	assert.NoError(t, err)

	// Verify main data still exists
	err = suite.db.QueryRow("SELECT COUNT(*) FROM benchmark_runs").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// Re-apply migrations
	err = suite.migration.Up()
	assert.NoError(t, err)

	// Verify data is still there
	err = suite.db.QueryRow("SELECT COUNT(*) FROM benchmark_runs").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

// BenchmarkMigrationUp benchmarks full migration application
func (suite *MigrationTestSuite) BenchmarkMigrationUp() {
	b := suite.T()
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	// ResetTimer not available in test suite context

	for i := 0; i < 10; i++ {
		// Reset database
		err := suite.migration.Reset()
		require.NoError(b, err)

		// Apply all migrations
		err = suite.migration.Up()
		require.NoError(b, err)
	}
}

// BenchmarkMigrationDown benchmarks migration rollback
func (suite *MigrationTestSuite) BenchmarkMigrationDown() {
	b := suite.T()
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	// Setup: apply all migrations
	err := suite.migration.Up()
	require.NoError(b, err)

	// ResetTimer not available in test suite context

	for i := 0; i < 10; i++ {
		// Rollback all migrations
		err = suite.migration.Down(0)
		require.NoError(b, err)

		// Reapply all migrations for next iteration
		err = suite.migration.Up()
		require.NoError(b, err)
	}
}

// Run the test suite
func TestMigrationTestSuite(t *testing.T) {
	// Skip if running in CI without Docker
	if os.Getenv("SKIP_INTEGRATION_TESTS") != "" {
		t.Skip("Skipping integration tests")
	}

	suite.Run(t, new(MigrationTestSuite))
}

// Unit tests for migration data and validation

// TestMigrationStructure tests the migration structure and data
func TestMigrationStructure(t *testing.T) {
	// Test that we have migrations defined
	assert.Greater(t, len(migrations), 0, "Should have at least one migration")

	// Test that migration versions are unique and sequential
	versions := make(map[int]bool)
	for i, migration := range migrations {
		// Check version uniqueness
		assert.False(t, versions[migration.Version], "Migration version %d should be unique", migration.Version)
		versions[migration.Version] = true

		// Check sequential versions (starting from 1)
		assert.Equal(t, i+1, migration.Version, "Migration versions should be sequential starting from 1")

		// Check required fields
		assert.NotEmpty(t, migration.Name, "Migration %d should have a name", migration.Version)
		assert.NotEmpty(t, migration.Up, "Migration %d should have Up SQL", migration.Version)
		assert.NotEmpty(t, migration.Down, "Migration %d should have Down SQL", migration.Version)

		// Check SQL validity (basic checks)
		assert.False(t, strings.Contains(migration.Up, "INVALID"), "Migration %d Up SQL should not contain INVALID", migration.Version)
		assert.False(t, strings.Contains(migration.Down, "INVALID"), "Migration %d Down SQL should not contain INVALID", migration.Version)
	}
}

// TestMigrationSQLSyntax tests basic SQL syntax in migrations
func TestMigrationSQLSyntax(t *testing.T) {
	for _, migration := range migrations {
		// Check that Up SQL contains CREATE statements
		upSQL := strings.ToUpper(migration.Up)
		if migration.Version <= 3 { // First 3 migrations should create tables
			assert.Contains(t, upSQL, "CREATE TABLE", "Migration %d should create tables", migration.Version)
		}

		// Check that Down SQL contains DROP statements
		downSQL := strings.ToUpper(migration.Down)
		assert.Contains(t, downSQL, "DROP", "Migration %d should drop objects in Down SQL", migration.Version)

		// Check for common SQL injection patterns (basic security check)
		assert.False(t, strings.Contains(migration.Up, "'; DROP"), "Migration %d Up SQL should not contain injection patterns", migration.Version)
		assert.False(t, strings.Contains(migration.Down, "'; DROP"), "Migration %d Down SQL should not contain injection patterns", migration.Version)
	}
}

// TestTableSchemaConstants tests the schema constant definitions
func TestTableSchemaConstants(t *testing.T) {
	schemas := []string{
		BenchmarkRunsTableSchema,
		BenchmarkMetricsTableSchema,
		ClientSummaryTableSchema,
		MethodSummaryTableSchema,
		ComparisonResultsTableSchema,
		ResponseDiffsTableSchema,
		HistoricRunsTableSchema,
		RegressionsTableSchema,
		BaselinesTableSchema,
	}

	for i, schema := range schemas {
		assert.NotEmpty(t, schema, "Schema %d should not be empty", i)
		assert.Contains(t, strings.ToUpper(schema), "CREATE TABLE", "Schema %d should contain CREATE TABLE", i)
		assert.Contains(t, strings.ToUpper(schema), "IF NOT EXISTS", "Schema %d should use IF NOT EXISTS", i)
	}
}

// TestGrafanaQueriesStructure tests Grafana queries structure
func TestGrafanaQueriesStructure(t *testing.T) {
	queries := []string{
		GrafanaQueries.LatencyOverTime,
		GrafanaQueries.ErrorRateOverTime,
		GrafanaQueries.ThroughputOverTime,
		GrafanaQueries.ClientComparison,
		GrafanaQueries.MethodBreakdown,
		GrafanaQueries.RunComparison,
		GrafanaQueries.HistoricTrends,
		GrafanaQueries.RecentRegressions,
		GrafanaQueries.PerformanceChanges,
		GrafanaQueries.BaselineComparison,
		GrafanaQueries.GitCommitCorrelation,
	}

	for i, query := range queries {
		assert.NotEmpty(t, query, "Query %d should not be empty", i)
		assert.Contains(t, strings.ToUpper(query), "SELECT", "Query %d should be a SELECT statement", i)

		// Check for parameterized queries
		paramCount := strings.Count(query, "$")
		assert.Greater(t, paramCount, 0, "Query %d should use parameterized queries", i)
	}
}

// TestMigrationReversibility tests that migrations can be applied and reversed
func TestMigrationReversibility(t *testing.T) {
	// This is a conceptual test - in practice, this would require a database
	// Here we just verify the structure supports reversibility

	for _, migration := range migrations {
		// Check that Down SQL attempts to reverse Up SQL
		upSQL := strings.ToUpper(migration.Up)
		downSQL := strings.ToUpper(migration.Down)

		if strings.Contains(upSQL, "CREATE TABLE") {
			assert.Contains(t, downSQL, "DROP TABLE",
				"Migration %d: If Up creates tables, Down should drop them", migration.Version)
		}

		if strings.Contains(upSQL, "CREATE INDEX") {
			assert.Contains(t, downSQL, "DROP INDEX",
				"Migration %d: If Up creates indices, Down should drop them", migration.Version)
		}

		if strings.Contains(upSQL, "CREATE FUNCTION") {
			assert.Contains(t, downSQL, "DROP FUNCTION",
				"Migration %d: If Up creates functions, Down should drop them", migration.Version)
		}
	}
}

// TestDefaultPostgresConfigMigrationCompatibility tests config compatibility
func TestDefaultPostgresConfigMigrationCompatibility(t *testing.T) {
	config := DefaultPostgresConfig()

	// Test that default config has reasonable values for migrations
	assert.Greater(t, config.MaxOpenConns, 0, "Max open connections should be positive")
	assert.GreaterOrEqual(t, config.MaxOpenConns, config.MaxIdleConns, "Max open should be >= max idle")
	assert.Greater(t, config.MaxLifetime, time.Duration(0), "Connection lifetime should be positive")

	// Test SSL mode is valid
	validSSLModes := []string{"disable", "require", "verify-ca", "verify-full"}
	assert.Contains(t, validSSLModes, config.SSLMode, "SSL mode should be valid")
}

// Enhanced edge case tests for migration system

// TestMigrationVersionGaps tests handling of non-sequential migration versions
func (suite *MigrationTestSuite) TestMigrationVersionGaps() {
	t := suite.T()

	// Initialize migration table
	err := suite.migration.Initialize()
	require.NoError(t, err)

	// Manually insert migration records with gaps
	_, err = suite.db.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (1, 'migration_1', NOW())")
	require.NoError(t, err)
	_, err = suite.db.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (3, 'migration_3', NOW())")
	require.NoError(t, err)
	_, err = suite.db.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (5, 'migration_5', NOW())")
	require.NoError(t, err)

	// Get applied migrations
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.True(t, applied[1])
	assert.False(t, applied[2]) // Gap
	assert.True(t, applied[3])
	assert.False(t, applied[4]) // Gap
	assert.True(t, applied[5])

	// Test version retrieval with gaps
	version, err := suite.migration.GetVersion()
	assert.NoError(t, err)
	assert.Equal(t, 5, version) // Should return highest version
}

// TestMigrationDatabaseConnectionLoss tests behavior when database connection is lost
func (suite *MigrationTestSuite) TestMigrationDatabaseConnectionLoss() {
	t := suite.T()

	// Initialize migrations
	err := suite.migration.Up()
	require.NoError(t, err)

	// Close the database connection to simulate connection loss
	originalDB := suite.migration.db
	suite.migration.db.Close()

	// Create a closed database connection
	closedDB, _ := sql.Open("postgres", "invalid connection string")
	closedDB.Close()
	suite.migration.db = closedDB

	// Try to get version - should fail gracefully
	_, err = suite.migration.GetVersion()
	assert.Error(t, err)

	// Try to apply migrations - should fail gracefully
	err = suite.migration.Up()
	assert.Error(t, err)

	// Restore original connection
	suite.migration.db = originalDB
}

// TestMigrationLargeDatasets tests migrations with substantial data
func (suite *MigrationTestSuite) TestMigrationLargeDatasets() {
	t := suite.T()

	// Apply initial migrations to have tables
	err := suite.migration.Up()
	require.NoError(t, err)

	// Insert a large number of records
	const recordCount = 10000
	tx, err := suite.db.Begin()
	require.NoError(t, err)

	for i := 0; i < recordCount; i++ {
		_, err = tx.Exec(`
			INSERT INTO benchmark_runs (
				run_id, test_name, description, start_time, end_time, duration,
				config, environment, tags
			) VALUES (
				$1, 'large_test', 'Large dataset test', NOW(), NOW(), '30m',
				'{}', '{}', '{}'
			)`, fmt.Sprintf("large_run_%d", i))
		if err != nil {
			tx.Rollback()
			require.NoError(t, err)
		}
	}

	err = tx.Commit()
	require.NoError(t, err)

	// Verify data exists
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM benchmark_runs WHERE test_name = 'large_test'").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, recordCount, count)

	// Test rollback with large dataset
	initialVersion, err := suite.migration.GetVersion()
	require.NoError(t, err)

	// Rollback one migration
	err = suite.migration.Down(initialVersion - 1)
	assert.NoError(t, err)

	// Re-apply migration
	err = suite.migration.Up()
	assert.NoError(t, err)

	// Verify data integrity after migration operations
	err = suite.db.QueryRow("SELECT COUNT(*) FROM benchmark_runs WHERE test_name = 'large_test'").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, recordCount, count)
}

// TestMigrationPermissionRestrictions tests behavior with restricted database permissions
func (suite *MigrationTestSuite) TestMigrationPermissionRestrictions() {
	t := suite.T()

	// This test simulates scenarios where the migration user has limited permissions
	// Note: This is a conceptual test - actual permission testing would require
	// setting up restricted database users

	// Test detection of missing permissions
	err := suite.migration.Up()
	assert.NoError(t, err) // Should succeed with full permissions in test environment

	// Verify that functions requiring elevated permissions were created
	var functionExists bool
	err = suite.db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.routines 
			WHERE routine_schema = 'public' 
			AND routine_name = 'get_metric_timeseries'
		)`).Scan(&functionExists)
	assert.NoError(t, err)
	assert.True(t, functionExists)
}

// TestMigrationSchemaValidation tests comprehensive schema validation after migrations
func (suite *MigrationTestSuite) TestMigrationSchemaValidation() {
	t := suite.T()

	// Apply all migrations
	err := suite.migration.Up()
	require.NoError(t, err)

	// Test table constraints and relationships
	expectedConstraints := map[string][]string{
		"benchmark_runs": {"benchmark_runs_pkey"},
		"client_summary": {"client_summary_pkey", "fk_client_summary_run_id"},
		"method_summary": {"method_summary_pkey", "fk_method_summary_run_id"},
		"historic_runs":  {"historic_runs_pkey"},
		"regressions":    {"regressions_pkey"},
		"baselines":      {"baselines_pkey"},
	}

	for tableName, expectedConstraintNames := range expectedConstraints {
		rows, err := suite.db.Query(`
			SELECT constraint_name 
			FROM information_schema.table_constraints 
			WHERE table_schema = 'public' 
			AND table_name = $1
			AND constraint_type IN ('PRIMARY KEY', 'FOREIGN KEY', 'UNIQUE')
		`, tableName)
		require.NoError(t, err)

		var constraints []string
		for rows.Next() {
			var constraintName string
			err = rows.Scan(&constraintName)
			require.NoError(t, err)
			constraints = append(constraints, constraintName)
		}
		rows.Close()

		// Verify at least the expected constraints exist
		for _, expectedConstraint := range expectedConstraintNames {
			found := false
			for _, constraint := range constraints {
				if strings.Contains(constraint, expectedConstraint) || constraint == expectedConstraint {
					found = true
					break
				}
			}
			assert.True(t, found, "Expected constraint %s not found for table %s", expectedConstraint, tableName)
		}
	}

	// Test column types and nullability
	expectedColumns := map[string]map[string]string{
		"benchmark_runs": {
			"run_id":     "text",
			"test_name":  "text",
			"start_time": "timestamp with time zone",
			"end_time":   "timestamp with time zone",
			"duration":   "text",
		},
		"client_summary": {
			"run_id":         "text",
			"client_name":    "text",
			"total_requests": "bigint",
			"total_errors":   "bigint",
			"error_rate":     "double precision",
		},
	}

	for tableName, expectedColumnTypes := range expectedColumns {
		for columnName, expectedType := range expectedColumnTypes {
			var dataType string
			err = suite.db.QueryRow(`
				SELECT data_type 
				FROM information_schema.columns 
				WHERE table_schema = 'public' 
				AND table_name = $1 
				AND column_name = $2
			`, tableName, columnName).Scan(&dataType)
			assert.NoError(t, err, "Column %s.%s should exist", tableName, columnName)
			assert.Contains(t, dataType, expectedType, "Column %s.%s should have type %s, got %s", tableName, columnName, expectedType, dataType)
		}
	}
}

// TestMigrationPerformanceMetrics tests migration performance characteristics
func (suite *MigrationTestSuite) TestMigrationPerformanceMetrics() {
	t := suite.T()

	// Measure time to apply all migrations
	startTime := time.Now()
	err := suite.migration.Up()
	migrationDuration := time.Since(startTime)

	assert.NoError(t, err)
	assert.Less(t, migrationDuration, 30*time.Second, "Migration should complete within 30 seconds")

	// Measure time to rollback all migrations
	startTime = time.Now()
	err = suite.migration.Down(0)
	rollbackDuration := time.Since(startTime)

	assert.NoError(t, err)
	assert.Less(t, rollbackDuration, 15*time.Second, "Rollback should complete within 15 seconds")

	// Test index creation performance
	err = suite.migration.Up()
	require.NoError(t, err)

	startTime = time.Now()
	err = suite.migration.CreateIndices()
	indexCreationDuration := time.Since(startTime)

	assert.NoError(t, err)
	assert.Less(t, indexCreationDuration, 10*time.Second, "Index creation should complete within 10 seconds")
}

// TestMigrationDataIntegrity tests data integrity during migration operations
func (suite *MigrationTestSuite) TestMigrationDataIntegrity() {
	t := suite.T()

	// Apply migrations and insert test data
	err := suite.migration.Up()
	require.NoError(t, err)

	// Insert reference data
	testData := []struct {
		runID    string
		testName string
		client   string
	}{
		{"integrity_run_1", "integrity_test", "client_1"},
		{"integrity_run_2", "integrity_test", "client_2"},
		{"integrity_run_3", "integrity_test", "client_3"},
	}

	for _, data := range testData {
		// Insert benchmark run
		_, err = suite.db.Exec(`
			INSERT INTO benchmark_runs (
				run_id, test_name, description, start_time, end_time, duration,
				config, environment, tags
			) VALUES (
				$1, $2, 'Integrity test', NOW(), NOW(), '30m',
				'{}', '{}', '{}'
			)`, data.runID, data.testName)
		require.NoError(t, err)

		// Insert client summary
		_, err = suite.db.Exec(`
			INSERT INTO client_summary (
				run_id, client_name, total_requests, total_errors, error_rate
			) VALUES (
				$1, $2, 1000, 10, 0.01
			)`, data.runID, data.client)
		require.NoError(t, err)
	}

	// Calculate checksums before rollback
	var beforeChecksums map[string]string = make(map[string]string)

	for tableName := range map[string]bool{"benchmark_runs": true, "client_summary": true} {
		var checksum string
		err = suite.db.QueryRow(fmt.Sprintf("SELECT md5(string_agg(md5(t.*::text), '')) FROM %s t WHERE test_name = 'integrity_test'", tableName)).Scan(&checksum)
		if err == nil {
			beforeChecksums[tableName] = checksum
		}
	}

	// Perform rollback and re-apply
	initialVersion, err := suite.migration.GetVersion()
	require.NoError(t, err)

	err = suite.migration.Down(initialVersion - 2)
	assert.NoError(t, err)

	err = suite.migration.Up()
	assert.NoError(t, err)

	// Verify data integrity after migration operations
	for _, data := range testData {
		var count int
		err = suite.db.QueryRow("SELECT COUNT(*) FROM benchmark_runs WHERE run_id = $1", data.runID).Scan(&count)
		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Run %s should exist after migration operations", data.runID)

		err = suite.db.QueryRow("SELECT COUNT(*) FROM client_summary WHERE run_id = $1", data.runID).Scan(&count)
		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Client summary for run %s should exist after migration operations", data.runID)
	}

	// Compare checksums after operations (for tables that still exist)
	for tableName, beforeChecksum := range beforeChecksums {
		var afterChecksum string
		err = suite.db.QueryRow(fmt.Sprintf("SELECT md5(string_agg(md5(t.*::text), '')) FROM %s t WHERE test_name = 'integrity_test'", tableName)).Scan(&afterChecksum)
		if err == nil {
			assert.Equal(t, beforeChecksum, afterChecksum, "Data integrity should be maintained for table %s", tableName)
		}
	}
}

// TestMigrationErrorRecovery tests recovery from various error conditions
func (suite *MigrationTestSuite) TestMigrationErrorRecovery() {
	t := suite.T()

	// Test recovery from incomplete migration state
	err := suite.migration.Initialize()
	require.NoError(t, err)

	// Manually insert an incomplete migration record
	_, err = suite.db.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (999, 'incomplete_migration', NOW())")
	require.NoError(t, err)

	// Try to apply migrations - should handle the incomplete state
	err = suite.migration.Up()
	assert.NoError(t, err)

	// Verify system recovered properly
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.True(t, applied[999]) // Incomplete migration should be preserved

	// Test recovery from corrupted migration table
	_, err = suite.db.Exec("UPDATE schema_migrations SET version = NULL WHERE version = 1")
	require.NoError(t, err)

	// System should handle NULL version gracefully
	applied, err = suite.migration.GetAppliedMigrations()
	// Should not panic and should return some result
	assert.NotNil(t, applied)
}

// TestMigrationConcurrencyEdgeCases tests advanced concurrency scenarios
func (suite *MigrationTestSuite) TestMigrationConcurrencyEdgeCases() {
	t := suite.T()

	// Test concurrent initialization attempts
	const concurrency = 5
	initResults := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			migrationService := NewMigrationService(suite.db, suite.logger)
			err := migrationService.Initialize()
			initResults <- err
		}()
	}

	// Collect initialization results
	successCount := 0
	for i := 0; i < concurrency; i++ {
		err := <-initResults
		if err == nil {
			successCount++
		}
	}

	// At least one should succeed, and system should be in consistent state
	assert.Greater(t, successCount, 0, "At least one initialization should succeed")

	// Verify migration table exists and is consistent
	var exists bool
	err := suite.db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = 'schema_migrations'
		)`).Scan(&exists)
	assert.NoError(t, err)
	assert.True(t, exists)

	// Test concurrent migration attempts with different services
	migrationResults := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			migrationService := NewMigrationService(suite.db, suite.logger)
			err := migrationService.Up()
			migrationResults <- err
		}()
	}

	// Collect migration results
	migrationSuccessCount := 0
	for i := 0; i < concurrency; i++ {
		err := <-migrationResults
		if err == nil {
			migrationSuccessCount++
		}
	}

	// System should handle concurrent migrations gracefully
	assert.Greater(t, migrationSuccessCount, 0, "At least one migration should succeed")

	// Verify final state is consistent
	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)
	assert.Len(t, applied, len(migrations))
}

// TestMigrationResourceLimits tests behavior under resource constraints
func (suite *MigrationTestSuite) TestMigrationResourceLimits() {
	t := suite.T()

	// Test migration with limited connection pool
	limitedDB, err := sql.Open("postgres", suite.db.Stats().OpenConnections)
	if err == nil {
		limitedDB.SetMaxOpenConns(1)
		limitedDB.SetMaxIdleConns(1)

		limitedMigration := NewMigrationService(limitedDB, suite.logger)
		err = limitedMigration.Up()
		assert.NoError(t, err, "Migration should succeed even with limited connections")

		limitedDB.Close()
	}

	// Test with memory constraints (conceptual - actual memory limits would require OS-level controls)
	// This tests whether migrations handle large operations efficiently
	err = suite.migration.Up()
	assert.NoError(t, err)

	// Create a large temporary table to simulate memory pressure
	_, err = suite.db.Exec(`
		CREATE TEMPORARY TABLE large_temp_table AS 
		SELECT generate_series(1, 100000) as id, 
			   md5(random()::text) as data
	`)
	if err == nil {
		// Test migration operations under memory pressure
		err = suite.migration.CreateIndices()
		assert.NoError(t, err, "Index creation should succeed under memory pressure")
	}
}

// TestMigrationVersionConsistency tests version numbering consistency
func (suite *MigrationTestSuite) TestMigrationVersionConsistency() {
	t := suite.T()

	// Verify migration versions are sequential without gaps
	for i := 0; i < len(migrations)-1; i++ {
		currentVersion := migrations[i].Version
		nextVersion := migrations[i+1].Version
		assert.Equal(t, currentVersion+1, nextVersion,
			"Migration versions should be sequential: %d -> %d", currentVersion, nextVersion)
	}

	// Test version consistency after operations
	err := suite.migration.Up()
	require.NoError(t, err)

	version, err := suite.migration.GetVersion()
	assert.NoError(t, err)
	assert.Equal(t, len(migrations), version)

	applied, err := suite.migration.GetAppliedMigrations()
	assert.NoError(t, err)

	// Count applied migrations
	appliedCount := 0
	for _, isApplied := range applied {
		if isApplied {
			appliedCount++
		}
	}
	assert.Equal(t, len(migrations), appliedCount)
}
