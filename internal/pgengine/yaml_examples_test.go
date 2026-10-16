package pgengine_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/stretchr/testify/require"
)

// SQL and PROGRAM parameters are execution sets, not a flat argument list.
func TestYamlSampleParameters(t *testing.T) {
	paths, err := filepath.Glob("../../samples/yaml/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := pgengine.ParseYamlFile(path)
			require.NoError(t, err)
			for _, chain := range cfg.Chains {
				for _, task := range chain.Tasks {
					kind := strings.ToUpper(task.Kind)
					if kind == "BUILTIN" {
						continue
					}
					for _, param := range task.Parameters {
						args, ok := param.([]any)
						require.True(t, ok, "%s / %s: parameter %v is not an array", chain.ChainName, task.TaskName, param)
						if kind == "PROGRAM" {
							for _, arg := range args {
								require.IsType(t, "", arg, "%s / %s", chain.ChainName, task.TaskName)
							}
						}
					}
				}
			}
		})
	}
}
