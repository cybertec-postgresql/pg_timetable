package pgengine_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/scheduler"
	"github.com/cybertec-postgresql/pg_timetable/internal/tasks"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
)

var cmdOpts *cmdparser.CmdOptions = cmdparser.NewCmdOptions("pgengine_unit_test")

func TestMain(m *testing.M) {
	testutils.TestMain(m)
}

// setupTestRenoteDBFunc used to connect to remote postgreSQL database
var setupTestRemoteDBFunc = func() (*sqlx.DB, *sqlx.Tx, error) {
	connstr := fmt.Sprintf("host='%s' port='%s' sslmode='%s' dbname='%s' user='%s' password='%s'",
		cmdOpts.Host, cmdOpts.Port, cmdOpts.SSLMode, cmdOpts.Dbname, cmdOpts.User, cmdOpts.Password)
	return pgengine.GetRemoteDBTransaction(context.Background(), connstr)
}

func TestInitAndTestConfigDBConnection(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	ctx := context.Background()

	require.NotNil(t, pgengine.ConfigDb, "ConfigDB should be initialized")

	t.Run("Check timetable tables", func(t *testing.T) {
		var oid int
		tableNames := []string{"database_connection", "base_task", "task_chain",
			"chain_execution_config", "chain_execution_parameters",
			"log", "execution_log", "run_status"}
		for _, tableName := range tableNames {
			err := pgengine.ConfigDb.Get(&oid, fmt.Sprintf("SELECT COALESCE(to_regclass('timetable.%s'), 0) :: int", tableName))
			assert.NoError(t, err, fmt.Sprintf("Query for %s existence failed", tableName))
			assert.NotEqual(t, pgengine.InvalidOid, oid, fmt.Sprintf("timetable.%s function doesn't exist", tableName))
		}
	})

	t.Run("Check timetable functions", func(t *testing.T) {
		var oid int
		funcNames := []string{"_validate_json_schema_type(text, jsonb)",
			"validate_json_schema(jsonb, jsonb, jsonb)",
			"get_running_jobs(bigint)",
			"trig_chain_fixer()",
			"is_cron_in_time(timetable.cron, timestamptz)"}
		for _, funcName := range funcNames {
			err := pgengine.ConfigDb.Get(&oid, fmt.Sprintf("SELECT COALESCE(to_regprocedure('timetable.%s'), 0) :: int", funcName))
			assert.NoError(t, err, fmt.Sprintf("Query for %s existence failed", funcName))
			assert.NotEqual(t, pgengine.InvalidOid, oid, fmt.Sprintf("timetable.%s function doesn't exist", funcName))
		}
	})

	t.Run("Check timetable.cron type input", func(t *testing.T) {
		stmts := []string{
			//cron
			"SELECT '0 1 1 * 1' :: timetable.cron",
			"SELECT '0 1 1 * 1,2' :: timetable.cron",
			"SELECT '0 1 1 * 1,2,3' :: timetable.cron",
			"SELECT '0 1 * * 1/4' :: timetable.cron",
			"SELECT '0 * * 0 1-4' :: timetable.cron",
			"SELECT '0 * * * 2/4' :: timetable.cron",
			"SELECT '* * * * *' :: timetable.cron",
			"SELECT '*/2 */2 * * *' :: timetable.cron",
			// predefined
			"SELECT '@reboot' :: timetable.cron",
			"SELECT '@every 1 sec' ::  timetable.cron",
			"SELECT '@after 1 sec' ::  timetable.cron"}
		for _, stmt := range stmts {
			_, err := pgengine.ConfigDb.Exec(stmt)
			assert.NoError(t, err, fmt.Sprintf("Wrong input cron format: %s", stmt))
		}
	})

	t.Run("Check log facility", func(t *testing.T) {
		var count int
		logLevels := []string{"DEBUG", "NOTICE", "LOG", "ERROR", "PANIC"}
		for _, pgengine.VerboseLogLevel = range []bool{true, false} {
			pgengine.ConfigDb.MustExec("TRUNCATE timetable.log")
			for _, logLevel := range logLevels {
				assert.NotPanics(t, func() {
					pgengine.LogToDB(ctx, logLevel, logLevel)
				}, "LogToDB panicked")

				if !pgengine.VerboseLogLevel {
					switch logLevel {
					case "DEBUG", "NOTICE", "LOG":
						continue
					}
				}
				err := pgengine.ConfigDb.Get(&count, "SELECT count(1) FROM timetable.log WHERE log_level = $1 AND message = $2",
					logLevel, logLevel)
				assert.NoError(t, err, fmt.Sprintf("Query for %s log entry failed", logLevel))
				assert.Equal(t, 1, count, fmt.Sprintf("%s log entry doesn't exist", logLevel))
			}
		}
	})

	t.Run("Check connection closing", func(t *testing.T) {
		pgengine.FinalizeConfigDBConnection()
		assert.Nil(t, pgengine.ConfigDb, "Connection isn't closed properly")
		// reinit connection to execute teardown actions
		pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdOpts)
	})

	t.Run("Check Reconnecting Database", func(t *testing.T) {
		assert.Equal(t, true, pgengine.ReconnectDbAndFixLeftovers(ctx),
			"Should succeed for reconnect")
	})

	t.Run("Check SetupCloseHandler function", func(t *testing.T) {
		assert.NotPanics(t, pgengine.SetupCloseHandler, "Setup Close handler failed")
	})
}

