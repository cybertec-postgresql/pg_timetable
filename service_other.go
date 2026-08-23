//go:build !windows

package main

import (
	"errors"
	"fmt"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
)

// handleServiceCommand is a stub for platforms without Windows services.
func handleServiceCommand(*config.CmdOptions) int {
	fmt.Println("Service management:", errors.New("only supported on Windows"))
	return ExitCodeFatalError
}
