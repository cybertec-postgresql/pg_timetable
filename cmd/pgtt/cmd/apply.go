package cmd

import (
	"context"
	"fmt"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/spf13/cobra"
)

// newApplyCmd implements `apply <file.yaml> [--replace]` (REQ-009 / AC-005).
func newApplyCmd() *cobra.Command {
	var replace bool
	cmd := &cobra.Command{
		Use:   "apply <file.yaml>",
		Short: "Import chains from a YAML file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if replace && !confirm(cmd, fmt.Sprintf("Replace existing chains from %q?", path)) {
				return fmt.Errorf("aborted: pass --yes to confirm replace")
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				n, err := c.ApplyYAML(ctx, path, replace)
				if err != nil {
					return err
				}
				cmd.Printf("OK: imported %d chain(s) from %q\n", n, path)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&replace, "replace", false, "replace existing chains with the same name")
	return cmd
}
