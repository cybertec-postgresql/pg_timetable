package scheduler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/scheduler"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
)

type testCommander struct{}

// overwrite CombinedOutput function of os/exec so only parameter syntax and return codes are checked...
func (c testCommander) CombinedOutput(_ context.Context, command string, args ...string) ([]byte, error) {
	if strings.HasPrefix(command, "ping") {
		return []byte(fmt.Sprint(command, args)), nil
	}
	return []byte(fmt.Sprintf("Command %s not found", command)), &exec.Error{Name: command, Err: exec.ErrNotFound}
}

func TestShellCommand(t *testing.T) {
	scheduler.Cmd = testCommander{}
	var err error
	var out string
	var retCode int

	mock, err := pgxmock.NewPool() //
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "scheduler_unit_test")
	scheduler := scheduler.New(pge, log.Init(config.LoggingOpts{LogLevel: "error"}))
	ctx := context.Background()

	_, _, err = scheduler.ExecuteProgramCommand(ctx, "", []string{""})
	assert.EqualError(t, err, "program command cannot be empty", "Empty command should out, fail")

	_, out, err = scheduler.ExecuteProgramCommand(ctx, "ping0", nil)
	assert.NoError(t, err, "Command with nil param is out, OK")
	assert.True(t, strings.HasPrefix(string(out), "ping0"), "Output should containt only command ")

	_, _, err = scheduler.ExecuteProgramCommand(ctx, "ping1", []string{})
	assert.NoError(t, err, "Command with empty array param is OK")

	_, _, err = scheduler.ExecuteProgramCommand(ctx, "ping2", []string{""})
	assert.NoError(t, err, "Command with empty string param is OK")

	_, _, err = scheduler.ExecuteProgramCommand(ctx, "ping3", []string{"[]"})
	assert.NoError(t, err, "Command with empty json array param is OK")

	_, _, err = scheduler.ExecuteProgramCommand(ctx, "ping3", []string{"[null]"})
	assert.NoError(t, err, "Command with nil array param is OK")

	_, _, err = scheduler.ExecuteProgramCommand(ctx, "ping4", []string{`["localhost"]`})
	assert.NoError(t, err, "Command with one param is OK")

	_, _, err = scheduler.ExecuteProgramCommand(ctx, "ping5", []string{`["localhost", "-4"]`})
	assert.NoError(t, err, "Command with many params is OK")

	_, _, err = scheduler.ExecuteProgramCommand(ctx, "pong", nil)
	assert.IsType(t, (*exec.Error)(nil), err, "Uknown command should produce error")

	retCode, _, err = scheduler.ExecuteProgramCommand(ctx, "ping5", []string{`{"param1": "localhost"}`})
	assert.IsType(t, (*json.UnmarshalTypeError)(nil), err, "Command should fail with mailformed json parameter")
	assert.NotEqual(t, 0, retCode, "return code should indicate failure.")
}
