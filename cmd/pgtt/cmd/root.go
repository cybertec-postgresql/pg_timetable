// Package cmd holds the cobra command tree for the pgtt CLI.
//
// Phase 0 scaffold (see spec/plan-pgtt-cli.md):
//   - root command with global flags (CON-004, REQ-014, REQ-015)
//   - version subcommand
//
// Connection handling, the internal Client layer, and functional subcommands
// are introduced in Phase 1+.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/tui"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Build-time version information. Override with -ldflags, e.g.:
//
//	go build -ldflags "-X .../cmd/pgtt/cmd.version=1.2.3" ./cmd/pgtt
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	// dbSchema is the pg_timetable DB schema version pgtt is compatible with.
	// Used by the Phase 1 schema-version check (REQ-016 / AC-009).
	dbSchema = "00733"
)

// globalOptions holds flags shared by all subcommands.
type globalOptions struct {
	dsn     string // PostgreSQL connection string (positional or --dsn)
	output  string // "text" | "table" | "json" (REQ-015)
	assume  bool   // --yes, skip confirmations (SEC-003)
	config  string // pgtt config file (viper)
	verbose bool
	noColor bool          // --no-color, disable ANSI output (also honors NO_COLOR env)
	refresh time.Duration // --refresh, TUI auto-refresh interval (bare `pgtt`)
}

var opts globalOptions

// newRootCmd builds the root command tree.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pgtt",
		Short: "Manage pg_timetable chains and tasks",
		Long: "pgtt is a command-line management tool for pg_timetable.\n" +
			"It connects directly to PostgreSQL and treats the timetable.* schema\n" +
			"as the single source of truth. See spec/spec-tool-pgtt-cli.md.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Bare `pgtt` (no subcommand) launches the TUI. Subcommands keep their
		// own RunE and never reach this. Accept an optional positional DSN.
		Args: cobra.MaximumNArgs(1),
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return initConfig(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return launchTUI(cmd, args)
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&opts.dsn, "dsn", "", "PostgreSQL connection string (may also be given as a positional arg)")
	pf.StringVarP(&opts.output, "output", "o", "table", "output format: text|tree|table|json")
	pf.BoolVar(&opts.assume, "yes", false, "skip confirmation prompts for destructive operations")
	pf.StringVar(&opts.config, "config", "", "pgtt config file")
	pf.BoolVarP(&opts.verbose, "verbose", "v", false, "verbose logging")
	pf.BoolVar(&opts.noColor, "no-color", false, "disable colored output (also honors the NO_COLOR env var)")
	pf.DurationVar(&opts.refresh, "refresh", 5*time.Second, "TUI auto-refresh interval (bare `pgtt` launches the TUI)")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newChainCmd())
	root.AddCommand(newSessionCmd())
	root.AddCommand(newActiveCmd())
	root.AddCommand(newLogCmd())
	root.AddCommand(newApplyCmd())
	root.AddCommand(newExportCmd())
	return root
}

// resolveDSN picks the connection string from, in order of precedence:
// --dsn flag, first positional arg, PGTT_CONNSTR env, else "" so that libpq
// environment variables (PGHOST, PGUSER, ...) are used by pgx.
func resolveDSN(args []string) string {
	if opts.dsn != "" {
		return opts.dsn
	}
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	if v := os.Getenv("PGTT_CONNSTR"); v != "" {
		return v
	}
	return ""
}

// withClient validates global flags, connects (without creating the schema),
// verifies schema compatibility (REQ-016), then runs fn with a ready Client.
func withClient(cmd *cobra.Command, args []string, fn func(context.Context, client.Client) error) error {
	if _, err := parseOutputFormat(opts.output); err != nil {
		return err
	}
	ctx := cmd.Context()
	c := client.New(dbSchema)
	if err := c.Connect(ctx, resolveDSN(args)); err != nil {
		return err
	}
	defer c.Close()
	if err := c.CheckSchemaVersion(ctx); err != nil {
		switch {
		case errors.Is(err, client.ErrSchemaAbsent):
			return fmt.Errorf("%w; run a pg_timetable instance against this database first", err)
		default:
			return err
		}
	}
	return fn(ctx, c)
}

// launchTUI connects (without creating the schema), verifies schema
// compatibility, then runs the interactive TUI for the lifetime of the session.
// Unlike withClient, the client stays open until the TUI exits. Bare `pgtt`
// (no subcommand) routes here.
func launchTUI(cmd *cobra.Command, args []string) error {
	// The TUI requires an interactive terminal on stdout.
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		return errors.New("pgtt with no subcommand starts an interactive TUI, " +
			"which requires a terminal; run `pgtt --help` for non-interactive commands")
	}
	ctx := cmd.Context()
	c := client.New(dbSchema)
	dsn := resolveDSN(args)
	if err := c.Connect(ctx, dsn); err != nil {
		return err
	}
	defer c.Close()
	if err := c.CheckSchemaVersion(ctx); err != nil {
		if errors.Is(err, client.ErrSchemaAbsent) {
			return fmt.Errorf("%w; run a pg_timetable instance against this database first", err)
		}
		return err
	}
	return tui.Run(ctx, c, tui.Options{
		Refresh: opts.refresh,
		Host:    tuiTarget(dsn),
		NoColor: opts.noColor || os.Getenv("NO_COLOR") != "",
	})
}

// tuiTarget returns a short, password-free connection label for the TUI header.
// It never includes credentials (SEC-002). An empty dsn means libpq env vars.
func tuiTarget(dsn string) string {
	if dsn == "" {
		return ""
	}
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil || cfg.Host == "" {
		return ""
	}
	if cfg.Database != "" {
		return fmt.Sprintf("%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)
	}
	return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
}

// initConfig wires viper precedence: flags > env (PGTT_*) > config file.
func initConfig(cmd *cobra.Command) error {
	v := viper.New()
	v.SetEnvPrefix("PGTT")
	v.AutomaticEnv()
	if opts.config != "" {
		v.SetConfigFile(opts.config)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config %q: %w", opts.config, err)
		}
	}
	return v.BindPFlags(cmd.Flags())
}

// Execute runs the root command and returns a process exit code.
func Execute(ctx context.Context) int {
	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		// Write to stderr, not stdout. Error is already redacted by the client
		// layer (redactDSNError) so passwords are not present (SEC-002 / AC-010).
		fmt.Fprintln(root.ErrOrStderr(), "Error:", err)
		return 1
	}
	return 0
}
