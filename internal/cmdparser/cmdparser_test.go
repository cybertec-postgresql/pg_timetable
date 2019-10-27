package cmdparser

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMain(t *testing.T) {
	os.Args = []string{0: "go-test"} //here 0th element stands for application name
	assert.Error(t, Parse(), "Should fail without required parameter -c")
	os.Args = []string{0: "go-test", "-c", "client01", "foo"}
	assert.Error(t, Parse(), "Should fail for unknown parameter")
	os.Args = []string{0: "go-test", "-c", "client01", "http://foo.bar/baz"}
	assert.Error(t, Parse(), "Should fail for invalid URI scheme")
	os.Args = []string{0: "go-test", "-c", "client01", "postgres://user:pwd@host/db"}
	assert.NoError(t, Parse(), "Should not fail for correct URI scheme")
	os.Args = []string{0: "go-test", "-c", "client01", "postgresql://user:pwd@host/db"}
	assert.NoError(t, Parse(), "Should not fail for correct URI scheme")
	os.Args = []string{0: "go-test", "-c", "client01", "-d", "postgres://user:pwd@host/db"}
	assert.NoError(t, Parse(), "Should not fail for correct URI scheme in --dbname")
	os.Args = []string{0: "go-test", "-c", "client01", "--pgurl=postgres://user:pwd@host/db"}
	assert.NoError(t, Parse(), "Should not fail for correct URI scheme")
}
