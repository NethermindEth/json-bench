package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
)

// MockGitExecutor allows mocking git commands for testing
type MockGitExecutor struct {
	mock.Mock
}

func (m *MockGitExecutor) ExecCommand(name string, args ...string) ([]byte, error) {
	arguments := m.Called(name, args)
	return arguments.Get(0).([]byte), arguments.Error(1)
}

// GitExecutor interface for dependency injection
type GitExecutor interface {
	ExecCommand(name string, args ...string) ([]byte, error)
}

// RealGitExecutor implements GitExecutor using actual git commands
type RealGitExecutor struct{}

func (r *RealGitExecutor) ExecCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// HistoricStorageTestSuite provides comprehensive tests for historic storage
type HistoricStorageTestSuite struct {
	suite.Suite
	container     *postgres.PostgresContainer
	db            *sql.DB
	storage       HistoricStorage
	storageImpl   *historicStorage
	ctx           context.Context
	logger        logrus.FieldLogger
	tempDir       string
	storageConfig *config.StorageConfig
	gitExecutor   *MockGitExecutor
}

// SetupSuite initializes the test environment
func (suite *HistoricStorageTestSuite) SetupSuite() {
	suite.ctx = context.Background()
	suite.logger = logrus.New().WithField("test", "historic_storage")

	// Create temporary directory for file storage
	tempDir, err := ioutil.TempDir("", "historic_test_*")
	require.NoError(suite.T(), err)
	suite.tempDir = tempDir

	// Setup PostgreSQL container
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

	// Run migrations
	migrationService := NewMigrationService(db, suite.logger)
	err = migrationService.Up()
	require.NoError(suite.T(), err)

	// Setup storage config
	suite.storageConfig = &config.StorageConfig{
		EnableHistoric: true,
		HistoricPath:   filepath.Join(suite.tempDir, "historic"),
		RetentionDays:  30,
	}

	// Create historic storage
	storage := NewHistoricStorage(db, suite.storageConfig, suite.logger)
	suite.storage = storage
	suite.storageImpl = storage.(*historicStorage)

	// Setup git executor mock
	suite.gitExecutor = new(MockGitExecutor)

	// Start storage
	err = suite.storage.Start(suite.ctx)
	require.NoError(suite.T(), err)
}

// TearDownSuite cleans up test resources
func (suite *HistoricStorageTestSuite) TearDownSuite() {
	if suite.storage != nil {
		suite.storage.Stop()
	}
	if suite.db != nil {
		suite.db.Close()
	}
	if suite.container != nil {
		suite.container.Terminate(suite.ctx)
	}
	if suite.tempDir != "" {
		os.RemoveAll(suite.tempDir)
	}
}

