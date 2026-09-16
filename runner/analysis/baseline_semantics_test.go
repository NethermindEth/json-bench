package analysis

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jsonrpc-bench/runner/storage"
	"github.com/jsonrpc-bench/runner/types"
)

// A baseline outlives the run it points at, and a comparison against one whose
// run has been deleted has to degrade to unverified rather than fail: the rows
// in the baselines table reference historic_runs, so a retention job takes the
// run and leaves the baseline.
func TestSemanticsOfADeletedBaselineRunIsUnknownRatherThanFatal(t *testing.T) {
	semantics, missing, err := semanticsOfBaselineRun(nil,
		fmt.Errorf("%w: run-2026-01-01", storage.ErrRunNotFound))

	require.NoError(t, err, "a deleted run must not fail the comparison")
	assert.True(t, missing, "the caller has to be able to say why it could not check")
	assert.Empty(t, semantics)

	// Unknown semantics are comparable but unverified, which is what makes the
	// degradation safe: no regression is derived from a semantics change that
	// was never recorded either way.
	verdict := types.CompareSemantics(semantics, types.ErrorRateSemanticsRPCAware)
	assert.True(t, verdict.Comparable)
	assert.False(t, verdict.Verified)
}

// An unreachable database is not evidence that the baseline measured something
// else, so it stays an error rather than being read as unknown semantics.
func TestALookupFailureIsStillAFailure(t *testing.T) {
	brokenDB := errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")

	_, missing, err := semanticsOfBaselineRun(nil, brokenDB)

	require.ErrorIs(t, err, brokenDB)
	assert.False(t, missing)
}

func TestARecordedSemanticsIsRead(t *testing.T) {
	semantics, missing, err := semanticsOfBaselineRun(
		&types.HistoricRun{ErrorRateSemantics: types.ErrorRateSemanticsHTTPOnly}, nil)

	require.NoError(t, err)
	assert.False(t, missing)
	assert.Equal(t, types.ErrorRateSemanticsHTTPOnly, semantics)
}
