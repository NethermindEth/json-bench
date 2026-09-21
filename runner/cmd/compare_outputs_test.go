package cmd

import (
	"path/filepath"
	"testing"

	"github.com/jsonrpc-bench/runner/comparator"
)

// compare writes four files, and the names are part of its interface: a
// consumer that has to fail closed on a missing corpus load report needs the
// name to be stable, not discovered.
func TestCompareOutputNames(t *testing.T) {
	if comparator.CorpusLoadReportFilename != "corpus-load-report.json" {
		t.Errorf("the load report's name changed: %q", comparator.CorpusLoadReportFilename)
	}

	original := outputDir
	t.Cleanup(func() { outputDir = original })

	outputDir = filepath.Join("some", "run")
	want := filepath.Join("some", "run", "corpus-load-report.json")
	if got := corpusLoadReportPath(); got != want {
		t.Errorf("corpusLoadReportPath() = %q, want %q", got, want)
	}
}
