package capture

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoreWritesEachDigestOnceGzipped(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, 8)
	require.NoError(t, err)
	s.Put("abc", []string{"x", "y"})
	s.Put("abc", []string{"ignored"})
	require.NoError(t, s.Close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	f, err := os.Open(filepath.Join(dir, "abc.json.gz"))
	require.NoError(t, err)
	defer f.Close()
	zr, err := gzip.NewReader(f)
	require.NoError(t, err)
	var got []string
	require.NoError(t, json.NewDecoder(zr).Decode(&got))
	require.Equal(t, []string{"x", "y"}, got)
}