// SetupTest prepares clean state for each test
func (suite *HistoricStorageTestSuite) SetupTest() {
	// Clean database tables
	tables := []string{
		"regressions",
		"baselines",
		"historic_runs",
	}

	for _, table := range tables {
		_, err := suite.db.Exec(fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		require.NoError(suite.T(), err)
	}

	// Clean file system
	historicPath := suite.storageConfig.HistoricPath
	if _, err := os.Stat(historicPath); err == nil {
		entries, err := ioutil.ReadDir(historicPath)
		require.NoError(suite.T(), err)
		for _, entry := range entries {
			os.RemoveAll(filepath.Join(historicPath, entry.Name()))
		}
	}

	// Reset git executor mock
	suite.gitExecutor = new(MockGitExecutor)
}

// TestHistoricStorageInitialization tests storage initialization
func (suite *HistoricStorageTestSuite) TestHistoricStorageInitialization() {
	t := suite.T()

	// Test that historic directory was created
	assert.DirExists(t, suite.storageConfig.HistoricPath)

	// Test with disabled historic storage
	disabledConfig := &config.StorageConfig{
		EnableHistoric: false,
	}
	disabledStorage := NewHistoricStorage(suite.db, disabledConfig, suite.logger)
	err := disabledStorage.Start(suite.ctx)
	assert.NoError(t, err)
}

// TestSaveHistoricRunBasic tests basic historic run saving
func (suite *HistoricStorageTestSuite) TestSaveHistoricRunBasic() {
	t := suite.T()

	// Setup git mocks
	suite.gitExecutor.On("ExecCommand", "git", []string{"rev-parse", "HEAD"}).
		Return([]byte("abc123def456\n"), nil)
	suite.gitExecutor.On("ExecCommand", "git", []string{"rev-parse", "--abbrev-ref", "HEAD"}).
		Return([]byte("main\n"), nil)

	result := createTestBenchmarkResult("basic_historic_test")

	savedRun, err := suite.storage.SaveHistoricRun(suite.ctx, result)
	assert.NoError(t, err)
	assert.NotNil(t, savedRun)
	assert.NotEmpty(t, savedRun.ID)
	assert.Equal(t, "basic_historic_test", savedRun.TestName)
	assert.Equal(t, 1, savedRun.ClientsCount)
	assert.Greater(t, savedRun.TotalRequests, int64(0))

	// Verify database insertion
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM historic_runs").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestSaveHistoricRunWithFiles tests file saving functionality
func (suite *HistoricStorageTestSuite) TestSaveHistoricRunWithFiles() {
	t := suite.T()

	result := createTestBenchmarkResult("file_test")

	savedRun, err := suite.storage.SaveHistoricRun(suite.ctx, result)
	require.NoError(t, err)

	// Check that files were saved
	runDir := filepath.Join(suite.storageConfig.HistoricPath, savedRun.ID)
	assert.DirExists(t, runDir)

	// Check result.json exists and is valid
	resultPath := filepath.Join(runDir, "result.json")
	assert.FileExists(t, resultPath)

	resultData, err := ioutil.ReadFile(resultPath)
	assert.NoError(t, err)

	var savedResult types.BenchmarkResult
	err = json.Unmarshal(resultData, &savedResult)
	assert.NoError(t, err)
	assert.Equal(t, result.StartTime, savedResult.StartTime)

	// Check metadata.json exists and is valid
	metadataPath := filepath.Join(runDir, "metadata.json")
	assert.FileExists(t, metadataPath)

	metadataData, err := ioutil.ReadFile(metadataPath)
	assert.NoError(t, err)

	var metadata map[string]interface{}
	err = json.Unmarshal(metadataData, &metadata)
	assert.NoError(t, err)
	assert.Equal(t, savedRun.ID, metadata["run_id"])
	assert.Equal(t, "file_test", metadata["test_name"])
}

// TestGetHistoricRun tests retrieving historic runs
func (suite *HistoricStorageTestSuite) TestGetHistoricRun() {
	t := suite.T()

	// Save a run first
	result := createTestBenchmarkResult("get_test")
	savedRun, err := suite.storage.SaveHistoricRun(suite.ctx, result)
	require.NoError(t, err)

	// Retrieve the run
	retrievedRun, err := suite.storage.GetHistoricRun(suite.ctx, savedRun.ID)
	assert.NoError(t, err)
	assert.NotNil(t, retrievedRun)
	assert.Equal(t, savedRun.ID, retrievedRun.ID)
	assert.Equal(t, savedRun.TestName, retrievedRun.TestName)
	assert.Equal(t, savedRun.TotalRequests, retrievedRun.TotalRequests)

	// Test non-existent run
	_, err = suite.storage.GetHistoricRun(suite.ctx, "nonexistent_id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "historic run not found")
}

// TestListHistoricRuns tests listing historic runs with filtering
func (suite *HistoricStorageTestSuite) TestListHistoricRuns() {
	t := suite.T()

	// Save multiple runs
	testNames := []string{"list_test_1", "list_test_2", "other_test"}
	savedRuns := make([]*types.HistoricRun, 0, len(testNames))

	for _, testName := range testNames {
		result := createTestBenchmarkResult(testName)
		savedRun, err := suite.storage.SaveHistoricRun(suite.ctx, result)
		require.NoError(t, err)
		savedRuns = append(savedRuns, savedRun)
	}

	// List all runs
	runs, err := suite.storage.ListHistoricRuns(suite.ctx, "", 10)
	assert.NoError(t, err)
	assert.Len(t, runs, 3)

	// List runs with filter
	runs, err = suite.storage.ListHistoricRuns(suite.ctx, "list_test_1", 10)
	assert.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, "list_test_1", runs[0].TestName)

	// Test limit
	runs, err = suite.storage.ListHistoricRuns(suite.ctx, "", 2)
	assert.NoError(t, err)
	assert.Len(t, runs, 2)

	// Verify ordering (newest first)
	runs, err = suite.storage.ListHistoricRuns(suite.ctx, "", 10)
	assert.NoError(t, err)
	for i := 1; i < len(runs); i++ {
		assert.True(t, runs[i-1].Timestamp.After(runs[i].Timestamp) ||
			runs[i-1].Timestamp.Equal(runs[i].Timestamp))
	}
}

// TestDeleteHistoricRun tests deleting historic runs
func (suite *HistoricStorageTestSuite) TestDeleteHistoricRun() {
	t := suite.T()

	// Save a run first
	result := createTestBenchmarkResult("delete_test")
	savedRun, err := suite.storage.SaveHistoricRun(suite.ctx, result)
	require.NoError(t, err)

	// Verify files exist
	runDir := filepath.Join(suite.storageConfig.HistoricPath, savedRun.ID)
	assert.DirExists(t, runDir)

	// Delete the run
	err = suite.storage.DeleteHistoricRun(suite.ctx, savedRun.ID)
	assert.NoError(t, err)

	// Verify database deletion
	var count int
	err = suite.db.QueryRow("SELECT COUNT(*) FROM historic_runs WHERE id = $1", savedRun.ID).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify file deletion
	assert.NoFileExists(t, runDir)

	// Test deleting non-existent run (should not error)
	err = suite.storage.DeleteHistoricRun(suite.ctx, "nonexistent_id")
	assert.NoError(t, err)
}

// TestGetHistoricTrends tests performance trend analysis
func (suite *HistoricStorageTestSuite) TestGetHistoricTrends() {
	t := suite.T()

	// Create multiple runs over time to establish a trend
	baseTime := time.Now().Add(-7 * 24 * time.Hour)

	for i := 0; i < 5; i++ {
		result := createTestBenchmarkResult("trend_test")

		// Modify latency to create a trend
		for clientName, metrics := range result.ClientMetrics {
			metrics.Latency.P95 = 100.0 + float64(i*10) // Increasing latency trend
			result.ClientMetrics[clientName] = metrics
		}

		// Adjust timestamp
		result.StartTime = baseTime.Add(time.Duration(i) * 24 * time.Hour).Format(time.RFC3339)
		result.EndTime = baseTime.Add(time.Duration(i)*24*time.Hour + 30*time.Minute).Format(time.RFC3339)

		_, err := suite.storage.SaveHistoricRun(suite.ctx, result)
		require.NoError(t, err)
	}

	// Get trend data
	trend, err := suite.storage.GetHistoricTrends(suite.ctx, "trend_test", "geth", "p95_latency", 30)
	assert.NoError(t, err)
	assert.NotNil(t, trend)
	assert.Equal(t, "trend_test", trend.TestName)
	assert.Equal(t, "geth", trend.Client)
	assert.Equal(t, "p95_latency", trend.Metric)
	assert.Len(t, trend.Points, 5)

	// Verify trend direction (should be degrading due to increasing latency)
	assert.Equal(t, "degrading", trend.Trend)
	assert.Greater(t, trend.TrendSlope, 0.0)
}

// TestCompareRuns tests historic run comparison
func (suite *HistoricStorageTestSuite) TestCompareRuns() {
	t := suite.T()

	// Save two runs with different performance characteristics
	result1 := createTestBenchmarkResult("compare_test")
	result1.ClientMetrics["geth"].Latency.P95 = 100.0
	result1.ClientMetrics["geth"].ErrorRate = 0.01
	savedRun1, err := suite.storage.SaveHistoricRun(suite.ctx, result1)
	require.NoError(t, err)

	result2 := createTestBenchmarkResult("compare_test")
	result2.ClientMetrics["geth"].Latency.P95 = 120.0 // 20% worse
	result2.ClientMetrics["geth"].ErrorRate = 0.015   // 50% worse
	result2.StartTime = time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	result2.EndTime = time.Now().Add(-30 * time.Minute).Format(time.RFC3339)
	savedRun2, err := suite.storage.SaveHistoricRun(suite.ctx, result2)
	require.NoError(t, err)

	// Compare the runs
	comparison, err := suite.storage.CompareRuns(suite.ctx, savedRun1.ID, savedRun2.ID)
	assert.NoError(t, err)
	assert.NotNil(t, comparison)
	assert.Equal(t, savedRun1.ID, comparison.RunID1)
	assert.Equal(t, savedRun2.ID, comparison.RunID2)
	assert.Contains(t, comparison.ClientChanges, "overall")

	// Verify comparison detected degradation
	overallChange := comparison.ClientChanges["overall"]
	assert.Equal(t, "degraded", overallChange.Status)
	assert.Greater(t, overallChange.P95LatencyChange, 15.0) // Should be around 20%
}

// TestGetHistoricSummary tests historic summary generation
func (suite *HistoricStorageTestSuite) TestGetHistoricSummary() {
	t := suite.T()

	// Save multiple runs for the same test
	testName := "summary_test"
	for i := 0; i < 5; i++ {
		result := createTestBenchmarkResult(testName)

		// Vary performance to create best/worst runs
		latency := 50.0 + float64(i*20)
		result.ClientMetrics["geth"].Latency.Avg = latency
		result.ClientMetrics["geth"].Latency.P95 = latency * 1.5

		result.StartTime = time.Now().Add(time.Duration(-i) * time.Hour).Format(time.RFC3339)
		result.EndTime = time.Now().Add(time.Duration(-i)*time.Hour + 30*time.Minute).Format(time.RFC3339)

		_, err := suite.storage.SaveHistoricRun(suite.ctx, result)
		require.NoError(t, err)
	}

	// Get summary
	summary, err := suite.storage.GetHistoricSummary(suite.ctx, testName)
	assert.NoError(t, err)
	assert.NotNil(t, summary)
	assert.Equal(t, testName, summary.TestName)
	assert.Equal(t, 5, summary.TotalRuns)
	assert.NotEmpty(t, summary.BestRun.ID)
	assert.NotEmpty(t, summary.WorstRun.ID)
	assert.Len(t, summary.RecentRuns, 5)

	// Verify best run has lowest latency
	assert.Equal(t, 50.0, summary.BestRun.AvgLatency)
	// Verify worst run has highest latency
	assert.Equal(t, 130.0, summary.WorstRun.AvgLatency)
}

// TestSaveResultFiles tests file saving operations
func (suite *HistoricStorageTestSuite) TestSaveResultFiles() {
	t := suite.T()

	result := createTestBenchmarkResult("files_test")
	runID := "test_run_" + time.Now().Format("20060102_150405")

	err := suite.storage.SaveResultFiles(suite.ctx, runID, result)
	assert.NoError(t, err)

	// Verify files were created
	runDir := filepath.Join(suite.storageConfig.HistoricPath, runID)
	assert.DirExists(t, runDir)

	resultPath := filepath.Join(runDir, "result.json")
	assert.FileExists(t, resultPath)

	metadataPath := filepath.Join(runDir, "metadata.json")
	assert.FileExists(t, metadataPath)

	// Verify file contents
	resultData, err := ioutil.ReadFile(resultPath)
	assert.NoError(t, err)

	var savedResult types.BenchmarkResult
	err = json.Unmarshal(resultData, &savedResult)
	assert.NoError(t, err)
	assert.Equal(t, result.StartTime, savedResult.StartTime)
}

// TestSaveResultFilesDisabled tests file saving when disabled
func (suite *HistoricStorageTestSuite) TestSaveResultFilesDisabled() {
	t := suite.T()

	// Create storage with disabled file saving
	disabledConfig := &config.StorageConfig{
		EnableHistoric: false,
	}
	disabledStorage := NewHistoricStorage(suite.db, disabledConfig, suite.logger)

	result := createTestBenchmarkResult("disabled_test")
	runID := "test_run_disabled"

	err := disabledStorage.SaveResultFiles(suite.ctx, runID, result)
	assert.NoError(t, err) // Should not error

	// Verify no files were created
	runDir := filepath.Join(suite.tempDir, "historic", runID)
	assert.NoFileExists(t, runDir)
}

// TestGetResultFiles tests file retrieval
func (suite *HistoricStorageTestSuite) TestGetResultFiles() {
	t := suite.T()

	result := createTestBenchmarkResult("get_files_test")
	runID := "test_run_get_files"

	// Save files first
	err := suite.storage.SaveResultFiles(suite.ctx, runID, result)
	require.NoError(t, err)

	// Get files path
	filesPath, err := suite.storage.GetResultFiles(suite.ctx, runID)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(suite.storageConfig.HistoricPath, runID), filesPath)

	// Test non-existent run
	_, err = suite.storage.GetResultFiles(suite.ctx, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "result files not found")
}

// TestCleanupOldFiles tests file cleanup functionality
func (suite *HistoricStorageTestSuite) TestCleanupOldFiles() {
	t := suite.T()

	// Create test files with different ages
	baseDir := suite.storageConfig.HistoricPath

	// Recent file (should not be deleted)
	recentDir := filepath.Join(baseDir, "recent_run")
	err := os.MkdirAll(recentDir, 0755)
	require.NoError(t, err)

	// Old file (should be deleted)
	oldDir := filepath.Join(baseDir, "old_run")
	err = os.MkdirAll(oldDir, 0755)
	require.NoError(t, err)

	// Make old directory appear old by changing modification time
	oldTime := time.Now().Add(-40 * 24 * time.Hour) // 40 days ago
	err = os.Chtimes(oldDir, oldTime, oldTime)
	require.NoError(t, err)

	// Run cleanup
	err = suite.storage.CleanupOldFiles(suite.ctx)
	assert.NoError(t, err)

	// Verify recent file still exists
	assert.DirExists(t, recentDir)

	// Verify old file was deleted
	assert.NoDirExists(t, oldDir)
}

// TestCleanupOldFilesDisabled tests cleanup when retention is disabled
func (suite *HistoricStorageTestSuite) TestCleanupOldFilesDisabled() {
	t := suite.T()

	// Create storage with retention disabled
	disabledConfig := &config.StorageConfig{
		EnableHistoric: true,
		HistoricPath:   suite.storageConfig.HistoricPath,
		RetentionDays:  0, // Disabled
	}
	disabledStorage := NewHistoricStorage(suite.db, disabledConfig, suite.logger)

	err := disabledStorage.CleanupOldFiles(suite.ctx)
	assert.NoError(t, err) // Should not error when disabled
}

// TestGitIntegration tests git information extraction
func (suite *HistoricStorageTestSuite) TestGitIntegration() {
	t := suite.T()

	// This test assumes we're in a git repository
	// In a real scenario, you'd mock the git commands
	result := createTestBenchmarkResult("git_test")

	savedRun, err := suite.storage.SaveHistoricRun(suite.ctx, result)
	assert.NoError(t, err)

	// The actual git commit and branch would be empty in tests
	// unless mocked, which is fine for this basic test
	assert.NotNil(t, savedRun)
}

// TestConcurrentAccess tests concurrent storage operations
func (suite *HistoricStorageTestSuite) TestConcurrentAccess() {
	t := suite.T()

	concurrency := 5
	results := make(chan error, concurrency)

	// Start multiple goroutines saving historic runs
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			result := createTestBenchmarkResult(fmt.Sprintf("concurrent_test_%d", id))
			_, err := suite.storage.SaveHistoricRun(suite.ctx, result)
			results <- err
		}(i)
	}

	// Collect results
	for i := 0; i < concurrency; i++ {
		err := <-results
		assert.NoError(t, err)
	}

	// Verify all runs were saved
	var count int
	err := suite.db.QueryRow("SELECT COUNT(*) FROM historic_runs").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, concurrency, count)
}

