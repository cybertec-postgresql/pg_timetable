package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfirm_AssumeYes verifies --yes bypasses prompting (SEC-003).
func TestConfirm_AssumeYes(t *testing.T) {
	t.Cleanup(func() { opts.assume = false })
	opts.assume = true

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	assert.True(t, confirm(cmd, "delete chain?"))
	assert.Empty(t, out.String(), "should not prompt when --yes is set")
}

// TestConfirm_NonTTYFailsSafe verifies that without --yes and without a TTY the
// destructive action is refused without prompting (REQ-014 / AC-008).
//
// stdin is replaced with a pipe (never a character device) to guarantee a
// non-interactive environment regardless of how the tests are launched.
func TestConfirm_NonTTYFailsSafe(t *testing.T) {
	t.Cleanup(func() { opts.assume = false })
	opts.assume = false

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	assert.False(t, isInteractive(), "piped stdin must be non-interactive")
	assert.False(t, confirm(cmd, "delete chain?"))
}
