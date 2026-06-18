package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/storage"
)

func loadClientRegistry(clientsPath string) (*config.ClientRegistry, error) {
	registry := config.NewClientRegistry()
	if clientsPath == "" {
		return registry, nil
	}
	if err := registry.LoadFromFile(clientsPath); err != nil {
		return nil, fmt.Errorf("failed to load clients configuration: %w", err)
	}
	logger.Info("Loaded clients configuration from ", clientsPath)
	return registry, nil
}

func loadBenchmarkConfig(configPath, clientsPath string, registry *config.ClientRegistry) (*config.Config, error) {
	loader := config.NewConfigLoader(registry)
	if clientsPath != "" {
		return loader.LoadTestConfig(configPath)
	}
	return loader.LoadWithBackwardCompatibility(configPath)
}

func openHistoricStorage(storageConfigPath string) (*storage.HistoricStorage, *sql.DB, error) {
	storageCfg, err := config.LoadStorageConfig(storageConfigPath, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load storage configuration: %w", err)
	}
	if !storageCfg.EnableHistoric {
		return nil, nil, fmt.Errorf("historic storage must be enabled in %s", storageConfigPath)
	}

	db, err := sql.Open("postgres", storageCfg.PostgreSQL.ConnectionString())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to postgres database: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to ping postgres database: %w", err)
	}

	if err := storage.RunMigrations(db, logger); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	historic, err := storage.NewHistoricStorage(storageCfg, logger)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to create historic storage: %w", err)
	}
	return historic, db, nil
}