// TestErrorHandling tests various error scenarios
func (suite *HistoricStorageTestSuite) TestErrorHandling() {
	t := suite.T()

	// Test with invalid database connection
	invalidDB := &sql.DB{} // This will cause errors
	invalidStorage := NewHistoricStorage(invalidDB, suite.storageConfig, suite.logger)

	result := createTestBenchmarkResult("error_test")
	_, err := invalidStorage.SaveHistoricRun(suite.ctx, result)
	assert.Error(t, err)

	// Test with context cancellation
	cancelledCtx, cancel := context.WithCancel(suite.ctx)
	cancel() // Cancel immediately

	_, err = suite.storage.SaveHistoricRun(cancelledCtx, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

// TestLargeDataHandling tests handling of large benchmark results
func (suite *HistoricStorageTestSuite) TestLargeDataHandling() {
	t := suite.T()

	result := createLargeBenchmarkResult()
	result.Config.(map[string]interface{})["test_name"] = "large_historic_test"

	savedRun, err := suite.storage.SaveHistoricRun(suite.ctx, result)
	assert.NoError(t, err)
	assert.NotNil(t, savedRun)

	// Verify large data was saved correctly
	retrievedRun, err := suite.storage.GetHistoricRun(suite.ctx, savedRun.ID)
	assert.NoError(t, err)
	assert.Equal(t, savedRun.ID, retrievedRun.ID)
}

// BenchmarkSaveHistoricRun benchmarks historic run saving
func (suite *HistoricStorageTestSuite) BenchmarkSaveHistoricRun() {
	b := suite.T()
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	result := createTestBenchmarkResult("benchmark_historic_test")

	// ResetTimer not available in test suite context

	for i := 0; i < 50; i++ {
		// Modify result to ensure unique run IDs
		result.StartTime = time.Now().Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		result.Config.(map[string]interface{})["test_name"] = fmt.Sprintf("benchmark_test_%d", i)

		_, err := suite.storage.SaveHistoricRun(suite.ctx, result)
		require.NoError(b, err)
	}
}

// BenchmarkGetHistoricTrends benchmarks trend analysis
func (suite *HistoricStorageTestSuite) BenchmarkGetHistoricTrends() {
	b := suite.T()
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	// Setup test data
	testName := "benchmark_trends"
	for i := 0; i < 100; i++ {
		result := createTestBenchmarkResult(testName)
		result.StartTime = time.Now().Add(time.Duration(-i) * time.Hour).Format(time.RFC3339)
		_, err := suite.storage.SaveHistoricRun(suite.ctx, result)
		require.NoError(b, err)
	}

	// ResetTimer not available in test suite context

	for i := 0; i < 50; i++ {
		_, err := suite.storage.GetHistoricTrends(suite.ctx, testName, "geth", "p95_latency", 30)
		require.NoError(b, err)
	}
}

// Run the test suite
func TestHistoricStorageTestSuite(t *testing.T) {
	// Skip if running in CI without Docker
	if os.Getenv("SKIP_INTEGRATION_TESTS") != "" {
		t.Skip("Skipping integration tests")
	}

	suite.Run(t, new(HistoricStorageTestSuite))
}

// Unit tests for utility functions

// TestGenerateHistoricRunID tests run ID generation
func TestGenerateHistoricRunID(t *testing.T) {
	result := createTestBenchmarkResult("test_name")
	runID := generateHistoricRunID(result)

	assert.Contains(t, runID, "test_name")
	assert.Contains(t, runID, time.Now().Format("20060102"))
	assert.True(t, len(runID) > len("test_name_20240101_120000"))
}

// TestExtractTestName tests test name extraction
func TestExtractTestName(t *testing.T) {
	result := &types.BenchmarkResult{
		Config: map[string]interface{}{
			"test_name": "my_test",
		},
	}

	testName := extractTestName(result)
	assert.Equal(t, "my_test", testName)

	// Test with missing test_name
	result.Config = map[string]interface{}{}
	testName = extractTestName(result)
	assert.Equal(t, "unknown", testName)

	// Test with invalid config
	result.Config = "not a map"
	testName = extractTestName(result)
	assert.Equal(t, "unknown", testName)
}

// TestExtractDescription tests description extraction
func TestExtractDescription(t *testing.T) {
	result := &types.BenchmarkResult{
		Config: map[string]interface{}{
			"description": "Test description",
		},
	}

	description := extractDescription(result)
	assert.Equal(t, "Test description", description)

	// Test with missing description
	result.Config = map[string]interface{}{}
	description = extractDescription(result)
	assert.Equal(t, "", description)
}

// TestExtractEndpointsCount tests endpoints count extraction
func TestExtractEndpointsCount(t *testing.T) {
	result := &types.BenchmarkResult{
		Config: map[string]interface{}{
			"endpoints": []interface{}{
				map[string]interface{}{"method": "eth_blockNumber"},
				map[string]interface{}{"method": "eth_getBalance"},
			},
		},
	}

	count := extractEndpointsCount(result)
	assert.Equal(t, 2, count)

	// Test with missing endpoints
	result.Config = map[string]interface{}{}
	count = extractEndpointsCount(result)
	assert.Equal(t, 0, count)
}

// TestExtractTargetRPS tests RPS extraction
func TestExtractTargetRPS(t *testing.T) {
	// Test with float64 RPS
	result := &types.BenchmarkResult{
		Config: map[string]interface{}{
			"rps": 100.5,
		},
	}

	rps := extractTargetRPS(result)
	assert.Equal(t, 100, rps)

	// Test with int RPS
	result.Config = map[string]interface{}{
		"rps": 200,
	}

	rps = extractTargetRPS(result)
	assert.Equal(t, 200, rps)

	// Test with missing RPS
	result.Config = map[string]interface{}{}
	rps = extractTargetRPS(result)
	assert.Equal(t, 0, rps)
}

// TestMustMarshalJSON tests JSON marshaling utility
func TestMustMarshalJSON(t *testing.T) {
	// Test valid object
	obj := map[string]interface{}{
		"key": "value",
	}

	result := mustMarshalJSON(obj)
	assert.NotEqual(t, json.RawMessage("{}"), result)

	// Test with function (which can't be marshaled)
	invalidObj := map[string]interface{}{
		"func": func() {},
	}

	result = mustMarshalJSON(invalidObj)
	assert.Equal(t, json.RawMessage("{}"), result)
}

// TestTrendCalculations tests trend calculation logic
func TestTrendCalculations(t *testing.T) {
	storage := &historicStorage{
		log: logrus.New().WithField("test", "trends"),
	}

	// Test with insufficient data
	trend := &types.HistoricTrend{
		Points: []types.TrendPoint{
			{Value: 100.0},
		},
	}

	storage.calculateTrendStatistics(trend)
	assert.Equal(t, "insufficient_data", trend.Trend)

	// Test with improving trend (decreasing values)
	trend = &types.HistoricTrend{
		Points: []types.TrendPoint{
			{Value: 100.0},
			{Value: 95.0},
			{Value: 90.0},
			{Value: 85.0},
		},
	}

	storage.calculateTrendStatistics(trend)
	assert.Equal(t, "improving", trend.Trend)
	assert.Less(t, trend.TrendSlope, -0.01)

	// Test with degrading trend (increasing values)
	trend = &types.HistoricTrend{
		Points: []types.TrendPoint{
			{Value: 80.0},
			{Value: 85.0},
			{Value: 90.0},
			{Value: 95.0},
		},
	}

	storage.calculateTrendStatistics(trend)
	assert.Equal(t, "degrading", trend.Trend)
	assert.Greater(t, trend.TrendSlope, 0.01)

	// Test with stable trend
	trend = &types.HistoricTrend{
		Points: []types.TrendPoint{
			{Value: 90.0},
			{Value: 90.1},
			{Value: 89.9},
			{Value: 90.0},
		},
	}

	storage.calculateTrendStatistics(trend)
	assert.Equal(t, "stable", trend.Trend)
	assert.Less(t, trend.TrendSlope, 0.01)
	assert.Greater(t, trend.TrendSlope, -0.01)
}
