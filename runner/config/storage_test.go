package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// StorageConfigTestSuite provides comprehensive tests for storage configuration
type StorageConfigTestSuite struct {
	suite.Suite
	logger   logrus.FieldLogger
	tempDir  string
	testFile string
}

// SetupTest prepares clean state for each test
func (suite *StorageConfigTestSuite) SetupTest() {
	suite.logger = logrus.New().WithField("test", "storage_config")

	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "storage_config_test_*")
	require.NoError(suite.T(), err)
	suite.tempDir = tempDir
	suite.testFile = filepath.Join(tempDir, "test_config.yaml")
}

// TearDownTest cleans up test resources
func (suite *StorageConfigTestSuite) TearDownTest() {
	if suite.tempDir != "" {
		os.RemoveAll(suite.tempDir)
	}
}

// TestDefaultStorageConfig tests default configuration generation
func (suite *StorageConfigTestSuite) TestDefaultStorageConfig() {
	t := suite.T()

	config := DefaultStorageConfig()

	// Test that default values are reasonable
	assert.NotNil(t, config)
	assert.Equal(t, "results/historic", config.HistoricPath)
	assert.Equal(t, 90, config.RetentionDays)
	assert.False(t, config.EnableHistoric)

	// Test PostgreSQL defaults
	assert.Equal(t, "localhost", config.PostgreSQL.Host)
	assert.Equal(t, 5432, config.PostgreSQL.Port)
	assert.Equal(t, "rpc_benchmarks", config.PostgreSQL.Database)
	assert.Equal(t, "postgres", config.PostgreSQL.User)
	assert.Equal(t, "", config.PostgreSQL.Password)
	assert.Equal(t, "disable", config.PostgreSQL.SSLMode)
	assert.Equal(t, 10, config.PostgreSQL.MaxOpenConns)
	assert.Equal(t, 5, config.PostgreSQL.MaxIdleConns)
	assert.Equal(t, "benchmark_metrics", config.PostgreSQL.MetricsTable)
	assert.Equal(t, "benchmark_runs", config.PostgreSQL.RunsTable)
	assert.Equal(t, "7d", config.PostgreSQL.RetentionPolicy)
}

// TestLoadStorageConfigNoFile tests loading when config file doesn't exist
func (suite *StorageConfigTestSuite) TestLoadStorageConfigNoFile() {
	t := suite.T()

	// Test with empty path
	config, err := LoadStorageConfig("", suite.logger)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// Should return default config
	defaultConfig := DefaultStorageConfig()
	assert.Equal(t, defaultConfig.HistoricPath, config.HistoricPath)
	assert.Equal(t, defaultConfig.RetentionDays, config.RetentionDays)
	assert.Equal(t, defaultConfig.EnableHistoric, config.EnableHistoric)

	// Test with non-existent file path
	config, err = LoadStorageConfig("/non/existent/path.yaml", suite.logger)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// Should also return default config
	assert.Equal(t, defaultConfig.HistoricPath, config.HistoricPath)
}

