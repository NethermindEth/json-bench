package comparator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CorpusLoadReportFilename is the name of the load report written beside the
// other compare outputs.
const CorpusLoadReportFilename = "corpus-load-report.json"

// CorpusLoadReportSchemaVersion identifies the shape of that document.
const CorpusLoadReportSchemaVersion = 1

// CorpusIdentitySpec states how a request_id in this document was computed, so
// a reader does not have to infer it from a matching digest.
type CorpusIdentitySpec struct {
	Algorithm     string `json:"algorithm"`
	CanonicalForm string `json:"canonical_form"`
}

// CorpusSampling records the loader inputs that decide which calls survive, so
// a smaller selection than the corpus holds is explained by the document that
// reports it.
type CorpusSampling struct {
	Sample        int    `json:"sample"`
	Seed          int64  `json:"seed"`
	BlockOverride string `json:"block_override,omitempty"`
}

// CorpusTotals is the whole-run accounting. Files split into loaded,
// excluded-only and skipped; entries split into unnamed, excluded and loaded;
// loaded calls split into selected and dropped by sampling. Each split adds up,
// which is what makes a missing call detectable rather than merely unlikely.
//
// EntriesParsed counts every entry read from a file that parsed, including one
// that turned out not to be a corpus file at all. Only a file that failed to
// parse contributes no entries, because there were none to read.
type CorpusTotals struct {
	FilesScanned      int `json:"files_scanned"`
	FilesLoaded       int `json:"files_loaded"`
	FilesExcludedOnly int `json:"files_excluded_only"`
	FilesSkipped      int `json:"files_skipped"`

	EntriesParsed   int `json:"entries_parsed"`
	EntriesUnnamed  int `json:"entries_unnamed"`
	EntriesExcluded int `json:"entries_excluded"`
	EntriesLoaded   int `json:"entries_loaded"`

	CallsSelected      int `json:"calls_selected"`
	CallsSampleDropped int `json:"calls_sample_dropped"`

	IdentityErrors int `json:"identity_errors"`
}

// CorpusRequests enumerates every call the loader saw, by identity: the ones
// handed to the comparator, the ones the method policy dropped, and the ones
// --sample did not choose. A consumer reconciling a run against the manifest
// that produced the corpus needs all three — an omission it cannot see is an
// omission it cannot approve.
type CorpusRequests struct {
	Selected      []CorpusRequest `json:"selected"`
	Excluded      []CorpusRequest `json:"excluded"`
	SampleDropped []CorpusRequest `json:"sample_dropped"`
}

// CorpusLoadReportDocument is the on-disk shape of corpus-load-report.json.
// The log lines remain, but they are no longer the only account of what was
// loaded: a consumer that must fail closed on an unexpected loader skip cannot
// depend on parsing prose.
type CorpusLoadReportDocument struct {
	SchemaVersion int                `json:"schema_version"`
	GeneratedAt   string             `json:"generated_at"`
	CorpusDir     string             `json:"corpus_dir"`
	Identity      CorpusIdentitySpec `json:"identity"`
	Sampling      CorpusSampling     `json:"sampling"`
	Totals        CorpusTotals       `json:"totals"`
	Files         []CorpusFileReport `json:"files"` // every file that parsed
	SkippedFiles  []CorpusSkip       `json:"skipped_files"`
	Requests      CorpusRequests     `json:"requests"`
}

// NewCorpusLoadReportDocument assembles the document from a load report. A nil
// report means the corpus directory could not be read at all; the document is
// still produced, with zero totals, because a consumer that fails closed on a
// missing report should see a corpus of zero files rather than nothing.
func NewCorpusLoadReportDocument(dir string, report *CorpusReport) CorpusLoadReportDocument {
	doc := CorpusLoadReportDocument{
		SchemaVersion: CorpusLoadReportSchemaVersion,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		CorpusDir:     dir,
		Identity: CorpusIdentitySpec{
			Algorithm:     "sha256",
			CanonicalForm: `["<method>",<params>] as compact JSON with sorted object keys, absent params as [], UTF-8`,
		},
		Files:        []CorpusFileReport{},
		SkippedFiles: []CorpusSkip{},
		Requests: CorpusRequests{
			Selected:      []CorpusRequest{},
			Excluded:      []CorpusRequest{},
			SampleDropped: []CorpusRequest{},
		},
	}
	if report == nil {
		return doc
	}

	doc.Sampling = CorpusSampling{
		Sample:        report.Sample,
		Seed:          report.SampleSeed,
		BlockOverride: report.BlockOverride,
	}
	if report.PerFile != nil {
		doc.Files = report.PerFile
	}
	if report.Skips != nil {
		doc.SkippedFiles = report.Skips
	}
	if report.Selected != nil {
		doc.Requests.Selected = report.Selected
	}
	if report.ExcludedRequests != nil {
		doc.Requests.Excluded = report.ExcludedRequests
	}
	if report.SampleDropped != nil {
		doc.Requests.SampleDropped = report.SampleDropped
	}

	totals := CorpusTotals{
		FilesScanned:       report.FilesScanned,
		FilesLoaded:        report.Files,
		FilesExcludedOnly:  report.Excluded,
		FilesSkipped:       len(report.Skips),
		EntriesLoaded:      report.Entries,
		CallsSelected:      len(doc.Requests.Selected),
		CallsSampleDropped: len(doc.Requests.SampleDropped),
	}
	for _, file := range doc.Files {
		totals.EntriesParsed += file.Entries
		totals.EntriesUnnamed += file.Unnamed
		totals.EntriesExcluded += file.Excluded
	}
	for _, bucket := range [][]CorpusRequest{doc.Requests.Selected, doc.Requests.Excluded, doc.Requests.SampleDropped} {
		for _, request := range bucket {
			if request.IdentityError != "" {
				totals.IdentityErrors++
			}
		}
	}
	doc.Totals = totals
	return doc
}

// SaveCorpusLoadReport writes the load report to dir/corpus-load-report.json.
func SaveCorpusLoadReport(outputDir, corpusDir string, report *CorpusReport) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	data, err := json.MarshalIndent(NewCorpusLoadReportDocument(corpusDir, report), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal corpus load report: %w", err)
	}
	path := filepath.Join(outputDir, CorpusLoadReportFilename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write corpus load report: %w", err)
	}
	return nil
}
