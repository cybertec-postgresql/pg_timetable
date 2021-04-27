package pgengine_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/jackc/pgtype"
	pgx "github.com/jackc/pgx/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/scheduler"
)

// this instance used for all engine tests
var pge *pgengine.PgEngine

var cmdOpts *config.CmdOptions = config.NewCmdOptions("--clientname=pgengine_unit_test", "--password=somestrong")

// SetupTestCaseEx allows to configure the test case before execution
func SetupTestCaseEx(t *testing.T, fc func(c *config.CmdOptions)) func(t *testing.T) {
	fc(cmdOpts)
	return SetupTestCase(t)
}

//SetupTestCase used to connect and to initialize test PostgreSQL database
func SetupTestCase(t *testing.T) func(t *testing.T) {
	t.Log("Setup test case")
	timeout := time.After(6 * time.Second)
	done := make(chan bool)
	go func() {
		pge, _ = pgengine.New(context.Background(), *cmdOpts, log.Init("error"))
		done <- true
	}()
	select {
	case <-timeout:
		t.Fatal("Cannot connect and initialize test database in time")
	case <-done:
	}
	return func(t *testing.T) {
		_, _ = pge.ConfigDb.Exec(context.Background(), "DROP SCHEMA IF EXISTS timetable CASCADE")
		pge.ConfigDb.Close()
		t.Log("Test schema dropped")
	}
}

// setupTestRenoteDBFunc used to connect to remote postgreSQL database
var setupTestRemoteDBFunc = func() (pgengine.PgxConnIface, pgx.Tx, error) {
	c := cmdOpts.Connection
	connstr := fmt.Sprintf("host='%s' port='%d' sslmode='%s' dbname='%s' user='%s' password='%s'",
		c.Host, c.Port, c.SSLMode, c.DBName, c.User, c.Password)
	return pge.GetRemoteDBTransaction(context.Background(), connstr)
}

func TestInitAndTestConfigDBConnection(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)

	ctx := context.Background()

	require.NotNil(t, pge.ConfigDb, "ConfigDB should be initialized")

	t.Run("Check timetable tables", func(t *testing.T) {
		var oid int
		tableNames := []string{"command", "task",
			"chain", "parameter",
			"log", "execution_log", "run_status"}
		for _, tableName := range tableNames {
			err := pge.ConfigDb.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(to_regclass('timetable.%s'), 0) :: int", tableName)).Scan(&oid)
			assert.NoError(t, err, fmt.Sprintf("Query for %s existence failed", tableName))
			assert.NotEqual(t, pgengine.InvalidOid, oid, fmt.Sprintf("timetable.%s table doesn't exist", tableName))
		}
	})

	t.Run("Check timetable functions", func(t *testing.T) {
		var oid int
		funcNames := []string{"_validate_json_schema_type(text, jsonb)",
			"validate_json_schema(jsonb, jsonb, jsonb)",
			"get_chain_running_statuses(bigint)",
			"health_check(TEXT)",
			"add_task(TEXT, BIGINT)",
			"is_cron_in_time(timetable.cron, timestamptz)"}
		for _, funcName := range funcNames {
			err := pge.ConfigDb.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(to_regprocedure('timetable.%s'), 0) :: int", funcName)).Scan(&oid)
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
			_, err := pge.ConfigDb.Exec(ctx, stmt)
			assert.NoError(t, err, fmt.Sprintf("Wrong input cron format: %s", stmt))
		}
	})

	t.Run("Check connection closing", func(t *testing.T) {
		pge.Finalize()
		assert.Nil(t, pge.ConfigDb, "Connection isn't closed properly")
		// reinit connection to execute teardown actions
		pge, _ = pgengine.New(context.Background(), *cmdOpts, log.Init("error"))
	})

	t.Run("Check Reconnecting Database", func(t *testing.T) {
		assert.Equal(t, true, pge.ReconnectAndFixLeftovers(ctx),
			"Should succeed for reconnect")
	})
}

func TestFailedConnect(t *testing.T) {
	c := config.NewCmdOptions("-h", "fake", "-c", "pgengine_test")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*pgengine.WaitTime*2)
	defer cancel()
	_, err := pgengine.New(ctx, *c, log.Init("error"))
	assert.ErrorIs(t, err, ctx.Err())
}

