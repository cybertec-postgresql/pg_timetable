package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChainStart_WorkerRequired verifies that `chain start` without --worker
// returns errWorkerRequired immediately, before any DB connection is made
// (REQ-005, REQ-006 / AC-002b).
func TestChainStart_WorkerRequired(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"chain", "start", "42"}) // no --worker, no connstring
	err := root.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, errWorkerRequired)
}

// TestChainStop_WorkerRequired mirrors the above for `chain stop`.
func TestChainStop_WorkerRequired(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"chain", "stop", "42"})
	err := root.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, errWorkerRequired)
}

// TestChainDelete_NonTTYFailSafe verifies that `chain delete` without --yes on a
// non-interactive stream fails safe without contacting the DB (AC-008 / SEC-003).
func TestChainDelete_NonTTYFailSafe(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	root := newRootCmd()
	root.SetArgs([]string{"chain", "delete", "my-chain"}) // no --yes, no --dsn → safe abort
	err = root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aborted")
}
