package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/freshness/mocknode"
	"github.com/jsonrpc-bench/runner/freshness/probe"
	"github.com/jsonrpc-bench/runner/freshness/review"
	"github.com/jsonrpc-bench/runner/freshness/schema"
)

func TestFreshnessExampleConfigsLoad(t *testing.T) {
	t.Setenv("EL_RPC_URL", "http://localhost:8545")
	t.Setenv("REFERENCE_RPC_URL", "http://reference:8545")
	pc, err := probe.LoadConfig("../../config/freshness/probe.example.yaml")
	require.NoError(t, err)
	require.NoError(t, pc.Validate())
	require.Equal(t, []string{schema.ProbeStateNumber, schema.ProbeLogsNumber}, pc.EnabledProbes())

	rc, err := review.LoadConfig("../../config/freshness/review.example.yaml")
	require.NoError(t, err)
	require.NoError(t, rc.Validate(false))
}

func TestFreshnessCLI(t *testing.T) {
	ref := mocknode.New(1, 8, 1)
	node := ref.Peer()
	defer node.Close()
	defer ref.Close()

	tmp := t.TempDir()
	probeCfg := filepath.Join(tmp, "probe.yaml")
	require.NoError(t, os.WriteFile(probeCfg, []byte(`
pair:
  id: cli-pair
  host_id: cli-host
  el:
    client_ref: mock
chain:
  slot_duration_seconds: 1
block_count: 2
poll_interval_ms: 2
lookahead_poll_interval_ms: 10
head_watch_interval_ms: 50
request_timeout_ms: 500
rtt_samples: 2
clock:
  mode: budget
  error_budget_ms: 1
`), 0o644))
	clients := filepath.Join(tmp, "clients.yaml")
	require.NoError(t, os.WriteFile(clients, []byte("clients:\n  - name: mock\n    url: "+node.URL()+"\n    headers:\n      X-Secret: s3cr3t-token\n"), 0o644))

	probeOut := filepath.Join(tmp, "probes")
	stdout := captureStdout(t, func() {
		done := make(chan error, 1)
		go func() {
			rootCmd.SetArgs([]string{"freshness", "probe", "--config", probeCfg, "--clients", clients, "--output", probeOut, "--log-level", "warn"})
			_, err := rootCmd.ExecuteC()
			done <- err
		}()
		require.Eventually(t, func() bool { return node.Calls("eth_getLogs") > 3 }, 10*time.Second, 10*time.Millisecond)
		for i := 0; i < 2; i++ {
			b := ref.Mine(2, 1, 1)
			time.Sleep(30 * time.Millisecond)
			node.Publish(b.Number, mocknode.All())
			ref.Publish(b.Number, mocknode.All())
			time.Sleep(30 * time.Millisecond)
		}
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(20 * time.Second):
			t.Fatal("probe command did not finish")
		}
	})
	probeDir := strings.TrimSpace(stdout)
	require.DirExists(t, probeDir)

	err := filepath.Walk(probeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContains(t, string(b), "s3cr3t-token", "%s leaks a credential", path)
		return nil
	})
	require.NoError(t, err)

	reviewCfg := filepath.Join(tmp, "review.yaml")
	reviewOut := filepath.Join(tmp, "review")
	require.NoError(t, os.WriteFile(reviewCfg, []byte("reference:\n  rpc_url: "+ref.URL()+"\nprobes:\n  - "+probeDir+"\noutput_directory: "+reviewOut+"\n"), 0o644))
	out := rootCmd.PersistentFlags().Lookup("output")
	require.NoError(t, out.Value.Set(out.DefValue))
	out.Changed = false
	stdout = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"freshness", "review", "--config", reviewCfg, "--log-level", "warn"})
		_, err := rootCmd.ExecuteC()
		require.NoError(t, err)
	})
	require.Equal(t, filepath.Join(reviewOut, schema.ReportFile), strings.TrimSpace(stdout))
	report, err := os.ReadFile(filepath.Join(reviewOut, schema.ReportFile))
	require.NoError(t, err)
	require.Contains(t, string(report), "| cli-pair |")
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}
