// Command pgtt is a CLI tool to manage pg_timetable chains and tasks.
//
// See spec/spec-tool-pgtt-cli.md and spec/plan-pgtt-cli.md for the design.
// This file is the Phase 0 scaffold: it wires the cobra root command and
// builds into a standalone binary (CON-001, CON-005). Functional commands are
// added in later phases.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cmd.Execute(ctx))
}
