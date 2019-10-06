package scheduler

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

type commander interface {
	CombinedOutput(string, ...string) ([]byte, error)
}

type realCommander struct{}

func (c realCommander) CombinedOutput(command string, args ...string) ([]byte, error) {
	return exec.Command(command, args...).CombinedOutput()
}

var cmd commander

// ExecuteTask executes built-in task depending on task name and returns err result
func executeShellCommand(command string, paramValues []string) (int, error) {
	if strings.TrimSpace(command) == "" {
		return -1, errors.New("Shell command cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		paramValues = []string{""}
	}
	for _, val := range paramValues {
		params := []string{}
		if val > "" {
			if err := json.Unmarshal([]byte(val), &params); err != nil {
				return -1, err
			}
		}
		out, err := cmd.CombinedOutput(command, params...) // #nosec
		pgengine.LogToDB("LOG", "Output of the shell command for command:\n", command, params, "\n", string(out))
		if err != nil {
			//check if we're dealing with an ExitError - i.e. return code other than 0
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode := exitError.ProcessState.ExitCode()
				pgengine.LogToDB("DEBUG", "Return value of the shell command:\n", command, params, "\n", exitCode)
				return exitCode, exitError
			}
			return -1, err
		}
	}
	return 0, nil
}

func init() {
	cmd = realCommander{}
}
