package cmd

import (
	"context"
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

// newLogCmd implements `log list` (REQ-012). `log tail` is added in Phase 5.
func newLogCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "log",
		Short: "Query scheduler logs",
	}

	var (
		chainID    int
		clientName string
		limit      int
	)
	list := &cobra.Command{
		Use:   "list [connstring]",
		Short: "List log entries",
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
						e.TS,
						e.LogLevel,
						e.ClientName,
						strconv.Itoa(e.PID),
						e.Message,
					})
				}
				return render(cmd.OutOrStdout(), entries, headers, rows)
			})
		},
	}
	list.Flags().IntVar(&chainID, "chain", 0, "filter by chain id")
	list.Flags().StringVar(&clientName, "client", "", "filter by client name")
	list.Flags().IntVar(&limit, "limit", 100, "maximum number of entries")
	c.AddCommand(list)
	return c
}