func TestSchedulerFunctions(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)

	ctx := context.Background()

	t.Run("Check FixSchedulerCrash function", func(t *testing.T) {
		assert.NotPanics(t, func() { pge.FixSchedulerCrash(ctx) }, "Fix scheduler crash failed")
	})

	t.Run("Check DeleteChainConfig funсtion", func(t *testing.T) {
		assert.Equal(t, false, pge.DeleteChainConfig(ctx, 0), "Should not delete in clean database")
	})

	t.Run("Check GetChainElements funсtion", func(t *testing.T) {
		var chains []pgengine.ChainElement
		tx, err := pge.StartTransaction(ctx)
		assert.NoError(t, err, "Should start transaction")
		assert.True(t, pge.GetChainElements(ctx, tx, &chains, 0), "Should no error in clean database")
		assert.Empty(t, chains, "Should be empty in clean database")
		pge.MustCommitTransaction(ctx, tx)
	})

	t.Run("Check GetChainParamValues funсtion", func(t *testing.T) {
		var paramVals []string
		tx, err := pge.StartTransaction(ctx)
		assert.NoError(t, err, "Should start transaction")
		assert.True(t, pge.GetChainParamValues(ctx, tx, &paramVals, &pgengine.ChainElement{
			TaskID:  0,
			ChainID: 0}), "Should no error in clean database")
		assert.Empty(t, paramVals, "Should be empty in clean database")
		pge.MustCommitTransaction(ctx, tx)
	})

	t.Run("Check InsertChainRunStatus funсtion", func(t *testing.T) {
		var id int
		assert.NotPanics(t, func() { id = pge.InsertChainRunStatus(ctx, 0) },
			"Should no error in clean database")
		assert.NotZero(t, id, "Run status id should be greater then 0")
	})

	t.Run("Check ExecuteSQLCommand function", func(t *testing.T) {
		tx, err := pge.StartTransaction(ctx)
		assert.NoError(t, err, "Should start transaction")
		f := func(sql string, params []string) error {
			_, err := pge.ExecuteSQLCommand(ctx, tx, sql, params)
			return err
		}
		assert.Error(t, f("", nil), "Should error for empty script")
		assert.Error(t, f(" 	", nil), "Should error for whitespace only script")
		assert.NoError(t, f(";", nil), "Simple query with nil as parameters argument")
		assert.NoError(t, f(";", []string{}), "Simple query with empty slice as parameters argument")
		assert.NoError(t, f("SELECT $1::int4", []string{"[42]"}), "Simple query with non empty parameters")
		assert.NoError(t, f("SELECT $1::int4", []string{"[42]", `[14]`}), "Simple query with doubled parameters")
		assert.NoError(t, f("SELECT $1::int4, $2::text", []string{`[42, "hey"]`}), "Simple query with two parameters")

		pge.MustCommitTransaction(ctx, tx)
	})

}

func TestBuiltInTasks(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)
	t.Run("Check built-in tasks number", func(t *testing.T) {
		var num int
		err := pge.ConfigDb.QueryRow(context.Background(), "SELECT count(1) FROM timetable.command WHERE kind = 'BUILTIN'").Scan(&num)
		assert.NoError(t, err, "Query for built-in tasks existence failed")
		assert.Equal(t, len(scheduler.Tasks), num, fmt.Sprintf("Wrong number of built-in tasks: %d", num))
	})
}

func TestGetRemoteDBTransaction(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)

	ctx := context.Background()

	remoteDb, tx, err := setupTestRemoteDBFunc()
	defer pge.FinalizeRemoteDBConnection(ctx, remoteDb)
	require.NoError(t, err, "remoteDB should be initialized")
	require.NotNil(t, remoteDb, "remoteDB should be initialized")

	t.Run("Check connection closing", func(t *testing.T) {
		pge.FinalizeRemoteDBConnection(ctx, remoteDb)
		assert.NotNil(t, remoteDb, "Connection isn't closed properly")
	})

	t.Run("Check set role function", func(t *testing.T) {
		var runUID pgtype.Varchar
		runUID.String = cmdOpts.Connection.User
		assert.NotPanics(t, func() { pge.SetRole(ctx, tx, runUID) }, "Set Role failed")
	})

	t.Run("Check reset role function", func(t *testing.T) {
		assert.NotPanics(t, func() { pge.ResetRole(ctx, tx) }, "Reset Role failed")
	})

	pge.MustCommitTransaction(ctx, tx)
}

func TestSamplesScripts(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)

	files, err := ioutil.ReadDir("../../samples")
	assert.NoError(t, err, "Cannot read samples directory")
	l := log.Init("error")
	for _, f := range files {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, pge.ExecuteCustomScripts(ctx, "../../samples/"+f.Name()),
			"Sample query failed: ", f.Name())
		assert.Equal(t, scheduler.New(pge, l).Run(ctx), scheduler.ContextCancelled)
		_, err = pge.ConfigDb.Exec(context.Background(),
			"TRUNCATE timetable.task, timetable.chain CASCADE")
		assert.NoError(t, err)
	}
}
