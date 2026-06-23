package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/spf13/cobra"
)

// errWorkerRequired is returned when --worker is omitted for start/stop.
var errWorkerRequired = errors.New("--worker is required: the NOTIFY channel is named after the worker's client_name")

// newChainCmd is the parent for chain-related subcommands.
func newChainCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "chain",
		Short: "Inspect and manage chains",
	}
	c.AddCommand(newChainListCmd())
	c.AddCommand(newChainShowCmd())
	c.AddCommand(newChainStartCmd())
	c.AddCommand(newChainStopCmd())
	c.AddCommand(newChainPauseCmd())
	c.AddCommand(newChainResumeCmd())
	c.AddCommand(newChainCreateCmd())
	c.AddCommand(newChainEditCmd())
	c.AddCommand(newChainDeleteCmd())
	c.AddCommand(newChainTaskCmd())
	c.AddCommand(newChainRunsCmd())
	c.AddCommand(newChainRunDetailCmd())
	return c
}

// newChainRunsCmd implements `chain runs <id|name>` (P5-4 / REQ-012).
func newChainRunsCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "runs <chain-id|name>",
		Short: "Show recent execution runs for a chain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				runs, err := c.ListRuns(ctx, args[0], limit)
				if err != nil {
					return err
				}
				headers := []string{"TXID", "STARTED", "FINISHED", "DURATION_MS", "STATUS", "WORKER", "TASKS", "FAILED"}
				rows := make([][]string, 0, len(runs))
				for _, r := range runs {
					rows = append(rows, []string{
						strconv.FormatInt(r.Txid, 10),
						r.StartedAt,
						r.FinishedAt,
						strconv.FormatInt(r.DurationMS, 10),
						r.Status,
						r.ClientName,
						strconv.Itoa(r.TotalTasks),
						strconv.Itoa(r.FailedTasks),
					})
				}
				return render(cmd.OutOrStdout(), runs, headers, rows)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "number of recent runs to show")
	return cmd
}

// newChainRunDetailCmd implements `chain run-detail <txid>` (P5-5 / REQ-012).
func newChainRunDetailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run-detail <txid>",
		Short: "Show per-task detail for a single chain run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			txid, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("txid must be a number: %w", err)
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				tasks, err := c.ShowRun(ctx, txid)
				if err != nil {
					return err
				}
				if len(tasks) == 0 {
					return fmt.Errorf("no execution log found for txid %d", txid)
				}
				headers := []string{"TASK_ID", "KIND", "STARTED", "FINISHED", "MS", "RC", "IGN", "PARAMS", "OUTPUT", "COMMAND"}
				rows := make([][]string, 0, len(tasks))
				for _, t := range tasks {
					rows = append(rows, []string{
						strconv.FormatInt(t.TaskID, 10),
						t.Kind,
						t.StartedAt,
						t.FinishedAt,
						strconv.FormatInt(t.DurationMS, 10),
						strconv.Itoa(t.Returncode),
						boolStr(t.IgnoreError),
						t.Params,
						t.Output,
						t.Command,
					})
				}
				return render(cmd.OutOrStdout(), tasks, headers, rows)
			})
		},
	}
}

// newChainCreateCmd implements `chain create` (REQ-004).
func newChainCreateCmd() *cobra.Command {
	var spec client.ChainSpec
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new chain with one initial task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				id, err := c.CreateChain(ctx, spec)
				if err != nil {
					return err
				}
				cmd.Printf("OK: created chain %q with id %d\n", spec.Name, id)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&spec.Name, "name", "", "chain name (required)")
	f.StringVar(&spec.Schedule, "schedule", "", "cron schedule, e.g. '* * * * *' (required)")
	f.StringVar(&spec.Command, "command", "", "initial task command (required)")
	f.StringVar(&spec.Kind, "kind", "SQL", "initial task kind: SQL|PROGRAM|BUILTIN")
	f.StringVar(&spec.ClientName, "client", "", "restrict to a specific worker client_name")
	f.IntVar(&spec.MaxInstances, "max-instances", 0, "max parallel instances (0=unlimited)")
	f.BoolVar(&spec.Live, "live", false, "create the chain in live (enabled) state")
	f.BoolVar(&spec.SelfDestruct, "self-destruct", false, "delete the chain after a successful run")
	f.BoolVar(&spec.Exclusive, "exclusive", false, "pause other chains while this runs")
	f.StringVar(&spec.OnError, "on-error", "", "on-error command")
	return cmd
}

