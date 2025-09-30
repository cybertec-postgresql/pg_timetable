package pgengine_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
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
	sqlParam1, ok := sqlTask.Parameters[0].([]any)
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

func TestParseYamlFile(t *testing.T) {
	t.Run("Valid YAML file", func(t *testing.T) {
		yamlContent := `chains:
  - name: "test-chain"
    schedule: "0 * * * *"
    live: true
    max_instances: 2
    timeout: 300
    self_destruct: true
    exclusive: true
    client_name: "test-client"
    on_error: "RETRY"
    tasks:
      - name: "test-task"
        kind: "SQL"
        command: "SELECT 1"
        parameters: ["param1", 42, true]
        ignore_error: false
        autonomous: false
        timeout: 60
        run_as: "postgres"
        connect_string: "test-db"`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		config, err := pgengine.ParseYamlFile(tmpfile)
		require.NoError(t, err)
		require.Len(t, config.Chains, 1)

		chain := config.Chains[0]
		assert.Equal(t, "test-chain", chain.ChainName)
		assert.Equal(t, "0 * * * *", chain.Schedule)
		assert.True(t, chain.Live)
		assert.Equal(t, 2, chain.MaxInstances)
		assert.Equal(t, 300, chain.Timeout)
		assert.True(t, chain.SelfDestruct)
		assert.True(t, chain.ExclusiveExecution)
		assert.Equal(t, "test-client", chain.ClientName)
		assert.Equal(t, "RETRY", chain.OnError)

		require.Len(t, chain.Tasks, 1)
		task := chain.Tasks[0]
		assert.Equal(t, "test-task", task.TaskName)
		assert.Equal(t, "SQL", task.Kind)
		assert.Equal(t, "SELECT 1", task.Command)
		assert.False(t, task.IgnoreError)
		assert.False(t, task.Autonomous)
		assert.Equal(t, 60, task.Timeout)
		assert.Equal(t, "postgres", task.RunAs)
		assert.Equal(t, "test-db", task.ConnectString)
	})

	t.Run("File not found", func(t *testing.T) {
		_, err := pgengine.ParseYamlFile("/non/existent/file.yaml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file not found")
	})

	t.Run("File cannot be read", func(t *testing.T) {
		_, err := pgengine.ParseYamlFile(".")
		assert.Error(t, err)
	})

	t.Run("Invalid YAML syntax", func(t *testing.T) {
		invalidYaml := `chains:
  - name: "test"
    schedule: "* * * * *"
    tasks:
      - name: "task1"
        kind: "SQL
        command: SELECT 1`

		tmpfile := createTempYamlFile(t, invalidYaml)
		defer removeTempFile(t, tmpfile)

		_, err := pgengine.ParseYamlFile(tmpfile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse YAML")
	})

	t.Run("Validation errors", func(t *testing.T) {
		invalidChain := `chains:
  - name: ""
    schedule: "* * * * *"
    tasks:
      - command: "SELECT 1"`

		tmpfile := createTempYamlFile(t, invalidChain)
		defer removeTempFile(t, tmpfile)

		_, err := pgengine.ParseYamlFile(tmpfile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chain name is required")
	})
}

func TestYamlChainValidation(t *testing.T) {
	t.Run("Valid chain", func(t *testing.T) {
		chain := &pgengine.YamlChain{
			Chain: pgengine.Chain{
				ChainName: "test-chain",
			},
			Schedule: "0 * * * *",
			Tasks: []pgengine.YamlTask{
				{
					ChainTask: pgengine.ChainTask{
						Command: "SELECT 1",
						Kind:    "SQL",
					},
				},
			},
		}

		err := chain.ValidateChain()
		assert.NoError(t, err)
	})

	t.Run("Missing chain name", func(t *testing.T) {
		chain := &pgengine.YamlChain{
			Schedule: "0 * * * *",
			Tasks: []pgengine.YamlTask{
				{
					ChainTask: pgengine.ChainTask{
						Command: "SELECT 1",
						Kind:    "SQL",
					},
				},
			},
		}

		err := chain.ValidateChain()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chain name is required")
	})

	t.Run("Missing schedule", func(t *testing.T) {
		chain := &pgengine.YamlChain{
			Chain: pgengine.Chain{
				ChainName: "test-chain",
			},
			Tasks: []pgengine.YamlTask{
				{
					ChainTask: pgengine.ChainTask{
						Command: "SELECT 1",
						Kind:    "SQL",
					},
				},
			},
		}

		err := chain.ValidateChain()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chain schedule is required")
	})

	t.Run("Invalid cron format", func(t *testing.T) {
		chain := &pgengine.YamlChain{
			Chain: pgengine.Chain{
				ChainName: "test-chain",
			},
			Schedule: "invalid cron",
			Tasks: []pgengine.YamlTask{
				{
					ChainTask: pgengine.ChainTask{
						Command: "SELECT 1",
						Kind:    "SQL",
					},
				},
			},
		}

		err := chain.ValidateChain()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid cron format")
	})

	t.Run("Special schedules", func(t *testing.T) {
		specialSchedules := []string{"@reboot", "@after", "@every"}
		for _, schedule := range specialSchedules {
			chain := &pgengine.YamlChain{
				Chain: pgengine.Chain{
					ChainName: "test-chain",
				},
				Schedule: schedule,
				Tasks: []pgengine.YamlTask{
					{
						ChainTask: pgengine.ChainTask{
							Command: "SELECT 1",
							Kind:    "SQL",
						},
					},
				},
			}

			err := chain.ValidateChain()
			assert.NoError(t, err, "Schedule %s should be valid", schedule)
		}
	})

	t.Run("No tasks", func(t *testing.T) {
		chain := &pgengine.YamlChain{
			Chain: pgengine.Chain{
				ChainName: "test-chain",
			},
			Schedule: "0 * * * *",
			Tasks:    []pgengine.YamlTask{},
		}

		err := chain.ValidateChain()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chain must have at least one task")
	})

	t.Run("Task validation error", func(t *testing.T) {
		chain := &pgengine.YamlChain{
			Chain: pgengine.Chain{
				ChainName: "test-chain",
			},
			Schedule: "0 * * * *",
			Tasks: []pgengine.YamlTask{
				{
					ChainTask: pgengine.ChainTask{
						Command: "", // Invalid empty command
						Kind:    "SQL",
					},
				},
			},
		}

		err := chain.ValidateChain()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task 1:")
		assert.Contains(t, err.Error(), "task command is required")
	})
}

func TestYamlTaskValidation(t *testing.T) {
	t.Run("Valid task", func(t *testing.T) {
		task := &pgengine.YamlTask{
			ChainTask: pgengine.ChainTask{
				Command: "SELECT 1",
				Kind:    "SQL",
				Timeout: 60,
			},
		}

		err := task.ValidateTask()
		assert.NoError(t, err)
	})

	t.Run("Missing command", func(t *testing.T) {
		task := &pgengine.YamlTask{
			ChainTask: pgengine.ChainTask{
				Kind: "SQL",
			},
		}

		err := task.ValidateTask()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task command is required")
	})

	t.Run("Valid kinds", func(t *testing.T) {
		validKinds := []string{"", "SQL", "PROGRAM", "BUILTIN", "sql", "program", "builtin"}
		for _, kind := range validKinds {
			task := &pgengine.YamlTask{
				ChainTask: pgengine.ChainTask{
					Command: "SELECT 1",
					Kind:    kind,
				},
			}

			err := task.ValidateTask()
			assert.NoError(t, err, "Kind %s should be valid", kind)
		}
	})

	t.Run("Invalid kind", func(t *testing.T) {
		task := &pgengine.YamlTask{
			ChainTask: pgengine.ChainTask{
				Command: "SELECT 1",
				Kind:    "INVALID",
			},
		}

		err := task.ValidateTask()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid task kind: INVALID")
	})

	t.Run("Negative timeout", func(t *testing.T) {
		task := &pgengine.YamlTask{
			ChainTask: pgengine.ChainTask{
				Command: "SELECT 1",
				Kind:    "SQL",
				Timeout: -1,
			},
		}

		err := task.ValidateTask()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task timeout must be non-negative")
	})
}

func TestYamlChainSetDefaults(t *testing.T) {
	t.Run("Set default schedule", func(t *testing.T) {
		chain := &pgengine.YamlChain{
			Chain: pgengine.Chain{
				ChainName: "test-chain",
			},
			Tasks: []pgengine.YamlTask{
				{
					ChainTask: pgengine.ChainTask{
						Command: "SELECT 1",
					},
				},
			},
		}

		chain.SetDefaults()
		assert.Equal(t, "* * * * *", chain.Schedule)
		assert.Equal(t, "SQL", chain.Tasks[0].Kind)
	})

	t.Run("Keep existing values", func(t *testing.T) {
		chain := &pgengine.YamlChain{
			Chain: pgengine.Chain{
				ChainName: "test-chain",
			},
			Schedule: "0 0 * * *",
			Tasks: []pgengine.YamlTask{
				{
					ChainTask: pgengine.ChainTask{
						Command: "echo hello",
						Kind:    "PROGRAM",
					},
				},
			},
		}

		chain.SetDefaults()
		assert.Equal(t, "0 0 * * *", chain.Schedule)
		assert.Equal(t, "PROGRAM", chain.Tasks[0].Kind)
	})
}

func TestParameterStorageIntegration(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	pge := container.Engine

	t.Run("Parameters stored as separate rows with correct order_id", func(t *testing.T) {
		yamlContent := `chains:
  - name: "test-parameters"
    schedule: "0 0 * * *"
    tasks:
      - name: "mixed-params"
        kind: "SQL"
        command: "SELECT $1, $2, $3, $4, $5"
        parameters:
          - "hello world"
          - 42
          - 3.14
          - true
          - ["array", "value"]`

		tempFile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tempFile)

		err := pge.LoadYamlChains(ctx, tempFile, false)
		require.NoError(t, err)

		// Get the task ID
		var taskID int64
		err = pge.ConfigDb.QueryRow(ctx,
			"SELECT task_id FROM timetable.task t JOIN timetable.chain c ON t.chain_id = c.chain_id WHERE c.chain_name = $1",
			"test-parameters").Scan(&taskID)
		require.NoError(t, err)

		// Verify parameters are stored as separate rows
		type paramRow struct {
			OrderID int    `db:"order_id"`
			Value   string `db:"value"`
		}

		rows, err := pge.ConfigDb.Query(ctx,
			"SELECT order_id, value::text FROM timetable.parameter WHERE task_id = $1 ORDER BY order_id",
			taskID)
		require.NoError(t, err)

		params, err := pgx.CollectRows(rows, pgx.RowToStructByName[paramRow])
		require.NoError(t, err)

		// Should have 5 parameters
		assert.Equal(t, 5, len(params))

		// Check each parameter
		assert.Equal(t, 1, params[0].OrderID)
		assert.Equal(t, `"hello world"`, params[0].Value)

		assert.Equal(t, 2, params[1].OrderID)
		assert.Equal(t, `42`, params[1].Value)

		assert.Equal(t, 3, params[2].OrderID)
		assert.Equal(t, `3.14`, params[2].Value)

		assert.Equal(t, 4, params[3].OrderID)
		assert.Equal(t, `true`, params[3].Value)

		assert.Equal(t, 5, params[4].OrderID)
		assert.Contains(t, params[4].Value, `["array", "value"]`)
	})

	t.Run("Object parameters stored as JSONB objects", func(t *testing.T) {
		yamlContent := `chains:
  - name: "test-object-params"
    schedule: "0 0 * * *"
    tasks:
      - name: "object-param"
        kind: "BUILTIN"
        command: "Log"
        parameters:
          - {"level": "WARNING", "message": "test", "count": 123}`

		tempFile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tempFile)

		err := pge.LoadYamlChains(ctx, tempFile, false)
		require.NoError(t, err)

		// Get the task ID
		var taskID int64
		err = pge.ConfigDb.QueryRow(ctx,
			"SELECT task_id FROM timetable.task t JOIN timetable.chain c ON t.chain_id = c.chain_id WHERE c.chain_name = $1",
			"test-object-params").Scan(&taskID)
		require.NoError(t, err)

		// Verify object parameter
		var value string
		err = pge.ConfigDb.QueryRow(ctx,
			"SELECT value::text FROM timetable.parameter WHERE task_id = $1 AND order_id = 1",
			taskID).Scan(&value)
		require.NoError(t, err)

		// Should be a valid JSON object
		assert.Contains(t, value, `"level"`)
		assert.Contains(t, value, `"WARNING"`)
		assert.Contains(t, value, `"message"`)
		assert.Contains(t, value, `"test"`)
		assert.Contains(t, value, `"count"`)
		assert.Contains(t, value, `123`)
	})

	t.Run("No parameters creates no parameter rows", func(t *testing.T) {
		yamlContent := `chains:
  - name: "test-no-params"
    schedule: "0 0 * * *"
    tasks:
      - name: "no-param"
        kind: "SQL"
        command: "SELECT 1"`

		tempFile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tempFile)

		err := pge.LoadYamlChains(ctx, tempFile, false)
		require.NoError(t, err)

		// Get the task ID
		var count int
		err = pge.ConfigDb.QueryRow(ctx, `
SELECT COUNT(*)
FROM timetable.task t
	JOIN timetable.chain c ON t.chain_id = c.chain_id 
	JOIN timetable.parameter p ON t.task_id = p.task_id
	WHERE c.chain_name = $1`,
			"test-no-params").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestNullString(t *testing.T) {
	// Note: nullString is not exported, so we test it indirectly through chain creation
	t.Run("Indirect test via chain creation", func(t *testing.T) {
		initmockdb(t)
		defer mockPool.Close()
		mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

		yamlContent := `chains:
  - name: "test-null-strings"
    schedule: "0 0 * * *"
    client_name: ""  # Should become NULL in database
    on_error: ""     # Should become NULL in database
    tasks:
      - name: "test-task"
        command: "SELECT 1"
        kind: "SQL"
        run_as: ""              # Should become NULL
        database_connection: "" # Should become NULL`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock chain and task creation with empty strings converted to NULL
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("test-null-strings").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery(`INSERT INTO timetable\.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		require.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

func TestLoadYamlChainsMultiTask(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	t.Run("Multi-task chain creation", func(t *testing.T) {
		yamlContent := `chains:
  - name: "multi-task-chain"
    schedule: "0 0 * * *"
    live: true
    max_instances: 2
    timeout: 300
    self_destruct: false
    exclusive: true
    client_name: "test-client"
    on_error: "CONTINUE"
    tasks:
      - name: "first-task"
        kind: "SQL"
        command: "SELECT 1"
        ignore_error: false
        autonomous: false
        timeout: 60
        run_as: "postgres"
        database_connection: "main"
        parameters: ["param1", 42]
      - name: "second-task"
        kind: "PROGRAM"
        command: "echo"
        ignore_error: true
        autonomous: true
        timeout: 30
        parameters: ["hello", "world"]`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock chain creation
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("multi-task-chain").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery(`INSERT INTO timetable\.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))

		// Mock first task creation
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))
		// Mock first task parameters (2 parameters)
		mockPool.ExpectExec(`INSERT INTO timetable\.parameter`).
			WithArgs(anyArgs(3)...).
			WillReturnResult(pgxmock.NewResult("INSERT", 1)).
			Times(2)

		// Mock second task creation
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(2))
		// Mock second task parameters (2 parameters)
		mockPool.ExpectExec(`INSERT INTO timetable\.parameter`).
			WithArgs(anyArgs(3)...).
			WillReturnResult(pgxmock.NewResult("INSERT", 1)).
			Times(2)

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		require.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Chain already exists without replace", func(t *testing.T) {
		yamlContent := `chains:
  - name: "existing-chain"
    schedule: "0 0 * * *"
    tasks:
      - command: "SELECT 1"
        kind: "SQL"`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock chain already exists
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("existing-chain").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Database error during chain creation", func(t *testing.T) {
		// Test with invalid schedule that fails YAML validation
		yamlContent := `chains:
  - name: "invalid-schedule-chain"
    schedule: "invalid cron expression that passes validation but fails in DB"
    tasks:
      - command: "SELECT 1"
        kind: "SQL"`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// This should fail at YAML validation level due to invalid cron format
		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse YAML file")
	})
}

