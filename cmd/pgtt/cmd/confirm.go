package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// confirm asks the user to confirm a destructive action (SEC-003 / AC-008).
//
// Behavior:
//   - if --yes was given, returns true without prompting;
//   - if stdin is not a TTY (e.g. running over SSH in a pipe / CI), it fails
//     safe: returns false without prompting (REQ-014 / AC-008);
//   - otherwise prompts on the command's output and reads a yes/no answer.
func confirm(cmd *cobra.Command, prompt string) bool {
	if opts.assume {
		return true
	}
	if !isInteractive() {
		return false
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N]: ", prompt)
	return readYes(cmd.InOrStdin())
}

func readYes(r io.Reader) bool {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// isInteractive reports whether stdin is attached to a terminal.
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
