package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/spf13/cobra"
)

// newSessionCmd implements `session list` (REQ-011).
func newSessionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "session",
		Short: "Inspect active scheduler sessions",
	}
	c.AddCommand(&cobra.Command{
		Use:   "list [connstring]",
		Short: "List active scheduler sessions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, args, func(ctx context.Context, cl client.Client) error {
				sessions, err := cl.ListSessions(ctx)
				if err != nil {
					return err
				}
				headers := []string{"CLIENT", "CLIENT_PID", "SERVER_PID", "STARTED_AT"}
				rows := make([][]string, 0, len(sessions))
				for _, s := range sessions {
					rows = append(rows, []string{
						s.ClientName,
						strconv.FormatInt(s.ClientPID, 10),
						strconv.FormatInt(s.ServerPID, 10),
						s.StartedAt,
					})
				}
				return render(cmd.OutOrStdout(), sessions, headers, rows)
			})
		},
	})
	return c
}

// newActiveCmd implements `active list` (REQ-011).
func newActiveCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "active",
		Short: "Inspect currently running chains",
	}
	c.AddCommand(&cobra.Command{
		Use:   "list [connstring]",
		Short: "List currently running chains",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, args, func(ctx context.Context, cl client.Client) error {
				active, err := cl.ListActiveChains(ctx)
				if err != nil {
					return err
				}
				headers := []string{"CHAIN_ID", "CLIENT", "STARTED_AT"}
				rows := make([][]string, 0, len(active))
				for _, a := range active {
					rows = append(rows, []string{
						strconv.Itoa(a.ChainID),
						a.ClientName,
						a.StartedAt,
					})
				}
				return render(cmd.OutOrStdout(), active, headers, rows)
			})
		},
	})
	return c
}

// newLogCmd implements the `log` command group (REQ-012, REQ-013).
func newLogCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "log",
		Short: "Query and stream scheduler activity",
	}
	c.AddCommand(newLogListCmd())
	c.AddCommand(newLogTailCmd())
	c.AddCommand(newLogDiagCmd())
	return c
}

// newLogListCmd implements `log list` — unified activity feed from both
// timetable.log and timetable.execution_log (REQ-012).
func newLogListCmd() *cobra.Command {
	var (
		chainID    int
		clientName string
		limit      int
	)
	list := &cobra.Command{
		Use:   "list [connstring]",
		Short: "List recent activity (execution results + scheduler messages)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, args, func(ctx context.Context, cl client.Client) error {
				entries, err := cl.ListActivity(ctx, client.LogFilter{
					ChainID:    chainID,
					ClientName: clientName,
					Limit:      limit,
				})
				if err != nil {
					return err
				}
				headers := []string{"TS", "SRC", "LEVEL", "CHAIN", "TASK", "MS", "RC", "CLIENT", "MESSAGE"}
				rows := make([][]string, 0, len(entries))
				for _, e := range entries {
					chainStr := ""
					if e.ChainID > 0 {
						chainStr = strconv.FormatInt(e.ChainID, 10)
					}
					taskStr := ""
					if e.TaskID > 0 {
						taskStr = strconv.FormatInt(e.TaskID, 10)
					}
					rows = append(rows, []string{
						e.TS, e.Source, e.Level,
						chainStr, taskStr,
						strconv.FormatInt(e.DurationMS, 10),
						strconv.Itoa(e.Returncode),
						e.ClientName, e.Message,
					})
				}
				return render(cmd.OutOrStdout(), entries, headers, rows)
			})
		},
	}
	list.Flags().IntVar(&chainID, "chain", 0, "filter by chain id")
	list.Flags().StringVar(&clientName, "client", "", "filter by client name")
	list.Flags().IntVar(&limit, "limit", 100, "maximum entries")
	return list
}

// newLogTailCmd implements `log tail` — live unified activity stream (REQ-013).
func newLogTailCmd() *cobra.Command {
	var (
		chainID    int
		clientName string
	)
	tail := &cobra.Command{
		Use:   "tail [connstring]",
		Short: "Stream activity live: execution results + scheduler messages (Ctrl-C to stop)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "# pgtt log tail — press Ctrl-C to stop")
			fmt.Fprintf(cmd.OutOrStdout(), "%-26s  %-4s  %-7s  %-6s  %-6s  %-7s  %-20s  %s\n",
				"TS", "SRC", "LEVEL", "CHAIN", "TASK", "MS", "CLIENT", "MESSAGE/OUTPUT")
			return withClient(cmd, args, func(ctx context.Context, cl client.Client) error {
				return cl.TailActivity(ctx, client.LogFilter{
					ChainID:    chainID,
					ClientName: clientName,
				}, func(e client.ActivityEntry) {
					chainStr := ""
					if e.ChainID > 0 {
						chainStr = strconv.FormatInt(e.ChainID, 10)
					}
					taskStr := ""
					if e.TaskID > 0 {
						taskStr = strconv.FormatInt(e.TaskID, 10)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%-26s  %-4s  %-7s  %-6s  %-6s  %-7d  %-20s  %s\n",
						e.TS, e.Source, e.Level,
						chainStr, taskStr, e.DurationMS,
						e.ClientName, e.Message)
				})
			})
		},
	}
	tail.Flags().IntVar(&chainID, "chain", 0, "filter by chain id")
	tail.Flags().StringVar(&clientName, "client", "", "filter by client name")
	return tail
}

// newLogDiagCmd implements `log diag` — raw timetable.log scheduler diagnostics
// only, for debugging scheduler internals.
func newLogDiagCmd() *cobra.Command {
	var (
		chainID    int
		clientName string
		limit      int
	)
	diag := &cobra.Command{
		Use:   "diag [connstring]",
		Short: "List raw scheduler diagnostic messages (timetable.log only)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, args, func(ctx context.Context, cl client.Client) error {
				entries, err := cl.ListLogs(ctx, client.LogFilter{
					ChainID:    chainID,
					ClientName: clientName,
					Limit:      limit,
				})
				if err != nil {
					return err
				}
				headers := []string{"TS", "LEVEL", "CLIENT", "PID", "MESSAGE"}
				rows := make([][]string, 0, len(entries))
				for _, e := range entries {
					rows = append(rows, []string{
						e.TS, e.LogLevel, e.ClientName,
						strconv.Itoa(e.PID), e.Message,
					})
				}
				return render(cmd.OutOrStdout(), entries, headers, rows)
			})
		},
	}
	diag.Flags().IntVar(&chainID, "chain", 0, "filter by chain id")
	diag.Flags().StringVar(&clientName, "client", "", "filter by client name")
	diag.Flags().IntVar(&limit, "limit", 100, "maximum entries")
	return diag
}