func TestLoadYamlChainsErrorCases(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	t.Run("Invalid YAML file", func(t *testing.T) {
		err := mockpge.LoadYamlChains(context.Background(), "/non/existent/file.yaml", false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse YAML file")
	})

	t.Run("Invalid YAML content", func(t *testing.T) {
		invalidYaml := `chains:
  - name: ""  # Invalid: empty name
    schedule: "0 0 * * *"
    tasks:
      - command: "SELECT 1"`

		tmpfile := createTempYamlFile(t, invalidYaml)
		defer removeTempFile(t, tmpfile)

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse YAML file")
	})
}

func TestCreateChainFromYamlEdgeCases(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	t.Run("Task with no parameters", func(t *testing.T) {
		yamlContent := `chains:
  - name: "no-params-chain"
    schedule: "0 0 * * *"
    tasks:
      - name: "no-param-task"
        kind: "SQL"
        command: "SELECT CURRENT_TIMESTAMP"
      - name: "empty-param-task"
        kind: "SQL"
        command: "SELECT 1"
        parameters: []`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock chain creation and tasks without parameters
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("no-params-chain").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery(`INSERT INTO timetable\.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))

		// Mock first task creation (no parameters)
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))
		// Mock second task creation (empty parameters)
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(2))

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		require.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Complex parameter types", func(t *testing.T) {
		yamlContent := `chains:
  - name: "complex-params-chain"
    schedule: "0 0 * * *"
    tasks:
      - name: "complex-task"
        kind: "SQL"
        command: "SELECT $1::jsonb"
        parameters:
          - {"key": "value", "number": 42, "nested": {"inner": true}}`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock chain and task creation
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("complex-params-chain").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery(`INSERT INTO timetable\.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))
		// Mock parameter insertion
		mockPool.ExpectExec(`INSERT INTO timetable\.parameter`).
			WithArgs(anyArgs(3)...).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		require.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

func TestCreateChainFromYamlErrorHandling(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	t.Run("Multi-task chain with invalid parameter conversion", func(t *testing.T) {
		yamlContent := `chains:
  - name: "param-error-chain"
    schedule: "0 0 * * *"
    tasks:
      - name: "first-task"
        kind: "SQL"
        command: "SELECT 1"
        parameters: [{"invalid": {"deeply": {"nested": "value"}}}]
      - name: "second-task"
        kind: "SQL"
        command: "SELECT 2"`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock chain creation
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("param-error-chain").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery(`INSERT INTO timetable\.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))

		// Mock first task with complex parameter
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))
		mockPool.ExpectExec(`INSERT INTO timetable\.parameter`).
			WithArgs(anyArgs(3)...).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		// Mock second task (no parameters)
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(2))

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		// Should succeed even with complex parameters
		require.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Multi-task chain with various field types", func(t *testing.T) {
		yamlContent := `chains:
  - name: "comprehensive-multi-task"
    schedule: "@every 1h"
    live: false
    max_instances: 3
    timeout: 600
    self_destruct: true
    exclusive: false
    client_name: "test-client-multi"
    on_error: "IGNORE"
    tasks:
      - name: "sql-task"
        kind: "SQL"
        command: "SELECT $1, $2"
        parameters: ["string", 123]
        ignore_error: true
        autonomous: false
        timeout: 120
        run_as: "test_user"
        connect_string: "dbname=test"
      - name: "program-task"  
        kind: "PROGRAM"
        command: "echo"
        parameters: ["hello", "world"]
        ignore_error: false
        autonomous: true
        timeout: 60
      - name: "builtin-task"
        kind: "BUILTIN" 
        command: "Sleep"
        parameters: [5]
        ignore_error: false
        autonomous: false
        timeout: 10`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock comprehensive chain creation
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("comprehensive-multi-task").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery(`INSERT INTO timetable\.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))

		// Mock sql-task creation with 2 parameters
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))
		mockPool.ExpectExec(`INSERT INTO timetable\.parameter`).
			WithArgs(anyArgs(3)...).
			WillReturnResult(pgxmock.NewResult("INSERT", 1)).
			Times(2)

		// Mock program-task creation with 2 parameters
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(2))
		mockPool.ExpectExec(`INSERT INTO timetable\.parameter`).
			WithArgs(anyArgs(3)...).
			WillReturnResult(pgxmock.NewResult("INSERT", 1)).
			Times(2)

		// Mock builtin-task creation with 1 parameter
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(3))
		mockPool.ExpectExec(`INSERT INTO timetable\.parameter`).
			WithArgs(anyArgs(3)...).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		require.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

func TestNullStringFunction(t *testing.T) {
	// Testing nullString indirectly through database operations
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	t.Run("All null fields", func(t *testing.T) {
		yamlContent := `chains:
  - name: "all-nulls-chain"
    schedule: "0 0 * * *"
    # client_name: ""     # Should be NULL
    # on_error: ""        # Should be NULL  
    tasks:
      - name: "null-task"
        command: "SELECT 1"
        kind: "SQL"
        # run_as: ""              # Should be NULL
        # connect_string: ""      # Should be NULL`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock chain creation with NULL fields
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("all-nulls-chain").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery(`INSERT INTO timetable\.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		// Mock task creation with NULL fields
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		require.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Mixed null and non-null fields", func(t *testing.T) {
		yamlContent := `chains:
  - name: "mixed-nulls-chain"
    schedule: "0 0 * * *"
    client_name: "real-client"
    # on_error not specified - should be NULL
    tasks:
      - name: "mixed-task"
        command: "SELECT 1"
        kind: "SQL"
        run_as: "real-user"
        # connect_string not specified - should be NULL`

		tmpfile := createTempYamlFile(t, yamlContent)
		defer removeTempFile(t, tmpfile)

		// Mock chain creation with mixed NULL/non-NULL fields
		mockPool.ExpectQuery("SELECT EXISTS").
			WithArgs("mixed-nulls-chain").
			WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(false))
		mockPool.ExpectQuery(`INSERT INTO timetable\.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		// Mock task creation with mixed NULL/non-NULL fields
		mockPool.ExpectQuery(`INSERT INTO timetable\.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))

		err := mockpge.LoadYamlChains(context.Background(), tmpfile, false)
		require.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

func TestCreateChainFromYamlErrors(t *testing.T) {
	initmockdb(t)
	defer mockPool.Close()
	mockpge := pgengine.NewDB(mockPool, "pgengine_unit_test")

	t.Run("Database error during chain creation", func(t *testing.T) {
		mockPool.ExpectQuery(`INSERT INTO timetable.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnError(fmt.Errorf("simulated DB error"))
		_, err := mockpge.CreateChainFromYaml(ctx, &pgengine.YamlChain{})
		assert.Error(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Database error during task creation", func(t *testing.T) {
		mockPool.ExpectQuery(`INSERT INTO timetable.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		mockPool.ExpectQuery(`INSERT INTO timetable.task`).
			WithArgs(anyArgs(10)...).
			WillReturnError(fmt.Errorf("simulated DB error on task"))

		_, err := mockpge.CreateChainFromYaml(ctx, &pgengine.YamlChain{
			Chain:    pgengine.Chain{ChainName: "test-chain"},
			Schedule: "0 0 * * *",
			Tasks: []pgengine.YamlTask{
				{ChainTask: pgengine.ChainTask{Command: "SELECT 1", Kind: "SQL"}},
			},
		})
		assert.Error(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Database error during parameter unmarshalling", func(t *testing.T) {
		mockPool.ExpectQuery(`INSERT INTO timetable.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		mockPool.ExpectQuery(`INSERT INTO timetable.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))

		_, err := mockpge.CreateChainFromYaml(ctx, &pgengine.YamlChain{
			Chain:    pgengine.Chain{ChainName: "test-chain"},
			Schedule: "0 0 * * *",
			Tasks: []pgengine.YamlTask{
				{
					ChainTask:  pgengine.ChainTask{Command: "SELECT 1", Kind: "SQL"},
					Parameters: []any{func() {}}, // functions cannot be marshalled to JSON
				},
			},
		})
		assert.Error(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Database error during parameter creation", func(t *testing.T) {
		mockPool.ExpectQuery(`INSERT INTO timetable.chain`).
			WithArgs(anyArgs(9)...).
			WillReturnRows(pgxmock.NewRows([]string{"chain_id"}).AddRow(1))
		mockPool.ExpectQuery(`INSERT INTO timetable.task`).
			WithArgs(anyArgs(10)...).
			WillReturnRows(pgxmock.NewRows([]string{"task_id"}).AddRow(1))
		mockPool.ExpectExec(`INSERT INTO timetable.parameter`).
			WithArgs(anyArgs(3)...).
			WillReturnError(fmt.Errorf("simulated DB error on parameter"))

		_, err := mockpge.CreateChainFromYaml(ctx, &pgengine.YamlChain{
			Chain:    pgengine.Chain{ChainName: "test-chain"},
			Schedule: "0 0 * * *",
			Tasks: []pgengine.YamlTask{
				{
					ChainTask:  pgengine.ChainTask{Command: "SELECT 1", Kind: "SQL"},
					Parameters: []any{"foo"},
				},
			},
		})
		assert.Error(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}
