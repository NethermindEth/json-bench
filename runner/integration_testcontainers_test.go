package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/storage"
)

// TestContainersIntegrationSuite provides comprehensive integration tests using testcontainers
type TestContainersIntegrationSuite struct {
	IntegrationTestSuite

	// Testcontainers
	postgresContainer *postgres.PostgresContainer
	containerCtx      context.Context
}

// SetupSuite initializes the test environment with real containers
func (suite *TestContainersIntegrationSuite) SetupSuite() {
	suite.containerCtx = context.Background()

	// Check if Docker is available
	if !suite.isDockerAvailable() {
		suite.T().Skip("Docker not available for testcontainers")
		return
	}

	// Start PostgreSQL container
	suite.setupPostgreSQLContainer()

	// Call parent setup with our container database
	suite.IntegrationTestSuite.SetupSuite()
}

// TearDownSuite cleans up containers and parent resources
func (suite *TestContainersIntegrationSuite) TearDownSuite() {
	// Call parent teardown first
	suite.IntegrationTestSuite.TearDownSuite()

	// Terminate containers
	if suite.postgresContainer != nil {
		if err := suite.postgresContainer.Terminate(suite.containerCtx); err != nil {
			suite.logger.WithError(err).Error("Failed to terminate PostgreSQL container")
		}
	}
}

// isDockerAvailable checks if Docker is available
func (suite *TestContainersIntegrationSuite) isDockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to create a simple container to test Docker availability
	req := testcontainers.ContainerRequest{
		Image:        "hello-world",
		ExposedPorts: []string{},
		WaitingFor:   wait.ForExit(),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})

	if err != nil {
		return false
	}

	defer container.Terminate(ctx)
	return true
}

// setupPostgreSQLContainer starts a PostgreSQL container for testing
func (suite *TestContainersIntegrationSuite) setupPostgreSQLContainer() {
	ctx, cancel := context.WithTimeout(suite.containerCtx, 120*time.Second)
	defer cancel()

	// Start PostgreSQL container
	postgresContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)

	require.NoError(suite.T(), err)
	suite.postgresContainer = postgresContainer

	// Get connection details
	host, err := postgresContainer.Host(ctx)
	require.NoError(suite.T(), err)

	port, err := postgresContainer.MappedPort(ctx, "5432")
	require.NoError(suite.T(), err)

	// Update storage config to use container
	suite.storageConfig = &config.StorageConfig{
		HistoricPath:   suite.testDir + "/historic",
		RetentionDays:  30,
		EnableHistoric: true,
		PostgreSQL: &config.PostgreSQLConfig{
			Host:                  host,
			Port:                  port.Int(),
			Database:              "testdb",
			Username:              "testuser",
			Password:              "testpass",
			SSLMode:               "disable",
			MaxConnections:        10,
			MaxIdleConnections:    2,
			ConnectionMaxLifetime: 30 * time.Minute,
			ConnectionTimeout:     10 * time.Second,
			Schema:                "public",
		},
	}

	// Open database connection
	suite.db, err = sql.Open("postgres", suite.storageConfig.PostgreSQL.GetConnectionString())
	require.NoError(suite.T(), err)

	// Test connection
	err = suite.db.Ping()
	require.NoError(suite.T(), err)

	suite.logger.WithFields(map[string]interface{}{
		"host": host,
		"port": port.Int(),
	}).Info("PostgreSQL container started successfully")
}

// TestCompleteSystemLifecycle tests the entire system lifecycle with real containers
func (suite *TestContainersIntegrationSuite) TestCompleteSystemLifecycle() {
	suite.logger.Info("Testing complete system lifecycle with testcontainers")

	// Run all test scenarios in sequence
	suite.Run("Scenario1_FreshSystemSetup", func() {
		suite.TestScenario1_FreshSystemSetup()
	})

	suite.Run("Scenario2_TrendAnalysisAndRegressionDetection", func() {
		suite.TestScenario2_TrendAnalysisAndRegressionDetection()
	})

	suite.Run("Scenario3_BaselineManagement", func() {
		suite.TestScenario3_BaselineManagement()
	})

	suite.Run("Scenario4_WebSocketNotifications", func() {
		suite.TestScenario4_WebSocketNotifications()
	})

	suite.Run("Scenario5_GrafanaDashboardQueries", func() {
		suite.TestScenario5_GrafanaDashboardQueries()
	})

	suite.Run("Scenario6_LargeDatasetPerformance", func() {
		suite.TestScenario6_LargeDatasetPerformance()
	})

	suite.Run("Scenario7_SystemRecovery", func() {
		suite.TestScenario7_SystemRecovery()
	})

	suite.Run("ConcurrentAccess", func() {
		suite.TestConcurrentAccess()
	})

	suite.Run("WebSocketConnectionLimits", func() {
		suite.TestWebSocketConnectionLimits()
	})

	suite.logger.Info("Complete system lifecycle test completed successfully")
}

