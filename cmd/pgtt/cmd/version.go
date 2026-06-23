package cmd

import (
	"github.com/spf13/cobra"
)

// newVersionCmd prints pgtt build and compatible-schema version info.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Printf("pgtt:\n")
			cmd.Printf("  Version:    %s\n", version)
			cmd.Printf("  DB Schema:  %s\n", dbSchema)
			cmd.Printf("  Git Commit: %s\n", commit)
			cmd.Printf("  Built:      %s\n", date)
			return nil
		},
	}
}
