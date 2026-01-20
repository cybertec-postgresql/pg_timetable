package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

type commander interface {
	CombinedOutput(context.Context, string, ...string) ([]byte, error)
}

type realCommander struct{}

// CombinedOutput executes program command and returns combined stdout and stderr
func (c realCommander) CombinedOutput(ctx context.Context, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdin = nil
	return cmd.CombinedOutput()
}

// Cmd executes a command
var Cmd commander = realCommander{}

// ExecuteProgramCommand executes program command and returns status code, output and error if any
func (sch *Scheduler) ExecuteProgramCommand(ctx context.Context, task *pgengine.ChainTask, paramValues []string) error {
	var err error
	var exitCode int
	command := strings.TrimSpace(task.Command)
	if command == "" {
		return errors.New("program command cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		paramValues = []string{""}
	}
	for _, val := range paramValues {
		exitCode = 0
		params := []string{}
		if val > "" {
			if err := json.Unmarshal([]byte(val), &params); err != nil {
				return err
			}
		}
		out, e := Cmd.CombinedOutput(ctx, command, params...) // #nosec
		if e != nil {
			exitCode = -1
			err = errors.Join(err, e) // accumulate errors for all param sets
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			}
		}
		sch.pgengine.LogTaskExecution(context.Background(), task, exitCode, string(out), val)
	}
	return err
}
