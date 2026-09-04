package config

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quietLog(t *testing.T) *logrus.Logger {
	t.Helper()
	log := logrus.New()
	log.SetOutput(io.Discard)
	return log
}

// The storage config was the one loader that still decoded loosely, and the
// shipped files disagreed with the struct: they said `username`, the struct read
// `user`, so every run silently connected as the default postgres user. Nothing
// caught it because the tests only ever exercised the struct's own key names.
func TestShippedStorageConfigsLoad(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "config", "storage", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no shipped storage configs found")

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := LoadStorageConfig(path, quietLog(t))
			require.NoError(t, err)

			assert.NotEmpty(t, cfg.PostgreSQL.User, "username must reach the connection string")
			assert.NotEmpty(t, cfg.PostgreSQL.Database)
			assert.Positive(t, cfg.PostgreSQL.Port)
			assert.Contains(t, cfg.PostgreSQL.ConnectionString(), "user="+cfg.PostgreSQL.User)
			require.NoError(t, cfg.PostgreSQL.Validate())
		})
	}
}

func TestLoadStorageConfigRejectsUnknownKey(t *testing.T) {
	path := writeTemp(t, "storage.yaml", `
historic_path: "./historic"
enable_historic: true
postgresql:
  host: "localhost"
  usernmae: "typo"
`)

	_, err := LoadStorageConfig(path, quietLog(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usernmae")
}

func TestLoadStorageConfigReadsPoolSizes(t *testing.T) {
	path := writeTemp(t, "storage.yaml", `
enable_historic: true
postgresql:
  username: "bench"
  max_connections: 25
  max_idle_connections: 7
`)

	cfg, err := LoadStorageConfig(path, quietLog(t))
	require.NoError(t, err)
	assert.Equal(t, "bench", cfg.PostgreSQL.User)
	assert.Equal(t, 25, cfg.PostgreSQL.MaxOpenConns)
	assert.Equal(t, 7, cfg.PostgreSQL.MaxIdleConns)
}
