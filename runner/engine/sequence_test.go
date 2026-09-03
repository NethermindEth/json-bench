package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jsonrpc-bench/runner/config"
	"github.com/jsonrpc-bench/runner/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadTestdataConfig(t *testing.T, name string) *config.Config {
	t.Helper()
	registry := config.NewClientRegistry()
	require.NoError(t, registry.LoadFromConfig(types.ClientsConfig{
		Clients: []types.ClientConfig{{Name: "stub_node", URL: "http://127.0.0.1:18545"}},
	}))
	cfg, err := config.NewConfigLoader(registry).LoadTestConfig(filepath.Join("testdata", name))
	require.NoError(t, err)
	return cfg
}

// Pre-generated request CSVs are how separate runs on separate hosts replay the
// same traffic, so committed corpora must keep matching what a given seed
// produces. The golden was captured from the k6-era generator before it was
// removed; a change to the drawing order or the payload encoding breaks every
// existing corpus and has to fail here.
func TestSequenceMatchesTheCapturedGolden(t *testing.T) {
	cfg := loadTestdataConfig(t, "sequence.yaml")

	requests, err := BuildSequence(cfg)
	require.NoError(t, err)

	var rendered bytes.Buffer
	require.NoError(t, WriteSequenceCSV(&rendered, requests))

	golden, err := os.ReadFile(filepath.Join("testdata", "sequence-seed424242.csv"))
	require.NoError(t, err)

	assert.Equal(t, string(golden), rendered.String())
}

func TestSequenceIsReproducibleAcrossRuns(t *testing.T) {
	cfg := loadTestdataConfig(t, "sequence.yaml")

	first, err := BuildSequence(cfg)
	require.NoError(t, err)
	second, err := BuildSequence(cfg)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestSequenceVariesWithoutASeed(t *testing.T) {
	cfg := loadTestdataConfig(t, "sequence.yaml")
	cfg.Seed = 0

	first, err := BuildSequence(cfg)
	require.NoError(t, err)
	second, err := BuildSequence(cfg)
	require.NoError(t, err)

	assert.NotEqual(t, first, second, "an unseeded config must not silently replay one sequence")
}

func TestSequenceHonoursWeights(t *testing.T) {
	cfg := loadTestdataConfig(t, "sequence.yaml")

	requests, err := BuildSequence(cfg)
	require.NoError(t, err)

	counts := map[string]int{}
	for _, req := range requests {
		counts[req.Name]++
	}

	// Weights are 10/30/60 over 220 draws; the bands are wide enough to be
	// stable for the committed seed while still catching an inverted mapping.
	assert.Greater(t, counts["call"], counts["getbalance"])
	assert.Greater(t, counts["getbalance"], counts["blocknumber"])
	assert.InDelta(t, 0.60, float64(counts["call"])/float64(len(requests)), 0.08)
	assert.InDelta(t, 0.30, float64(counts["getbalance"])/float64(len(requests)), 0.08)
	assert.InDelta(t, 0.10, float64(counts["blocknumber"])/float64(len(requests)), 0.08)
}

func TestSequenceLengthCoversTheRun(t *testing.T) {
	cfg := loadTestdataConfig(t, "sequence.yaml")

	requests, err := BuildSequence(cfg)
	require.NoError(t, err)

	// 20 rps for 10s needs 200 requests, and the sequence carries 10% more so a
	// generator running slightly ahead of schedule cannot exhaust it. The count
	// is 221 rather than 220 because 20*10*1.1 is 220.00000000000003 in float64
	// and the length is a ceiling. Committed corpora were generated with that
	// arithmetic, so it is reproduced rather than corrected.
	assert.Equal(t, 221, len(requests))
}

func TestSequenceCSVRoundTrip(t *testing.T) {
	cfg := loadTestdataConfig(t, "sequence.yaml")

	requests, err := BuildSequence(cfg)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "requests.csv")
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, WriteSequenceCSV(file, requests))
	require.NoError(t, file.Close())

	loaded, err := LoadSequenceCSV(path)
	require.NoError(t, err)
	assert.Equal(t, requests, loaded)
}

func TestLoadSequenceCSVRejectsBadRows(t *testing.T) {
	for name, body := range map[string]string{
		"wrong column count": "1,name,eth_call\n",
		"non-numeric id":     "x,name,eth_call,{}\n",
		"empty method":       "1,name,,{}\n",
		"empty file":         "",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "requests.csv")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			_, err := LoadSequenceCSV(path)
			assert.Error(t, err)
		})
	}
}