// newChainEditCmd implements `chain edit` (REQ-004). Only changed flags apply.
func newChainEditCmd() *cobra.Command {
	var (
		schedule     string
		clientName   string
		maxInstances int
		live         bool
		selfDestruct bool
		exclusive    bool
		onError      string
	)
	cmd := &cobra.Command{
		Use:   "edit <chain-id|name>",
		Short: "Update chain attributes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			f := cmd.Flags()
			var edit client.ChainEdit
			if f.Changed("schedule") {
				edit.Schedule = &schedule
			}
			if f.Changed("client") {
				edit.ClientName = &clientName
			}
			if f.Changed("max-instances") {
				edit.MaxInstances = &maxInstances
			}
			if f.Changed("live") {
				edit.Live = &live
			}
			if f.Changed("self-destruct") {
				edit.SelfDestruct = &selfDestruct
			}
			if f.Changed("exclusive") {
				edit.Exclusive = &exclusive
			}
			if f.Changed("on-error") {
				edit.OnError = &onError
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.EditChain(ctx, ref, edit); err != nil {
					return err
				}
				cmd.Printf("OK: chain %q updated\n", ref)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&schedule, "schedule", "", "cron schedule")
	f.StringVar(&clientName, "client", "", "worker client_name (empty clears)")
	f.IntVar(&maxInstances, "max-instances", 0, "max parallel instances")
	f.BoolVar(&live, "live", false, "live (enabled) state")
	f.BoolVar(&selfDestruct, "self-destruct", false, "self-destruct after success")
	f.BoolVar(&exclusive, "exclusive", false, "exclusive execution")
	f.StringVar(&onError, "on-error", "", "on-error command (empty clears)")
	return cmd
}

// newChainDeleteCmd implements `chain delete` (REQ-004 / SEC-003 / AC-008).
func newChainDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <chain-id|name>",
		Short: "Delete a chain and all its tasks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			if !confirm(cmd, fmt.Sprintf("Delete chain %q and all its tasks?", ref)) {
				return fmt.Errorf("aborted: pass --yes to confirm deletion")
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.DeleteChain(ctx, ref); err != nil {
					return err
				}
				cmd.Printf("OK: chain %q deleted\n", ref)
				return nil
			})
		},
	}
}

// newChainTaskCmd is the parent command for `chain task` subcommands (REQ-004, REQ-008).
func newChainTaskCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks within a chain",
	}
	c.AddCommand(newTaskAddCmd())
	c.AddCommand(newTaskEditCmd())
	c.AddCommand(newTaskDeleteCmd())
	c.AddCommand(newTaskMoveCmd())
	return c
}

// newTaskAddCmd implements `chain task add` (REQ-004).
func newTaskAddCmd() *cobra.Command {
	var spec client.TaskSpec
	cmd := &cobra.Command{
		Use:   "add <chain-id|name>",
		Short: "Add a task to a chain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				id, err := c.AddTask(ctx, args[0], spec)
				if err != nil {
					return err
				}
				cmd.Printf("OK: added task %d to chain %q\n", id, args[0])
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&spec.Name, "name", "", "task name")
	f.StringVar(&spec.Command, "command", "", "task command (required)")
	f.StringVar(&spec.Kind, "kind", "SQL", "SQL|PROGRAM|BUILTIN")
	f.StringVar(&spec.RunAs, "run-as", "", "role to SET ROLE before execution")
	f.StringVar(&spec.ConnectStr, "connect", "", "remote database connection string")
	f.BoolVar(&spec.IgnoreError, "ignore-error", false, "continue chain on task failure")
	f.BoolVar(&spec.Autonomous, "autonomous", false, "run outside chain transaction")
	f.IntVar(&spec.Timeout, "timeout", 0, "abort task after this many milliseconds")
	return cmd
}

// newTaskEditCmd implements `chain task edit` (REQ-004). Only changed flags apply.
func newTaskEditCmd() *cobra.Command {
	var (
		name        string
		kind        string
		command     string
		runAs       string
		connect     string
		ignoreError bool
		autonomous  bool
		timeout     int
	)
	cmd := &cobra.Command{
		Use:   "edit <task-id>",
		Short: "Update task attributes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("task-id must be a number: %w", err)
			}
			f := cmd.Flags()
			var edit client.TaskEdit
			if f.Changed("name") {
				edit.Name = &name
			}
			if f.Changed("kind") {
				edit.Kind = &kind
			}
			if f.Changed("command") {
				edit.Command = &command
			}
			if f.Changed("run-as") {
				edit.RunAs = &runAs
			}
			if f.Changed("connect") {
				edit.ConnectStr = &connect
			}
			if f.Changed("ignore-error") {
				edit.IgnoreError = &ignoreError
			}
			if f.Changed("autonomous") {
				edit.Autonomous = &autonomous
			}
			if f.Changed("timeout") {
				edit.Timeout = &timeout
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.EditTask(ctx, taskID, edit); err != nil {
					return err
				}
				cmd.Printf("OK: task %d updated\n", taskID)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "task name")
	f.StringVar(&kind, "kind", "", "SQL|PROGRAM|BUILTIN")
	f.StringVar(&command, "command", "", "task command")
	f.StringVar(&runAs, "run-as", "", "role")
	f.StringVar(&connect, "connect", "", "remote connection string (empty clears)")
	f.BoolVar(&ignoreError, "ignore-error", false, "ignore errors")
	f.BoolVar(&autonomous, "autonomous", false, "autonomous execution")
	f.IntVar(&timeout, "timeout", 0, "timeout ms")
	return cmd
}