// TestDatabaseMigrations tests database migrations with a real PostgreSQL instance
func (suite *TestContainersIntegrationSuite) TestDatabaseMigrations() {
	suite.logger.Info("Testing database migrations")

	// Get current schema version
	initialVersion, err := suite.migrationService.GetVersion()
	require.NoError(suite.T(), err)
	assert.True(suite.T(), initialVersion > 0, "Should have migrations applied")

	// Test rollback and reapply
	targetVersion := initialVersion - 1
	if targetVersion >= 0 {
		err = suite.migrationService.Down(targetVersion)
		require.NoError(suite.T(), err)

		currentVersion, err := suite.migrationService.GetVersion()
		require.NoError(suite.T(), err)
		assert.Equal(suite.T(), targetVersion, currentVersion)

		// Reapply migrations
		err = suite.migrationService.Up()
		require.NoError(suite.T(), err)

		finalVersion, err := suite.migrationService.GetVersion()
		require.NoError(suite.T(), err)
		assert.Equal(suite.T(), initialVersion, finalVersion)
	}

	suite.logger.Info("Database migrations test completed successfully")
}

// TestPostgreSQLSpecificFeatures tests PostgreSQL-specific features
func (suite *TestContainersIntegrationSuite) TestPostgreSQLSpecificFeatures() {
	suite.logger.Info("Testing PostgreSQL-specific features")

	// Test JSONB operations
	query := `
		SELECT COUNT(*) FROM historic_runs 
		WHERE config ? 'test_name' 
		AND config->>'test_name' LIKE 'test%'
	`

	var count int
	err := suite.db.QueryRow(query).Scan(&count)
	require.NoError(suite.T(), err)

	// Test full-text search capabilities
	query = `
		SELECT COUNT(*) FROM historic_runs 
		WHERE to_tsvector('english', description) @@ to_tsquery('english', 'test')
	`

	err = suite.db.QueryRow(query).Scan(&count)
	require.NoError(suite.T(), err)

	// Test array operations
	query = `
		SELECT COUNT(*) FROM historic_runs 
		WHERE tags && ARRAY['performance', 'benchmark']
	`

	err = suite.db.QueryRow(query).Scan(&count)
	require.NoError(suite.T(), err)

	// Test window functions for trend analysis
	query = `
		SELECT 
			test_name,
			timestamp,
			avg_latency_ms,
			LAG(avg_latency_ms) OVER (PARTITION BY test_name ORDER BY timestamp) as prev_latency,
			avg_latency_ms - LAG(avg_latency_ms) OVER (PARTITION BY test_name ORDER BY timestamp) as latency_diff
		FROM historic_runs 
		WHERE test_name = $1
		ORDER BY timestamp
		LIMIT 5
	`

	if len(suite.testRuns) > 0 {
		rows, err := suite.db.Query(query, suite.testRuns[0].TestName)
		require.NoError(suite.T(), err)

		var results []struct {
			TestName    string
			Timestamp   time.Time
			AvgLatency  float64
			PrevLatency *float64
			LatencyDiff *float64
		}

		for rows.Next() {
			var r struct {
				TestName    string
				Timestamp   time.Time
				AvgLatency  float64
				PrevLatency *float64
				LatencyDiff *float64
			}

			err := rows.Scan(&r.TestName, &r.Timestamp, &r.AvgLatency, &r.PrevLatency, &r.LatencyDiff)
			require.NoError(suite.T(), err)
			results = append(results, r)
		}
		rows.Close()

		assert.True(suite.T(), len(results) > 0, "Should return window function results")
	}

	suite.logger.Info("PostgreSQL-specific features test completed successfully")
}