func TestSchedulerFunctions(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	ctx := context.Background()

	t.Run("Check FixSchedulerCrash function", func(t *testing.T) {
		assert.NotPanics(t, func() { pgengine.FixSchedulerCrash(ctx) }, "Fix scheduler crash failed")
	})

	t.Run("Check CanProceedChainExecution funсtion", func(t *testing.T) {
		assert.Equal(t, true, pgengine.CanProceedChainExecution(ctx, 0, 0), "Should proceed with clean database")
	})

	t.Run("Check DeleteChainConfig funсtion", func(t *testing.T) {
		assert.Equal(t, false, pgengine.DeleteChainConfig(ctx, 0), "Should not delete in clean database")
	})

	t.Run("Check GetChainElements funсtion", func(t *testing.T) {
		var chains []pgengine.ChainElementExecution
		tx, err := pgengine.StartTransaction(ctx)
		assert.NoError(t, err, "Should start transaction")
		assert.True(t, pgengine.GetChainElements(ctx, tx, &chains, 0), "Should no error in clean database")
		assert.Empty(t, chains, "Should be empty in clean database")
		pgengine.MustCommitTransaction(ctx, tx)
	})

	t.Run("Check GetChainParamValues funсtion", func(t *testing.T) {
		var paramVals []string
		tx, err := pgengine.StartTransaction(ctx)
		assert.NoError(t, err, "Should start transaction")
		assert.True(t, pgengine.GetChainParamValues(ctx, tx, &paramVals, &pgengine.ChainElementExecution{
			ChainID:     0,
			ChainConfig: 0}), "Should no error in clean database")
		assert.Empty(t, paramVals, "Should be empty in clean database")
		pgengine.MustCommitTransaction(ctx, tx)
	})

	t.Run("Check InsertChainRunStatus funсtion", func(t *testing.T) {
		var id int
		assert.NotPanics(t, func() { id = pgengine.InsertChainRunStatus(ctx, 0, 0) }, "Should no error in clean database")
		assert.NotZero(t, id, "Run status id should be greater then 0")
	})

	t.Run("Check Remote DB Connection string", func(t *testing.T) {
		var databaseConnection sql.NullString
		tx, err := pgengine.StartTransaction(ctx)
		assert.NoError(t, err, "Should start transaction")
		assert.NotNil(t, pgengine.GetConnectionString(ctx, databaseConnection), "Should no error in clean database")
		pgengine.MustCommitTransaction(ctx, tx)
	})

	t.Run("Check ExecuteSQLCommand function", func(t *testing.T) {
		tx, err := pgengine.StartTransaction(ctx)
		assert.NoError(t, err, "Should start transaction")
		assert.Error(t, pgengine.ExecuteSQLCommand(ctx, tx, "", nil), "Should error for empty script")
		assert.Error(t, pgengine.ExecuteSQLCommand(ctx, tx, " 	", nil), "Should error for whitespace only script")
		assert.NoError(t, pgengine.ExecuteSQLCommand(ctx, tx, ";", nil), "Simple query with nil as parameters argument")
		assert.NoError(t, pgengine.ExecuteSQLCommand(ctx, tx, ";", []string{}), "Simple query with empty slice as parameters argument")
		assert.NoError(t, pgengine.ExecuteSQLCommand(ctx, tx, "SELECT $1", []string{"[42]"}), "Simple query with non empty parameters")
		assert.NoError(t, pgengine.ExecuteSQLCommand(ctx, tx, "SELECT $1", []string{"[42]", `["hey"]`}), "Simple query with doubled parameters")
		assert.NoError(t, pgengine.ExecuteSQLCommand(ctx, tx, "SELECT $1, $2", []string{`[42, "hey"]`}), "Simple query with two parameters")

		pgengine.MustCommitTransaction(ctx, tx)
	})

}

func TestBuiltInTasks(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)
	t.Run("Check built-in tasks number", func(t *testing.T) {
		var num int
		err := pgengine.ConfigDb.Get(&num, "SELECT count(1) FROM timetable.base_task WHERE kind = 'BUILTIN'")
		assert.NoError(t, err, "Query for built-in tasks existence failed")
		assert.Equal(t, len(tasks.Tasks), num, fmt.Sprintf("Wrong number of built-in tasks: %d", num))
	})
}

func TestGetRemoteDBTransaction(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	ctx := context.Background()

	remoteDb, tx, err := setupTestRemoteDBFunc()
	defer pgengine.FinalizeRemoteDBConnection(ctx, remoteDb)
	require.NoError(t, err, "remoteDB should be initialized")
	require.NotNil(t, remoteDb, "remoteDB should be initialized")

	t.Run("Check connection closing", func(t *testing.T) {
		pgengine.FinalizeRemoteDBConnection(ctx, remoteDb)
		assert.NotNil(t, remoteDb, "Connection isn't closed properly")
	})

	t.Run("Check set role function", func(t *testing.T) {
		var runUID sql.NullString
		runUID.String = cmdOpts.User
		assert.NotPanics(t, func() { pgengine.SetRole(ctx, tx, runUID) }, "Set Role failed")
	})

	t.Run("Check reset role function", func(t *testing.T) {
		assert.NotPanics(t, func() { pgengine.ResetRole(ctx, tx) }, "Reset Role failed")
	})

	pgengine.MustCommitTransaction(ctx, tx)
}

func TestSamplesScripts(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	files, err := ioutil.ReadDir("../../samples")
	assert.NoError(t, err, "Cannot read samples directory")

	for _, f := range files {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ok := pgengine.ExecuteCustomScripts(ctx, "../../samples/"+f.Name())
		assert.True(t, ok, "Sample query failed: ", f.Name())
		assert.Equal(t, scheduler.Run(ctx, false), scheduler.ContextCancelled)
		_, _ = pgengine.ConfigDb.Exec("TRUNCATE timetable.task_chain CASCADE")
	}
}
