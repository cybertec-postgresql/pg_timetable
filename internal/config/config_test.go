package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	os.Args = []string{0: "config_test", "--config=../../config.example.yaml"}
	_, err := NewConfig(nil)
	assert.NoError(t, err)

	os.Args = []string{0: "config_test", "--unknown"}
	_, err = NewConfig(nil)
	assert.Error(t, err)

	os.Args = []string{0: "config_test"} // clientname arg is missing
	_, err = NewConfig(nil)
	assert.Error(t, err)

	os.Args = []string{0: "config_test", "--config=foo.boo.bar.baz.yaml"}
	_, err = NewConfig(nil)
	assert.Error(t, err)
}
