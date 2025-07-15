package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// StorageConfig holds configuration for historic storage
type StorageConfig struct {
	HistoricPath   string `yaml:"historic_path"`
	RetentionDays  int    `yaml:"retention_days"`
	EnableHistoric bool   `yaml:"enable_historic"`

	// PostgreSQL configuration
	PostgreSQL PostgreSQLConfig `yaml:"postgresql"`
}

// PostgreSQLConfig contains database configuration for Grafana integration
type PostgreSQLConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	Database     string `yaml:"database"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	SSLMode      string `yaml:"ssl_mode"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`

	// Grafana-specific settings
	MetricsTable    string `yaml:"metrics_table"`
	RunsTable       string `yaml:"runs_table"`
	RetentionPolicy string `yaml:"retention_policy"`
}

// DefaultStorageConfig returns a default storage configuration
func DefaultStorageConfig() *StorageConfig {
	return &StorageConfig{
		HistoricPath:   "results/historic",
		RetentionDays:  90,
		EnableHistoric: false,
		PostgreSQL: PostgreSQLConfig{
			Host:            "localhost",
			Port:            5432,
			Database:        "rpc_benchmarks",
			User:            "postgres",
			Password:        "",
			SSLMode:         "disable",
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			MetricsTable:    "benchmark_metrics",
			RunsTable:       "benchmark_runs",
			RetentionPolicy: "7d",
		},
	}
}

// LoadStorageConfig loads storage configuration from file
func LoadStorageConfig(path string, log logrus.FieldLogger) (*StorageConfig, error) {
	log = log.WithField("component", "storage_config")

	if path == "" {
		log.Info("No storage config path provided, using defaults")
		return DefaultStorageConfig(), nil
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.WithField("path", path).Info("Storage config file not found, using defaults")
		return DefaultStorageConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read storage config file: %w", err)
	}

	var cfg StorageConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal storage config: %w", err)
	}

	// Apply defaults for missing fields
	if cfg.HistoricPath == "" {
		cfg.HistoricPath = "results/historic"
	}
	if cfg.RetentionDays == 0 {
		cfg.RetentionDays = 90
	}
	if cfg.PostgreSQL.Host == "" {
		cfg.PostgreSQL.Host = "localhost"
	}
	if cfg.PostgreSQL.Port == 0 {
		cfg.PostgreSQL.Port = 5432
	}
	if cfg.PostgreSQL.Database == "" {
		cfg.PostgreSQL.Database = "rpc_benchmarks"
	}
	if cfg.PostgreSQL.User == "" {
		cfg.PostgreSQL.User = "postgres"
	}
	if cfg.PostgreSQL.SSLMode == "" {
		cfg.PostgreSQL.SSLMode = "disable"
	}
	if cfg.PostgreSQL.MaxOpenConns == 0 {
		cfg.PostgreSQL.MaxOpenConns = 10
	}
	if cfg.PostgreSQL.MaxIdleConns == 0 {
		cfg.PostgreSQL.MaxIdleConns = 5
	}
	if cfg.PostgreSQL.MetricsTable == "" {
		cfg.PostgreSQL.MetricsTable = "benchmark_metrics"
	}
	if cfg.PostgreSQL.RunsTable == "" {
		cfg.PostgreSQL.RunsTable = "benchmark_runs"
	}
	if cfg.PostgreSQL.RetentionPolicy == "" {
		cfg.PostgreSQL.RetentionPolicy = "7d"
	}

	log.WithFields(logrus.Fields{
		"historic_path":   cfg.HistoricPath,
		"retention_days":  cfg.RetentionDays,
		"enable_historic": cfg.EnableHistoric,
		"pg_host":         cfg.PostgreSQL.Host,
		"pg_port":         cfg.PostgreSQL.Port,
		"pg_database":     cfg.PostgreSQL.Database,
	}).Info("Loaded storage configuration")

	return &cfg, nil
}

// Validate validates the storage configuration
func (c *StorageConfig) Validate() error {
	if c.EnableHistoric {
		if c.HistoricPath == "" {
			return fmt.Errorf("historic_path is required when historic storage is enabled")
		}

		// Ensure historic path exists
		if err := os.MkdirAll(c.HistoricPath, 0755); err != nil {
			return fmt.Errorf("failed to create historic path: %w", err)
		}

		// Validate PostgreSQL config
		if err := c.PostgreSQL.Validate(); err != nil {
			return fmt.Errorf("invalid PostgreSQL configuration: %w", err)
		}
	}

	return nil
}

// Validate validates the PostgreSQL configuration
func (c *PostgreSQLConfig) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if c.Database == "" {
		return fmt.Errorf("database is required")
	}
	if c.User == "" {
		return fmt.Errorf("user is required")
	}
	if c.MaxOpenConns <= 0 {
		return fmt.Errorf("max_open_conns must be greater than 0")
	}
	if c.MaxIdleConns <= 0 {
		return fmt.Errorf("max_idle_conns must be greater than 0")
	}
	if c.MetricsTable == "" {
		return fmt.Errorf("metrics_table is required")
	}
	if c.RunsTable == "" {
		return fmt.Errorf("runs_table is required")
	}

	return nil
}

// ConnectionString returns the PostgreSQL connection string
func (c *PostgreSQLConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Database, c.SSLMode,
	)
}

// EnsureHistoricPath creates the historic path if it doesn't exist
func (c *StorageConfig) EnsureHistoricPath() error {
	if c.HistoricPath == "" {
		return nil
	}

	absPath, err := filepath.Abs(c.HistoricPath)
	if err != nil {
		return fmt.Errorf("failed to resolve historic path: %w", err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return fmt.Errorf("failed to create historic path: %w", err)
	}

	return nil
}
