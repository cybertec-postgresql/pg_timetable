package pgengine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBootstrapSQLFileExists(t *testing.T) {
	require.FileExists(t, "../../sql/"+SQLSchemaFile, "Bootstrap file doesn't exist")
}
