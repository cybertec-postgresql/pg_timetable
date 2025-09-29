package pgengine_test

import (
	"context"
	"os"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create temporary YAML file
func createTempYamlFile(t *testing.T, content string) string {
	tmpfile, err := os.CreateTemp("", "test-*.yaml")
	require.NoError(t, err)

	_, err = tmpfile.Write([]byte(content))
	require.NoError(t, err)

	err = tmpfile.Close()
	require.NoError(t, err)

	return tmpfile.Name()
}

// Helper function to remove temporary file
func removeTempFile(t *testing.T, filePath string) {
	err := os.Remove(filePath)
	require.NoError(t, err)
}

func TestLoadYamlChainsIntegration(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	pge := container.Engine

	t.Run("Single task chain", func(t *testing.T) {
		// Create a simple YAML chain config
		yamlContent := `chains:
  - name: test-single-task
    schedule: "0 0 * * *"
    tasks:
      - command: SELECT 1
        kind: SQL`

		// Create temporary YAML file
		tempFile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tempFile)

		// Load the chain
		err := pge.LoadYamlChains(ctx, tempFile, false)
		require.NoError(t, err)

		// Verify the chain was created
		var count int
		err = pge.ConfigDb.QueryRow(ctx,
			"SELECT COUNT(*) FROM timetable.chain WHERE chain_name = $1",
			"test-single-task").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("Replace existing chain", func(t *testing.T) {
		chainName := "test-replace-chain"

		// First, create a chain
		yamlContent1 := `chains:
  - name: test-replace-chain
    schedule: "0 0 * * *"
    tasks:
      - command: SELECT 1
        kind: SQL`

		tempFile1 := createTempYamlFile(t, yamlContent1)
		defer removeTempFile(t, tempFile1)

		err := pge.LoadYamlChains(ctx, tempFile1, false)
		require.NoError(t, err)

		// Now replace it with a different chain
		yamlContent2 := `chains:
  - name: test-replace-chain
    schedule: "0 1 * * *"
    tasks:
      - command: SELECT 2  
        kind: SQL`

		tempFile2 := createTempYamlFile(t, yamlContent2)
		defer removeTempFile(t, tempFile2)

		// Should succeed with replace=true
		err = pge.LoadYamlChains(ctx, tempFile2, true)
		require.NoError(t, err)

		// Verify the schedule was updated
		var schedule string
		err = pge.ConfigDb.QueryRow(ctx,
			"SELECT run_at FROM timetable.chain WHERE chain_name = $1",
			chainName).Scan(&schedule)
		require.NoError(t, err)
		assert.Equal(t, "0 1 * * *", schedule)
	})
}

func TestYamlParameterHandling(t *testing.T) {
	// Test parsing and validation of different parameter formats
	yamlContent := `chains:
  - name: "test-parameters"
    schedule: "0 0 * * *"
    tasks:
      - name: "sql-test"
        kind: "SQL"
        command: "SELECT $1, $2, $3"
        parameters:
          - ["value1", 42, true]
          - ["value2", 99, false]
      
      - name: "program-test"
        kind: "PROGRAM"
        command: "echo"
        parameters:
          - ["-n", "hello world"]
          - ["goodbye"]
      
      - name: "sleep-test"
        kind: "BUILTIN"
        command: "Sleep"
        parameters:
          - 5
          - 10
      
      - name: "log-test"
        kind: "BUILTIN"
        command: "Log"
        parameters:
          - "warning message"
          - {"level": "WARNING", "message": "test"}
`

	// Create temporary file with content
	tmpfile, err := os.CreateTemp("", "test-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpfile.Name())

	_, err = tmpfile.Write([]byte(yamlContent))
	require.NoError(t, err)
	err = tmpfile.Close()
	require.NoError(t, err)

	// Parse the YAML
	yamlConfig, err := pgengine.ParseYamlFile(tmpfile.Name())
	require.NoError(t, err)

	// Check parsed content
	require.Equal(t, 1, len(yamlConfig.Chains))
	chain := yamlConfig.Chains[0]
	require.Equal(t, "test-parameters", chain.ChainName)
	require.Equal(t, 4, len(chain.Tasks))

	// Check SQL task parameters
	sqlTask := chain.Tasks[0]
	require.Equal(t, "SQL", sqlTask.Kind)
	require.Equal(t, 2, len(sqlTask.Parameters))
	sqlParam1, ok := sqlTask.Parameters[0].([]interface{})
	require.True(t, ok, "SQL parameter should be an array")
	assert.Equal(t, 3, len(sqlParam1))

	// Check PROGRAM task parameters
	programTask := chain.Tasks[1]
	require.Equal(t, "PROGRAM", programTask.Kind)
	require.Equal(t, 2, len(programTask.Parameters))

	// Check BUILTIN Sleep task parameters
	sleepTask := chain.Tasks[2]
	require.Equal(t, "BUILTIN", sleepTask.Kind)
	require.Equal(t, "Sleep", sleepTask.Command)
	require.Equal(t, 2, len(sleepTask.Parameters))
	sleepParam1, ok := sleepTask.Parameters[0].(int)
	require.True(t, ok, "Sleep parameter should be an integer")
	assert.Equal(t, 5, sleepParam1)

	// Check BUILTIN Log task parameters
	logTask := chain.Tasks[3]
	require.Equal(t, "BUILTIN", logTask.Kind)
	require.Equal(t, "Log", logTask.Command)
	require.Equal(t, 2, len(logTask.Parameters))
	logParam1, ok := logTask.Parameters[0].(string)
	require.True(t, ok, "Log parameter can be a string")
	assert.Equal(t, "warning message", logParam1)
}
