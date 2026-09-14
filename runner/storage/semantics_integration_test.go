package storage

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/types"
)

// testPostgresDSNEnv names the connection string for a throwaway database.
// Without it these tests skip, so `go test ./...` passes on a machine with no
// Postgres instead of failing on a refused connection.
const testPostgresDSNEnv = "BENCH_TEST_POSTGRES_DSN"

func semanticsTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv(testPostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to a scratch database to run this test", testPostgresDSNEnv)
	}

	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))
	require.NoError(t, RunMigrations(db, logrus.New()))

	return db
}

func insertSemanticsRun(t *testing.T, database *Database, id, semantics string) {
	t.Helper()
	require.NoError(t, database.InsertRun(&types.HistoricRun{
		ID:                 id,
		Timestamp:          time.Now(),
		TestName:           "semantics-gate",
		ConfigHash:         "hash",
		Duration:           "1m",
		TotalRequests:      100,
		SuccessRate:        99,
		AvgLatency:         10,
		P95Latency:         20,
		ErrorRateSemantics: semantics,
	}))
	t.Cleanup(func() {
		_, _ = database.db.Exec(`DELETE FROM benchmark_runs WHERE id = $1`, id)
	})
}

// The column has to survive the round trip. A field added to the struct but not
// to the SQL is silently dropped, which is how full_results came to be computed
// on every run and never stored.
func TestErrorRateSemanticsRoundTrip(t *testing.T) {
	db := semanticsTestDB(t)
	database := &Database{db: db, log: logrus.New()}

	insertSemanticsRun(t, database, "semantics-rpc-aware", types.ErrorRateSemanticsRPCAware)

	got, err := database.GetRun("semantics-rpc-aware")
	require.NoError(t, err)
	assert.Equal(t, types.ErrorRateSemanticsRPCAware, got.ErrorRateSemantics)

	runs, err := database.ListRuns(types.RunFilter{TestName: "semantics-gate", Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, runs)
	for _, run := range runs {
		if run.ID == "semantics-rpc-aware" {
			assert.Equal(t, types.ErrorRateSemanticsRPCAware, run.ErrorRateSemantics,
				"the listing must carry it too, or a caller cannot tell before comparing")
			return
		}
	}
	t.Fatal("the inserted run was not listed")
}

// A run that recorded nothing must read back as unrecorded rather than as an
// empty-but-known value, so the comparison reports itself unverified.
func TestUnrecordedSemanticsReadsBackEmpty(t *testing.T) {
	db := semanticsTestDB(t)
	database := &Database{db: db, log: logrus.New()}

	insertSemanticsRun(t, database, "semantics-absent", "")

	got, err := database.GetRun("semantics-absent")
	require.NoError(t, err)
	assert.Empty(t, got.ErrorRateSemantics)

	verdict := types.CompareSemantics(got.ErrorRateSemantics, types.ErrorRateSemanticsRPCAware)
	assert.True(t, verdict.Comparable)
	assert.False(t, verdict.Verified)
}

// The whole point: a comparison across the change in what the error rate counts
// must be refused rather than reported as a regression.
func TestCompareRunsRefusesMismatchedSemantics(t *testing.T) {
	db := semanticsTestDB(t)
	database := &Database{db: db, log: logrus.New()}

	insertSemanticsRun(t, database, "semantics-old", types.ErrorRateSemanticsHTTPOnly)
	insertSemanticsRun(t, database, "semantics-new", types.ErrorRateSemanticsRPCAware)

	storage := &HistoricStorage{db: database, log: logrus.New()}

	comparison, err := storage.CompareRuns(context.Background(), "semantics-old", "semantics-new")
	require.NoError(t, err, "a refusal is a verdict, not a failure to answer")
	require.NotNil(t, comparison)

	assert.False(t, comparison.Comparability.Comparable)
	assert.True(t, comparison.Comparability.Verified)
	assert.Empty(t, comparison.Regressions, "no regression may be derived from an incomparable pair")
	assert.Empty(t, comparison.Improvements)
	assert.Contains(t, comparison.Summary, "Refused")
}

func TestCompareRunsProceedsWhenSemanticsMatch(t *testing.T) {
	db := semanticsTestDB(t)
	database := &Database{db: db, log: logrus.New()}

	insertSemanticsRun(t, database, "semantics-a", types.ErrorRateSemanticsRPCAware)
	insertSemanticsRun(t, database, "semantics-b", types.ErrorRateSemanticsRPCAware)

	storage := &HistoricStorage{db: database, log: logrus.New()}

	comparison, err := storage.CompareRuns(context.Background(), "semantics-a", "semantics-b")
	require.NoError(t, err)
	require.NotNil(t, comparison)

	assert.True(t, comparison.Comparability.Comparable)
	assert.True(t, comparison.Comparability.Verified)
	assert.NotContains(t, comparison.Summary, "Refused")
}
