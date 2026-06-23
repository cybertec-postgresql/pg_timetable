package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/spf13/cobra"
)

// newExportCmd implements `export <chain-id|name>... [-f out.yaml]` (REQ-010 / AC-006).
func newExportCmd() *cobra.Command {
	var outFile string
	cmd := &cobra.Command{
		Use:   "export <chain-id|name>...",
		Short: "Export chains to YAML (best-effort static snapshot)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				data, warnings, err := c.ExportYAML(ctx, args)
				if err != nil {
					return err
				}
				for _, w := range warnings {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
				}
				if outFile != "" {
					if err := os.WriteFile(outFile, data, 0o644); err != nil {
						return fmt.Errorf("writing %q: %w", outFile, err)
					}
					cmd.Printf("OK: exported %d chain(s) to %q\n", len(args), outFile)
				} else {
					cmd.Print(string(data))
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&outFile, "file", "f", "", "output file (default: stdout)")
	return cmd
}
