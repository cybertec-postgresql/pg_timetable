package scheduler

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testCommander struct{}

func (c testCommander) CombinedOutput(command string, args ...string) ([]byte, error) {
	if strings.HasPrefix(command, "ping") {
		return []byte(fmt.Sprint(command, args)), nil
	}
	return []byte(fmt.Sprint(command, args)), &exec.Error{Name: command, Err: exec.ErrNotFound}
}

func TestShellCommand(t *testing.T) {
	cmd = testCommander{}
	assert.EqualError(t, executeShellCommand("", []string{""}), "Shell command cannot be empty",
		"Empty command should fail")
	assert.NoError(t, executeShellCommand("ping0", nil),
		"Command with nil param is OK")
	assert.NoError(t, executeShellCommand("ping1", []string{}),
		"Command with empty array param is OK")
	assert.NoError(t, executeShellCommand("ping2", []string{""}),
		"Command with empty string param is OK")
	assert.NoError(t, executeShellCommand("ping3", []string{"[]"}),
		"Command with empty json array param is OK")
	assert.NoError(t, executeShellCommand("ping3", []string{"[null]"}),
		"Command with nil array param is OK")
	assert.NoError(t, executeShellCommand("ping4", []string{`["localhost"]`}),
		"Command with one param is OK")
	assert.NoError(t, executeShellCommand("ping5", []string{`["localhost", "-4"]`}),
		"Command with many params is OK")
	assert.IsType(t, (*exec.Error)(nil), executeShellCommand("pong", nil),
		"Uknown command should produce error")
	assert.IsType(t, (*json.UnmarshalTypeError)(nil), executeShellCommand("ping5", []string{`{"param1": "localhost"}`}),
		"Command should fail with mailformed json parameter")
}
