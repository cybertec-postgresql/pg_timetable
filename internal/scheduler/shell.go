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
func executeShellCommand(command string, paramValues []string) error {
	if strings.TrimSpace(command) == "" {
		return errors.New("Shell command cannot be empty")
	}
	if len(paramValues) == 0 { //mimic empty param
		paramValues = []string{""}
	}
	for _, val := range paramValues {
		params := []string{}
		if val > "" {
			if err := json.Unmarshal([]byte(val), &params); err != nil {
				return err
			}
		}
		out, err := cmd.CombinedOutput(command, params...) // #nosec
		pgengine.LogToDB("LOG", "Output of the shell command for command:\n", command, params, "\n", string(out))
		if err != nil {
			return err
		}
	}
	return nil
}

func init() {
	cmd = realCommander{}
}