// newTaskDeleteCmd implements `chain task delete` (REQ-004 / SEC-003).
func newTaskDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <task-id>",
		Short: "Delete a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("task-id must be a number: %w", err)
			}
			if !confirm(cmd, fmt.Sprintf("Delete task %d?", taskID)) {
				return fmt.Errorf("aborted: pass --yes to confirm deletion")
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.DeleteTask(ctx, taskID); err != nil {
					return err
				}
				cmd.Printf("OK: task %d deleted\n", taskID)
				return nil
			})
		},
	}
}

// newTaskMoveCmd implements `chain task move` (REQ-008).
func newTaskMoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "move <task-id> {up|down}",
		Short: "Reorder a task within its chain",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("task-id must be a number: %w", err)
			}
			var up bool
			switch args[1] {
			case "up":
				up = true
			case "down":
				up = false
			default:
				return fmt.Errorf("direction must be 'up' or 'down'")
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.MoveTask(ctx, taskID, up); err != nil {
					return err
				}
				cmd.Printf("OK: task %d moved %s\n", taskID, args[1])
				return nil
			})
		},
	}
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// newChainListCmd implements `chain list` (REQ-002 / AC-001 / P5-3).
func newChainListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [connstring]",
		Short: "List chains with last-run summary",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, args, func(ctx context.Context, c client.Client) error {
				chains, err := c.ListChains(ctx)
				if err != nil {
					return err
				}
				headers := []string{"ID", "NAME", "RUN_AT", "LIVE", "ACTIVE", "CLIENT",
					"LAST_RUN", "DURATION_MS", "RC", "STATUS", "LAST_WORKER"}
				rows := make([][]string, 0, len(chains))
				for _, ch := range chains {
					rows = append(rows, []string{
						strconv.Itoa(ch.ChainID),
						ch.ChainName,
						ch.RunAt,
						boolStr(ch.Live),
						boolStr(ch.Active),
						ch.ClientName,
						ch.LastRun,
						strconv.FormatInt(ch.LastDurationMS, 10),
						strconv.Itoa(ch.LastReturncode),
						ch.LastStatus,
						ch.LastWorker,
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

// newChainStartCmd implements `chain start` (REQ-005 / AC-002, AC-002b).
// --worker is REQUIRED: the NOTIFY channel is named after the worker client_name.
// The chain runs exactly once regardless of its live flag (P0-1 decision).
func newChainStartCmd() *cobra.Command {
	var worker string
	var delay int

	cmd := &cobra.Command{
		Use:   "start <chain-id>",
		Short: "Trigger a one-shot run of a chain (ignores live flag)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if worker == "" {
				return errWorkerRequired
			}
			chainID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("chain-id must be a number: %w", err)
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.StartChain(ctx, chainID, worker, delay); err != nil {
					return err
				}
				cmd.Printf("OK: START signal sent to worker %q for chain %d\n", worker, chainID)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&worker, "worker", "", "target scheduler client_name (required)")
	cmd.Flags().IntVar(&delay, "delay", 0, "delay in seconds before the chain starts")
	return cmd
}

// newChainStopCmd implements `chain stop` (REQ-006 / AC-003, AC-002b).
func newChainStopCmd() *cobra.Command {
	var worker string

	cmd := &cobra.Command{
		Use:   "stop <chain-id>",
		Short: "Cancel a currently running chain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if worker == "" {
				return errWorkerRequired
			}
			chainID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("chain-id must be a number: %w", err)
			}
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.StopChain(ctx, chainID, worker); err != nil {
					return err
				}
				cmd.Printf("OK: STOP signal sent to worker %q for chain %d\n", worker, chainID)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&worker, "worker", "", "target scheduler client_name (required)")
	return cmd
}

// newChainPauseCmd implements `chain pause` (REQ-007 / AC-004).
func newChainPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <chain-id|name>",
		Short: "Pause a chain (sets live=false)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.PauseChain(ctx, ref); err != nil {
					return err
				}
				cmd.Printf("OK: chain %q paused (live=false)\n", ref)
				return nil
			})
		},
	}
}

// newChainResumeCmd implements `chain resume` (REQ-007).
func newChainResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <chain-id|name>",
		Short: "Resume a chain (sets live=true)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			return withClient(cmd, nil, func(ctx context.Context, c client.Client) error {
				if err := c.ResumeChain(ctx, ref); err != nil {
					return err
				}
				cmd.Printf("OK: chain %q resumed (live=true)\n", ref)
				return nil
			})
		},
	}
}