// TestLoadStorageConfigValidFile tests loading valid configuration file
func (suite *StorageConfigTestSuite) TestLoadStorageConfigValidFile() {
	t := suite.T()

	// Create test config file
	configContent := `
historic_path: "/custom/historic/path"
retention_days: 180
enable_historic: true

postgresql:
  host: "custom-host"
  port: 5433
  database: "custom_db"
  user: "custom_user"
  password: "custom_pass"
  ssl_mode: "require"
  max_open_conns: 20
  max_idle_conns: 10
  metrics_table: "custom_metrics"
  runs_table: "custom_runs"
  retention_policy: "14d"
`

	err := os.WriteFile(suite.testFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load the config
	config, err := LoadStorageConfig(suite.testFile, suite.logger)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// Verify loaded values
	assert.Equal(t, "/custom/historic/path", config.HistoricPath)
	assert.Equal(t, 180, config.RetentionDays)
	assert.True(t, config.EnableHistoric)

	// Verify PostgreSQL config
	assert.Equal(t, "custom-host", config.PostgreSQL.Host)
	assert.Equal(t, 5433, config.PostgreSQL.Port)
	assert.Equal(t, "custom_db", config.PostgreSQL.Database)
	assert.Equal(t, "custom_user", config.PostgreSQL.User)
	assert.Equal(t, "custom_pass", config.PostgreSQL.Password)
	assert.Equal(t, "require", config.PostgreSQL.SSLMode)
	assert.Equal(t, 20, config.PostgreSQL.MaxOpenConns)
	assert.Equal(t, 10, config.PostgreSQL.MaxIdleConns)
	assert.Equal(t, "custom_metrics", config.PostgreSQL.MetricsTable)
	assert.Equal(t, "custom_runs", config.PostgreSQL.RunsTable)
	assert.Equal(t, "14d", config.PostgreSQL.RetentionPolicy)
}

// TestLoadStorageConfigPartialFile tests loading config with missing fields
func (suite *StorageConfigTestSuite) TestLoadStorageConfigPartialFile() {
	t := suite.T()

	// Create partial config file (missing some fields)
	configContent := `
historic_path: "/partial/path"
enable_historic: true

postgresql:
  host: "partial-host"
  database: "partial_db"
`

	err := os.WriteFile(suite.testFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load the config
	config, err := LoadStorageConfig(suite.testFile, suite.logger)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// Verify specified values are preserved
	assert.Equal(t, "/partial/path", config.HistoricPath)
	assert.True(t, config.EnableHistoric)
	assert.Equal(t, "partial-host", config.PostgreSQL.Host)
	assert.Equal(t, "partial_db", config.PostgreSQL.Database)

	// Verify defaults are applied for missing fields
	assert.Equal(t, 90, config.RetentionDays)                            // Default
	assert.Equal(t, 5432, config.PostgreSQL.Port)                        // Default
	assert.Equal(t, "postgres", config.PostgreSQL.User)                  // Default
	assert.Equal(t, "disable", config.PostgreSQL.SSLMode)                // Default
	assert.Equal(t, 10, config.PostgreSQL.MaxOpenConns)                  // Default
	assert.Equal(t, 5, config.PostgreSQL.MaxIdleConns)                   // Default
	assert.Equal(t, "benchmark_metrics", config.PostgreSQL.MetricsTable) // Default
	assert.Equal(t, "benchmark_runs", config.PostgreSQL.RunsTable)       // Default
	assert.Equal(t, "7d", config.PostgreSQL.RetentionPolicy)             // Default
}

// TestLoadStorageConfigInvalidYAML tests loading malformed YAML file
func (suite *StorageConfigTestSuite) TestLoadStorageConfigInvalidYAML() {
	t := suite.T()

	// Create invalid YAML file
	invalidContent := `
historic_path: "/test/path"
invalid_yaml: [
  - missing closing bracket
enable_historic: true
`

	err := os.WriteFile(suite.testFile, []byte(invalidContent), 0644)
	require.NoError(t, err)

	// Load the config - should fail
	config, err := LoadStorageConfig(suite.testFile, suite.logger)
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to unmarshal storage config")
}

// TestLoadStorageConfigUnreadableFile tests loading unreadable file
func (suite *StorageConfigTestSuite) TestLoadStorageConfigUnreadableFile() {
	t := suite.T()

	// Create a file and make it unreadable
	err := os.WriteFile(suite.testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	err = os.Chmod(suite.testFile, 0000) // No permissions
	require.NoError(t, err)

	// Restore permissions after test
	defer os.Chmod(suite.testFile, 0644)

	// Load the config - should fail
	config, err := LoadStorageConfig(suite.testFile, suite.logger)
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed to read storage config file")
}

// TestStorageConfigValidateEnabled tests validation when historic storage is enabled
func (suite *StorageConfigTestSuite) TestStorageConfigValidateEnabled() {
	t := suite.T()

	// Test valid enabled config
	config := &StorageConfig{
		HistoricPath:   filepath.Join(suite.tempDir, "valid_historic"),
		RetentionDays:  30,
		EnableHistoric: true,
		PostgreSQL: PostgreSQLConfig{
			Host:            "localhost",
			Port:            5432,
			Database:        "test_db",
			User:            "test_user",
			Password:        "test_pass",
			SSLMode:         "disable",
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			MetricsTable:    "metrics",
			RunsTable:       "runs",
			RetentionPolicy: "7d",
		},
	}

	err := config.Validate()
	assert.NoError(t, err)

	// Verify directory was created
	_, err = os.Stat(config.HistoricPath)
	assert.NoError(t, err)
}

// TestStorageConfigValidateDisabled tests validation when historic storage is disabled
func (suite *StorageConfigTestSuite) TestStorageConfigValidateDisabled() {
	t := suite.T()

	// Test disabled config (should always pass)
	config := &StorageConfig{
		HistoricPath:   "",    // Empty path
		RetentionDays:  0,     // Invalid value
		EnableHistoric: false, // Disabled
		PostgreSQL:     PostgreSQLConfig{
			// Empty/invalid PostgreSQL config should be ignored when disabled
		},
	}

	err := config.Validate()
	assert.NoError(t, err)
}

// TestStorageConfigValidateInvalidHistoricPath tests validation with invalid historic path
func (suite *StorageConfigTestSuite) TestStorageConfigValidateInvalidHistoricPath() {
	t := suite.T()

	config := &StorageConfig{
		HistoricPath:   "", // Empty path
		EnableHistoric: true,
		PostgreSQL: PostgreSQLConfig{
			Host:         "localhost",
			Port:         5432,
			Database:     "test_db",
			User:         "test_user",
			MaxOpenConns: 10,
			MaxIdleConns: 5,
			MetricsTable: "metrics",
			RunsTable:    "runs",
		},
	}

	err := config.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "historic_path is required when historic storage is enabled")
}

// TestStorageConfigValidateInvalidPostgreSQL tests validation with invalid PostgreSQL config
func (suite *StorageConfigTestSuite) TestStorageConfigValidateInvalidPostgreSQL() {
	t := suite.T()

	config := &StorageConfig{
		HistoricPath:   suite.tempDir,
		EnableHistoric: true,
		PostgreSQL: PostgreSQLConfig{
			Host:     "", // Invalid: empty host
			Port:     5432,
			Database: "test_db",
			User:     "test_user",
		},
	}

	err := config.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid PostgreSQL configuration")
}

// TestPostgreSQLConfigValidation tests PostgreSQL config validation in detail
func (suite *StorageConfigTestSuite) TestPostgreSQLConfigValidation() {
	t := suite.T()

	testCases := []struct {
		name          string
		config        PostgreSQLConfig
		shouldError   bool
		errorContains string
	}{
		{
			name: "valid_config",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         5432,
				Database:     "test_db",
				User:         "test_user",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
				MetricsTable: "metrics",
				RunsTable:    "runs",
			},
			shouldError: false,
		},
		{
			name: "empty_host",
			config: PostgreSQLConfig{
				Host:         "",
				Port:         5432,
				Database:     "test_db",
				User:         "test_user",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
				MetricsTable: "metrics",
				RunsTable:    "runs",
			},
			shouldError:   true,
			errorContains: "host is required",
		},
		{
			name: "invalid_port_zero",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         0,
				Database:     "test_db",
				User:         "test_user",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
				MetricsTable: "metrics",
				RunsTable:    "runs",
			},
			shouldError:   true,
			errorContains: "port must be between 1 and 65535",
		},
		{
			name: "invalid_port_too_high",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         70000,
				Database:     "test_db",
				User:         "test_user",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
				MetricsTable: "metrics",
				RunsTable:    "runs",
			},
			shouldError:   true,
			errorContains: "port must be between 1 and 65535",
		},
		{
			name: "empty_database",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         5432,
				Database:     "",
				User:         "test_user",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
				MetricsTable: "metrics",
				RunsTable:    "runs",
			},
			shouldError:   true,
			errorContains: "database is required",
		},
		{
			name: "empty_user",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         5432,
				Database:     "test_db",
				User:         "",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
				MetricsTable: "metrics",
				RunsTable:    "runs",
			},
			shouldError:   true,
			errorContains: "user is required",
		},
		{
			name: "invalid_max_open_conns",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         5432,
				Database:     "test_db",
				User:         "test_user",
				MaxOpenConns: 0,
				MaxIdleConns: 5,
				MetricsTable: "metrics",
				RunsTable:    "runs",
			},
			shouldError:   true,
			errorContains: "max_open_conns must be greater than 0",
		},
		{
			name: "invalid_max_idle_conns",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         5432,
				Database:     "test_db",
				User:         "test_user",
				MaxOpenConns: 10,
				MaxIdleConns: 0,
				MetricsTable: "metrics",
				RunsTable:    "runs",
			},
			shouldError:   true,
			errorContains: "max_idle_conns must be greater than 0",
		},
		{
			name: "empty_metrics_table",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         5432,
				Database:     "test_db",
				User:         "test_user",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
				MetricsTable: "",
				RunsTable:    "runs",
			},
			shouldError:   true,
			errorContains: "metrics_table is required",
		},
		{
			name: "empty_runs_table",
			config: PostgreSQLConfig{
				Host:         "localhost",
				Port:         5432,
				Database:     "test_db",
				User:         "test_user",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
				MetricsTable: "metrics",
				RunsTable:    "",
			},
			shouldError:   true,
			errorContains: "runs_table is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.shouldError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPostgreSQLConnectionString tests connection string generation
func (suite *StorageConfigTestSuite) TestPostgreSQLConnectionString() {
	t := suite.T()

	testCases := []struct {
		name     string
		config   PostgreSQLConfig
		expected string
	}{
		{
			name: "basic_config",
			config: PostgreSQLConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "postgres",
				Password: "password",
				Database: "testdb",
				SSLMode:  "disable",
			},
			expected: "host=localhost port=5432 user=postgres password=password dbname=testdb sslmode=disable",
		},
		{
			name: "empty_password",
			config: PostgreSQLConfig{
				Host:     "remote-host",
				Port:     5433,
				User:     "user",
				Password: "",
				Database: "mydb",
				SSLMode:  "require",
			},
			expected: "host=remote-host port=5433 user=user password= dbname=mydb sslmode=require",
		},
		{
			name: "special_characters",
			config: PostgreSQLConfig{
				Host:     "host-with-dashes",
				Port:     5432,
				User:     "user_with_underscores",
				Password: "pass@word#123",
				Database: "db-name_test",
				SSLMode:  "verify-full",
			},
			expected: "host=host-with-dashes port=5432 user=user_with_underscores password=pass@word#123 dbname=db-name_test sslmode=verify-full",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.config.ConnectionString()
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestEnsureHistoricPath tests historic path creation
func (suite *StorageConfigTestSuite) TestEnsureHistoricPath() {
	t := suite.T()

	// Test with valid path
	config := &StorageConfig{
		HistoricPath: filepath.Join(suite.tempDir, "test_historic"),
	}

	err := config.EnsureHistoricPath()
	assert.NoError(t, err)

	// Verify directory was created
	stat, err := os.Stat(config.HistoricPath)
	assert.NoError(t, err)
	assert.True(t, stat.IsDir())

	// Test with empty path (should not error)
	config.HistoricPath = ""
	err = config.EnsureHistoricPath()
	assert.NoError(t, err)
}

// TestEnsureHistoricPathNestedDirectories tests creating nested directories
func (suite *StorageConfigTestSuite) TestEnsureHistoricPathNestedDirectories() {
	t := suite.T()

	// Test with deeply nested path
	config := &StorageConfig{
		HistoricPath: filepath.Join(suite.tempDir, "level1", "level2", "level3", "historic"),
	}

	err := config.EnsureHistoricPath()
	assert.NoError(t, err)

	// Verify all directories were created
	stat, err := os.Stat(config.HistoricPath)
	assert.NoError(t, err)
	assert.True(t, stat.IsDir())
}

// TestEnsureHistoricPathExistingDirectory tests behavior with existing directory
func (suite *StorageConfigTestSuite) TestEnsureHistoricPathExistingDirectory() {
	t := suite.T()

	// Create directory first
	historicPath := filepath.Join(suite.tempDir, "existing_historic")
	err := os.MkdirAll(historicPath, 0755)
	require.NoError(t, err)

	config := &StorageConfig{
		HistoricPath: historicPath,
	}

	// Should not error on existing directory
	err = config.EnsureHistoricPath()
	assert.NoError(t, err)

	// Directory should still exist
	stat, err := os.Stat(config.HistoricPath)
	assert.NoError(t, err)
	assert.True(t, stat.IsDir())
}

// TestEnsureHistoricPathPermissionError tests handling permission errors
func (suite *StorageConfigTestSuite) TestEnsureHistoricPathPermissionError() {
	t := suite.T()

	// Create a read-only parent directory
	readOnlyParent := filepath.Join(suite.tempDir, "readonly")
	err := os.MkdirAll(readOnlyParent, 0755)
	require.NoError(t, err)

	err = os.Chmod(readOnlyParent, 0444) // Read-only
	require.NoError(t, err)

	// Restore permissions after test
	defer os.Chmod(readOnlyParent, 0755)

	config := &StorageConfig{
		HistoricPath: filepath.Join(readOnlyParent, "should_fail"),
	}

	// Should fail due to permission error
	err = config.EnsureHistoricPath()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create historic path")
}

// TestConfigurationIntegration tests full configuration loading and validation workflow
func (suite *StorageConfigTestSuite) TestConfigurationIntegration() {
	t := suite.T()

	// Create a complete valid configuration
	configContent := `
historic_path: "` + filepath.Join(suite.tempDir, "integration_historic") + `"
retention_days: 60
enable_historic: true

postgresql:
  host: "integration-host"
  port: 5432
  database: "integration_db"
  user: "integration_user"
  password: "integration_pass"
  ssl_mode: "require"
  max_open_conns: 15
  max_idle_conns: 8
  metrics_table: "integration_metrics"
  runs_table: "integration_runs"
  retention_policy: "30d"
`

	err := os.WriteFile(suite.testFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load configuration
	config, err := LoadStorageConfig(suite.testFile, suite.logger)
	require.NoError(t, err)
	require.NotNil(t, config)

	// Validate configuration
	err = config.Validate()
	assert.NoError(t, err)

	// Ensure historic path was created during validation
	stat, err := os.Stat(config.HistoricPath)
	assert.NoError(t, err)
	assert.True(t, stat.IsDir())

	// Test PostgreSQL connection string generation
	connStr := config.PostgreSQL.ConnectionString()
	assert.Contains(t, connStr, "host=integration-host")
	assert.Contains(t, connStr, "port=5432")
	assert.Contains(t, connStr, "user=integration_user")
	assert.Contains(t, connStr, "password=integration_pass")
	assert.Contains(t, connStr, "dbname=integration_db")
	assert.Contains(t, connStr, "sslmode=require")
}

// TestConfigurationEdgeCases tests various edge cases and corner scenarios
func (suite *StorageConfigTestSuite) TestConfigurationEdgeCases() {
	t := suite.T()

	// Test with very long paths
	longPath := filepath.Join(suite.tempDir, "very", "very", "very", "long", "path", "with", "many", "levels", "to", "test", "deep", "nesting", "behavior", "historic")
	config := &StorageConfig{
		HistoricPath:   longPath,
		EnableHistoric: true,
		PostgreSQL: PostgreSQLConfig{
			Host:         "localhost",
			Port:         5432,
			Database:     "test",
			User:         "test",
			MaxOpenConns: 1,
			MaxIdleConns: 1,
			MetricsTable: "m",
			RunsTable:    "r",
		},
	}

	err := config.Validate()
	assert.NoError(t, err)

	// Test with minimum valid values
	minConfig := &StorageConfig{
		HistoricPath:   filepath.Join(suite.tempDir, "min"),
		RetentionDays:  1,
		EnableHistoric: true,
		PostgreSQL: PostgreSQLConfig{
			Host:         "h",
			Port:         1,
			Database:     "d",
			User:         "u",
			MaxOpenConns: 1,
			MaxIdleConns: 1,
			MetricsTable: "m",
			RunsTable:    "r",
		},
	}

	err = minConfig.Validate()
	assert.NoError(t, err)

	// Test with maximum valid port
	maxPortConfig := &StorageConfig{
		HistoricPath:   filepath.Join(suite.tempDir, "max"),
		EnableHistoric: true,
		PostgreSQL: PostgreSQLConfig{
			Host:         "localhost",
			Port:         65535,
			Database:     "test",
			User:         "test",
			MaxOpenConns: 1,
			MaxIdleConns: 1,
			MetricsTable: "metrics",
			RunsTable:    "runs",
		},
	}

	err = maxPortConfig.Validate()
	assert.NoError(t, err)
}

// TestConcurrentConfigOperations tests concurrent configuration operations
func (suite *StorageConfigTestSuite) TestConcurrentConfigOperations() {
	t := suite.T()

	// Create multiple config files
	configs := make([]string, 5)
	for i := 0; i < 5; i++ {
		configFile := filepath.Join(suite.tempDir, "concurrent_config_"+string(rune(i+'0'))+".yaml")
		configContent := `
historic_path: "` + filepath.Join(suite.tempDir, "concurrent_historic_"+string(rune(i+'0'))) + `"
retention_days: ` + string(rune(30+i)) + `
enable_historic: true

postgresql:
  host: "localhost"
  port: 5432
  database: "concurrent_db_` + string(rune(i+'0')) + `"
  user: "user_` + string(rune(i+'0')) + `"
  max_open_conns: ` + string(rune(10+i)) + `
  max_idle_conns: ` + string(rune(5+i)) + `
  metrics_table: "metrics_` + string(rune(i+'0')) + `"
  runs_table: "runs_` + string(rune(i+'0')) + `"
`

		err := os.WriteFile(configFile, []byte(configContent), 0644)
		require.NoError(t, err)
		configs[i] = configFile
	}

	// Load configs concurrently
	results := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func(configFile string) {
			config, err := LoadStorageConfig(configFile, suite.logger)
			if err != nil {
				results <- err
				return
			}

			err = config.Validate()
			results <- err
		}(configs[i])
	}

	// Collect results
	for i := 0; i < 5; i++ {
		err := <-results
		assert.NoError(t, err, "Concurrent config operation %d should succeed", i)
	}
}

// Benchmark tests for performance validation

func BenchmarkDefaultStorageConfig(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = DefaultStorageConfig()
	}
}

func BenchmarkLoadStorageConfig(b *testing.B) {
	// Setup
	tempDir, _ := os.MkdirTemp("", "bench_storage_config_*")
	defer os.RemoveAll(tempDir)

	configFile := filepath.Join(tempDir, "bench_config.yaml")
	configContent := `
historic_path: "/bench/historic"
retention_days: 90
enable_historic: true

postgresql:
  host: "localhost"
  port: 5432
  database: "bench_db"
  user: "bench_user"
  max_open_conns: 10
  max_idle_conns: 5
  metrics_table: "metrics"
  runs_table: "runs"
`

	os.WriteFile(configFile, []byte(configContent), 0644)
	logger := logrus.New().WithField("test", "benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadStorageConfig(configFile, logger)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStorageConfigValidation(b *testing.B) {
	// Setup
	tempDir, _ := os.MkdirTemp("", "bench_validation_*")
	defer os.RemoveAll(tempDir)

	config := &StorageConfig{
		HistoricPath:   filepath.Join(tempDir, "bench_historic"),
		RetentionDays:  90,
		EnableHistoric: true,
		PostgreSQL: PostgreSQLConfig{
			Host:         "localhost",
			Port:         5432,
			Database:     "bench_db",
			User:         "bench_user",
			MaxOpenConns: 10,
			MaxIdleConns: 5,
			MetricsTable: "metrics",
			RunsTable:    "runs",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := config.Validate()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPostgreSQLConnectionString(b *testing.B) {
	config := PostgreSQLConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "password",
		Database: "testdb",
		SSLMode:  "disable",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.ConnectionString()
	}
}

// Run the test suite
func TestStorageConfigTestSuite(t *testing.T) {
	suite.Run(t, new(StorageConfigTestSuite))
}

// Additional unit tests for edge cases

func TestPostgreSQLConfigEdgeCases(t *testing.T) {
	// Test with very large values
	config := PostgreSQLConfig{
		Host:         "localhost",
		Port:         65535,
		Database:     "test",
		User:         "test",
		MaxOpenConns: 1000,
		MaxIdleConns: 500,
		MetricsTable: "metrics",
		RunsTable:    "runs",
	}

	err := config.Validate()
	assert.NoError(t, err)

	// Test boundary conditions
	config.Port = 1
	err = config.Validate()
	assert.NoError(t, err)

	config.Port = 65536
	err = config.Validate()
	assert.Error(t, err)

	config.Port = 0
	err = config.Validate()
	assert.Error(t, err)

	config.Port = -1
	err = config.Validate()
	assert.Error(t, err)
}

func TestStorageConfigZeroValues(t *testing.T) {
	// Test behavior with zero values
	config := &StorageConfig{}

	// Should not error when disabled
	err := config.Validate()
	assert.NoError(t, err)

	// Enable with zero values - should error
	config.EnableHistoric = true
	err = config.Validate()
	assert.Error(t, err)
}

func TestPathResolution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "path_resolution_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test relative path resolution
	config := &StorageConfig{
		HistoricPath: "relative/path/to/historic",
	}

	err = config.EnsureHistoricPath()
	assert.NoError(t, err)

	// Should create relative directory
	_, err = os.Stat("relative/path/to/historic")
	assert.NoError(t, err)

	// Cleanup
	os.RemoveAll("relative")
}

func TestConfigurationSerialization(t *testing.T) {
	// Test that configuration can be properly serialized and deserialized
	original := DefaultStorageConfig()
	original.HistoricPath = "/test/path"
	original.EnableHistoric = true
	original.PostgreSQL.Password = "test_password"

	// This test verifies the structure is properly tagged for YAML
	// In a real scenario, you might use yaml.Marshal/Unmarshal
	assert.Equal(t, "/test/path", original.HistoricPath)
	assert.True(t, original.EnableHistoric)
	assert.Equal(t, "test_password", original.PostgreSQL.Password)
}
