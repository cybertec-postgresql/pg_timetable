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

// overwrite CombinedOutput function of os/exec so only parameter syntax and return codes are checked...
func (c testCommander) CombinedOutput(command string, args ...string) ([]byte, error) {
	if strings.HasPrefix(command, "ping") {
		return []byte(fmt.Sprint(command, args)), nil
	}
	return []byte(fmt.Sprint(command, args)), &exec.Error{Name: command, Err: exec.ErrNotFound}
}

func TestShellCommand(t *testing.T) {
	cmd = testCommander{}
	var err error
	var retCode int

	err, retCode = executeShellCommand("", []string{""})
	assert.EqualError(t, err, "Shell command cannot be empty", "Empty command should fail")

	err, retCode = 	executeShellCommand("ping0", nil)
	assert.NoError(t, err, "Command with nil param is OK")

	err, retCode = 	executeShellCommand("ping1", []string{})
	assert.NoError(t, err, "Command with empty array param is OK")

	err, retCode = 	executeShellCommand("ping2", []string{""})
	assert.NoError(t, err, "Command with empty string param is OK")

	err, retCode = 	executeShellCommand("ping3", []string{"[]"})
	assert.NoError(t, err, "Command with empty json array param is OK")

	err, retCode = 	executeShellCommand("ping3", []string{"[null]"})
	assert.NoError(t, err, "Command with nil array param is OK")

	err, retCode = executeShellCommand("ping4", []string{`["localhost"]`})
	assert.NoError(t, err, "Command with one param is OK")

	err, retCode = 	executeShellCommand("ping5", []string{`["localhost", "-4"]`})
	assert.NoError(t, err, "Command with many params is OK")

	err, retCode = 	executeShellCommand("pong", nil)
	assert.IsType(t, (*exec.Error)(nil), err, "Uknown command should produce error")

	err, retCode = 	executeShellCommand("ping5", []string{`{"param1": "localhost"}`})
	assert.IsType(t, (*json.UnmarshalTypeError)(nil), err, "Command should fail with mailformed json parameter")
	assert.NotEqual(t, 0, retCode, "return code should indicate failure.")

	//to make the tests below work, it is needed to remove the reimplementation of the CombinedOutput function above.
	// err, retCode = 	executeShellCommand("/bin/true", nil)
	// assert.Equal(t, 0, retCode, "/bin/true should have 0 return code")

	// err, retCode = 	executeShellCommand("/bin/false", nil)
	// assert.Equal(t, 1, retCode, "/bin/false should have 1 return code")
	// assert.IsType(t, (*exec.ExitError)(nil), err, "/bin/false should produce ExitError")
}
