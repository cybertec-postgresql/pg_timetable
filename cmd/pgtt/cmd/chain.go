package cmd

import (
	"context"
	"strconv"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/spf13/cobra"
)

// newChainCmd is the parent for chain-related subcommands.
func newChainCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "chain",
		Short: "Inspect and manage chains",
	}
	c.AddCommand(newChainListCmd())
	c.AddCommand(newChainShowCmd())
	return c
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// newChainListCmd implements `chain list` (REQ-002 / AC-001).
func newChainListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [connstring]",
		Short: "List chains",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, args, func(ctx context.Context, c client.Client) error {
				chains, err := c.ListChains(ctx)
				if err != nil {
					return err
				}
				headers := []string{"ID", "NAME", "RUN_AT", "LIVE", "CLIENT", "ACTIVE", "LAST_STATUS"}
				rows := make([][]string, 0, len(chains))
				for _, ch := range chains {
					rows = append(rows, []string{
						strconv.Itoa(ch.ChainID),
						ch.ChainName,
						ch.RunAt,
						boolStr(ch.Live),
						ch.ClientName,
						boolStr(ch.Active),
						ch.LastStatus,
					})
				}
				return render(cmd.OutOrStdout(), chains, headers, rows)
			})
		},
	}
}

// chainDetail is the JSON shape for `chain show`.
type chainDetail struct {
	Chain client.ChainListItem `json:"chain"`
	Tasks []client.ChainTask   `json:"tasks"`
}

// newChainShowCmd implements `chain show` (REQ-003).
func newChainShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <chain-id|name>",
		Short: "Show a chain and its tasks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				chain, tasks, err := c.ShowChain(ctx, ref)
				if err != nil {
					return err
				}
				detail := chainDetail{Chain: *chain, Tasks: tasks}
				headers := []string{"ORDER", "TASK_ID", "NAME", "KIND", "COMMAND", "IGNORE_ERR", "AUTONOMOUS", "CONN", "TIMEOUT"}
				rows := make([][]string, 0, len(tasks))
				for i, t := range tasks {
					rows = append(rows, []string{
						strconv.Itoa(i + 1),
						strconv.Itoa(t.TaskID),
						t.TaskName,
						t.Kind,
						t.Command,
						boolStr(t.IgnoreError),
						boolStr(t.Autonomous),
						t.ConnectString,
						strconv.Itoa(t.Timeout),
					})
				}
				return render(cmd.OutOrStdout(), detail, headers, rows)
			})
		},
	}
}