// TestDatabasePerformanceUnderLoad tests database performance with concurrent load
func (suite *TestContainersIntegrationSuite) TestDatabasePerformanceUnderLoad() {
	suite.logger.Info("Testing database performance under load")

	// Test concurrent writes
	numWorkers := 20
	operationsPerWorker := 50
	testName := "load-test"

	results := make(chan error, numWorkers*operationsPerWorker)
	startTime := time.Now()

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			for j := 0; j < operationsPerWorker; j++ {
				result := suite.generateBenchmarkResult(
					fmt.Sprintf("%s-worker-%d", testName, workerID),
					j+1,
				)

				_, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
				results <- err
			}
		}(i)
	}

	// Collect results
	var errors []error
	for i := 0; i < numWorkers*operationsPerWorker; i++ {
		if err := <-results; err != nil {
			errors = append(errors, err)
		}
	}

	duration := time.Since(startTime)

	suite.logger.WithFields(map[string]interface{}{
		"workers":               numWorkers,
		"operations_per_worker": operationsPerWorker,
		"total_operations":      numWorkers * operationsPerWorker,
		"duration":              duration,
		"ops_per_second":        float64(numWorkers*operationsPerWorker) / duration.Seconds(),
		"error_count":           len(errors),
	}).Info("Database load test completed")

	// Performance assertions
	assert.Less(suite.T(), len(errors), numWorkers*operationsPerWorker/10, "Error rate should be less than 10%")
	assert.Less(suite.T(), duration.Seconds(), 60.0, "Load test should complete within 60 seconds")

	// Test concurrent reads during writes
	go func() {
		for i := 0; i < 100; i++ {
			_, err := suite.historicStorage.ListHistoricRuns(suite.ctx, testName, 10)
			if err != nil {
				suite.logger.WithError(err).Warn("Read operation failed during load test")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	time.Sleep(2 * time.Second) // Let concurrent reads run

	suite.logger.Info("Database performance under load test completed successfully")
}

// TestContainerResourceLimits tests behavior under resource constraints
func (suite *TestContainersIntegrationSuite) TestContainerResourceLimits() {
	suite.logger.Info("Testing container resource limits")

	// Test behavior with limited connections
	originalMaxConnections := suite.storageConfig.PostgreSQL.MaxConnections
	suite.storageConfig.PostgreSQL.MaxConnections = 5

	// Create multiple connections to test pool limits
	var connections []*sql.DB
	for i := 0; i < 10; i++ {
		db, err := sql.Open("postgres", suite.storageConfig.PostgreSQL.GetConnectionString())
		if err != nil {
			break
		}

		db.SetMaxOpenConns(1)
		connections = append(connections, db)

		// Test connection
		ctx, cancel := context.WithTimeout(suite.ctx, 5*time.Second)
		if err := db.PingContext(ctx); err != nil {
			cancel()
			db.Close()
			break
		}
		cancel()
	}

	suite.logger.WithField("connections_created", len(connections)).Info("Created database connections")

	// Test operations with limited connections
	for i := 0; i < 5; i++ {
		result := suite.generateBenchmarkResult("resource-limit-test", i+1)
		_, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
		assert.NoError(suite.T(), err, "Operations should work within connection limits")
	}

	// Cleanup connections
	for _, db := range connections {
		db.Close()
	}

	// Restore original settings
	suite.storageConfig.PostgreSQL.MaxConnections = originalMaxConnections

	suite.logger.Info("Container resource limits test completed successfully")
}

// TestDataIntegrityAndConsistency tests data integrity across operations
func (suite *TestContainersIntegrationSuite) TestDataIntegrityAndConsistency() {
	suite.logger.Info("Testing data integrity and consistency")

	testName := "integrity-test"

	// Create multiple related runs
	var runIDs []string
	for i := 0; i < 5; i++ {
		result := suite.generateBenchmarkResult(testName, i+1)
		savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
		require.NoError(suite.T(), err)
		runIDs = append(runIDs, savedRun.ID)
	}

	// Create baseline from first run
	baseline, err := suite.baselineManager.SetBaseline(
		suite.ctx,
		runIDs[0],
		"integrity-baseline",
		"Baseline for integrity testing",
	)
	require.NoError(suite.T(), err)

	// Test referential integrity - try to delete run that's used as baseline
	err = suite.historicStorage.DeleteHistoricRun(suite.ctx, runIDs[0])
	// This should either fail or cascade properly
	if err == nil {
		// If deletion succeeded, baseline should be cleaned up
		_, err = suite.baselineManager.GetBaseline(suite.ctx, baseline.Name)
		assert.Error(suite.T(), err, "Baseline should be removed when referenced run is deleted")
	}

	// Test transaction consistency
	tx, err := suite.db.Begin()
	require.NoError(suite.T(), err)

	// Insert partial data and rollback
	_, err = tx.Exec(`
		INSERT INTO historic_runs (id, test_name, timestamp, avg_latency_ms) 
		VALUES ($1, $2, $3, $4)
	`, "partial-test", testName, time.Now(), 100.0)
	require.NoError(suite.T(), err)

	// Rollback transaction
	err = tx.Rollback()
	require.NoError(suite.T(), err)

	// Verify data was not committed
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM historic_runs WHERE id = $1", "partial-test").Scan(&count)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 0, count, "Rolled back data should not exist")

	suite.logger.Info("Data integrity and consistency test completed successfully")
}

// TestBackupAndRestore tests backup and restore capabilities
func (suite *TestContainersIntegrationSuite) TestBackupAndRestore() {
	suite.logger.Info("Testing backup and restore capabilities")

	// Create some test data
	testName := "backup-test"
	var originalRunIDs []string

	for i := 0; i < 3; i++ {
		result := suite.generateBenchmarkResult(testName, i+1)
		savedRun, err := suite.historicStorage.SaveHistoricRun(suite.ctx, result)
		require.NoError(suite.T(), err)
		originalRunIDs = append(originalRunIDs, savedRun.ID)
	}

	// Get count of original data
	var originalCount int
	err := suite.db.QueryRow("SELECT COUNT(*) FROM historic_runs WHERE test_name = $1", testName).Scan(&originalCount)
	require.NoError(suite.T(), err)

	// Simulate backup by dumping specific data
	backupQuery := `
		SELECT id, test_name, description, timestamp, avg_latency_ms 
		FROM historic_runs 
		WHERE test_name = $1
	`

	rows, err := suite.db.Query(backupQuery, testName)
	require.NoError(suite.T(), err)
	defer rows.Close()

	type backupRow struct {
		ID          string
		TestName    string
		Description string
		Timestamp   time.Time
		AvgLatency  float64
	}

	var backupData []backupRow
	for rows.Next() {
		var row backupRow
		err := rows.Scan(&row.ID, &row.TestName, &row.Description, &row.Timestamp, &row.AvgLatency)
		require.NoError(suite.T(), err)
		backupData = append(backupData, row)
	}

	assert.Equal(suite.T(), originalCount, len(backupData), "Backup should contain all original data")

	// Delete original data
	_, err = suite.db.Exec("DELETE FROM historic_runs WHERE test_name = $1", testName)
	require.NoError(suite.T(), err)

	// Verify deletion
	var afterDeleteCount int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM historic_runs WHERE test_name = $1", testName).Scan(&afterDeleteCount)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 0, afterDeleteCount, "Data should be deleted")

	// Restore from backup (simplified)
	for _, row := range backupData {
		_, err = suite.db.Exec(`
			INSERT INTO historic_runs (id, test_name, description, timestamp, avg_latency_ms, 
			                          total_requests, total_errors, overall_error_rate, 
			                          p95_latency_ms, p99_latency_ms, max_latency_ms, best_client,
			                          config, performance_scores, full_results, environment) 
			VALUES ($1, $2, $3, $4, $5, 0, 0, 0, 0, 0, 0, '', '{}', '{}', '{}', '{}')
		`, row.ID, row.TestName, row.Description, row.Timestamp, row.AvgLatency)
		require.NoError(suite.T(), err)
	}

	// Verify restore
	var afterRestoreCount int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM historic_runs WHERE test_name = $1", testName).Scan(&afterRestoreCount)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), originalCount, afterRestoreCount, "Restored data should match original count")

	suite.logger.Info("Backup and restore test completed successfully")
}

// Run the testcontainers integration test suite
func TestTestContainersIntegrationSuite(t *testing.T) {
	// Skip if not enabled
	if testing.Short() {
		t.Skip("Skipping testcontainers integration tests in short mode")
	}

	if os.Getenv("TESTCONTAINERS_TESTS") != "1" {
		t.Skip("Testcontainers integration tests not enabled. Set TESTCONTAINERS_TESTS=1 to run.")
	}

	suite.Run(t, new(TestContainersIntegrationSuite))
}
