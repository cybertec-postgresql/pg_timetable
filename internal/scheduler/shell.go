package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
func (sch *Scheduler) ExecuteProgramCommand(ctx context.Context, command string, paramValues []string) (code int, stdout string, stderr error) {

	command = strings.TrimSpace(command)
	if command == "" {
		return -1, "", errors.New("program command cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		paramValues = []string{""}
	}
	for _, val := range paramValues {
		params := []string{}
		if val > "" {
			if err := json.Unmarshal([]byte(val), &params); err != nil {
				return -1, "", err
			}
		}
		out, err := Cmd.CombinedOutput(ctx, command, params...) // #nosec
		cmdLine := fmt.Sprintf("%s %v: ", command, params)
		stdout = strings.TrimSpace(string(out))
		l := sch.l.WithField("command", cmdLine).
			WithField("output", string(out))
		if err != nil {
			//check if we're dealing with an ExitError - i.e. return code other than 0
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode := exitError.ExitCode()
				l.WithField("retcode", exitCode).Debug("Program run", cmdLine, exitCode)
				return exitCode, stdout, exitError
			}
			return -1, stdout, err
		}
		l.WithField("retcode", 0).Debug("Program run")
	}
	return 0, stdout, nil
}
