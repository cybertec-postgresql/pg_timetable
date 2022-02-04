package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFail(t *testing.T) {
	tests := [][]string{
		{0: "go-test", "--unknown-oprion"},
		{0: "go-test", "-c", "client01", "-f", "foo"},
	}
	for _, d := range tests {
		os.Args = d
		_, err := Parse(nil)
		assert.Error(t, err)
	}
}

func TestParseSuccess(t *testing.T) {
	tests := [][]string{
		{0: "go-test", "non-optional-value"},
	}
	for _, d := range tests {
		os.Args = d
		_, err := Parse(nil)
		assert.NoError(t, err)
	}
}

func TestLogLevel(t *testing.T) {
	c := &CmdOptions{Logging: LoggingOpts{LogLevel: "debug"}}
	assert.True(t, c.Verbose())
	c = &CmdOptions{Logging: LoggingOpts{LogLevel: "info"}}
	assert.False(t, c.Verbose())
}

func TestVersionOnly(t *testing.T) {
	c := &CmdOptions{Version: true}
	os.Args = []string{0: "go-test", "-v"}
	assert.True(t, c.VersionOnly())
	c = &CmdOptions{Version: false}
	assert.False(t, c.VersionOnly())
}

func TestNewCmdOptions(t *testing.T) {
	c := NewCmdOptions("-c", "config_unit_test", "--password=somestrong")
	assert.NotNil(t, c)
}
