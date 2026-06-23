package cmd

import (
	"context"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/spf13/cobra"
)

// newCheckCmd connects and verifies schema compatibility (REQ-016 / AC-009).
// It is also the simplest end-to-end exercise of the Phase 1 connection path.
func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [connstring]",
		Short: "Verify connection and timetable schema compatibility",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, args, func(_ context.Context, _ client.Client) error {
				cmd.Printf("OK: connected; timetable schema %s is compatible\n", dbSchema)
				return nil
			})
		},
	}
}
