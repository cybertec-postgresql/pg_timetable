package pgengine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/stretchr/testify/require"
)

// SQL and PROGRAM parameters are execution sets, not a flat argument list.
// Check the shipped examples without running their programs or connecting to a DB.
func TestYamlExampleParameters(t *testing.T) {
	check := func(t *testing.T, path string) {
		t.Helper()
		cfg, err := pgengine.ParseYamlFile(path)
		require.NoError(t, err)
		for _, chain := range cfg.Chains {
			for _, task := range chain.Tasks {
				for _, param := range task.Parameters {
					value, err := json.Marshal(param)
					require.NoError(t, err)
					switch task.Kind {
					case "SQL":
						var args []any
						require.NoError(t, json.Unmarshal(value, &args), "%s / %s", chain.ChainName, task.TaskName)
					case "PROGRAM":
						var args []string
						require.NoError(t, json.Unmarshal(value, &args), "%s / %s", chain.ChainName, task.TaskName)
					}
				}
			}
		}
	}

	paths, err := filepath.Glob("../../samples/yaml/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) { check(t, path) })
	}

	blocks := regexp.MustCompile("(?s)```yaml\\r?\\n(chains:.*?)```")
	for _, path := range []string{"../../README.md", "../../docs/yaml-format.md"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			text, err := os.ReadFile(path)
			require.NoError(t, err)
			examples := blocks.FindAllSubmatch(text, -1)
			require.NotEmpty(t, examples)
			for _, example := range examples {
				path := filepath.Join(t.TempDir(), "example.yaml")
				require.NoError(t, os.WriteFile(path, example[1], 0600))
				check(t, path)
			}
		})
	}
}
