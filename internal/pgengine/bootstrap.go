package pgengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"

	pgconn "github.com/jackc/pgconn"
	pgx "github.com/jackc/pgx/v4"
	pgxpool "github.com/jackc/pgx/v4/pgxpool"
)

// WaitTime specifies amount of time in seconds to wait before reconnecting to DB
const WaitTime = 5

// maximum wait time before reconnect attempts
const maxWaitTime = WaitTime * 16

type pgxpoolIface interface {
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
	Begin(ctx context.Context) (pgx.Tx, error)
	Close()
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
	Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error)
	Ping(ctx context.Context) error
}

// ConfigDb is the global database object
var ConfigDb pgxpoolIface

// ClientName is unique ifentifier of the scheduler application running
var ClientName string

// NoProgramTasks parameter disables PROGRAM tasks executing
var NoProgramTasks bool

var sqls = []string{sqlDDL, sqlJSONSchema, sqlTasks, sqlJobFunctions}
var sqlNames = []string{"DDL", "JSON Schema", "Built-in Tasks", "Job Functions"}

// Logger incapsulates Logger interface from pgx package
type Logger struct {
	pgx.Logger
}

// Log prints messages using native log levels
func (l Logger) Log(ctx context.Context, level pgx.LogLevel, msg string, data map[string]interface{}) {
	var s string
	switch level {
	case pgx.LogLevelTrace, pgx.LogLevelDebug, pgx.LogLevelInfo:
		s = "DEBUG"
	case pgx.LogLevelWarn:
		s = "NOTICE"
	case pgx.LogLevelError:
		s = "ERROR"
	default:
		s = "LOG"
	}
	j, _ := json.Marshal(data)
	s = fmt.Sprintf(GetLogPrefix(s), fmt.Sprint(msg, " ", string(j)))
	fmt.Println(s)
}

// OpenDB opens connection to the database
var OpenDB func(driverName string, dataSourceName string) (*sql.DB, error) = sql.Open

// TryLockClientName obtains lock on the server to prevent another client with the same name
func TryLockClientName(ctx context.Context, conn *pgx.Conn) error {
	// check if the schema is available already first
	var procoid int
	err := conn.QueryRow(ctx, "SELECT COALESCE(to_regproc('timetable.try_lock_client_name')::int4, 0)").Scan(&procoid)
	if err != nil {
		return err
	}
	if procoid == 0 {
		//there is no schema yet, will lock after bootstrapping
		Log("DEBUG", "There is no schema yet, will lock after bootstrapping, server pid ", conn.PgConn().PID())
		return nil
	}

	var wt int = WaitTime
	for {
		Log("DEBUG", fmt.Sprintf("Trying to get lock for '%s', client pid %d, server pid %d", ClientName, os.Getpid(), conn.PgConn().PID()))
		sql := fmt.Sprintf("SELECT timetable.try_lock_client_name(%d, $worker$%s$worker$)", os.Getpid(), ClientName)
		var locked bool
		err = conn.QueryRow(ctx, sql).Scan(&locked)
		if err != nil {
			Log("ERROR", "Error occurred during client name locking: ", err)
			return err
		} else if !locked {
			Log("LOG", "Cannot obtain lock for a session")
		} else {
			return nil
		}
		select {
		case <-time.After(time.Duration(wt) * time.Second):
			if wt < maxWaitTime {
				wt = wt * 2
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// getPgxConnConfig transforms standard connestion string to pgx specific one with
func getPgxConnConfig(cmdOpts cmdparser.CmdOptions) *pgxpool.Config {
	connstr := fmt.Sprintf("application_name='pg_timetable' host='%s' port='%s' dbname='%s' sslmode='%s' user='%s' password='%s'",
		cmdOpts.Host, cmdOpts.Port, cmdOpts.Dbname, cmdOpts.SSLMode, cmdOpts.User, cmdOpts.Password)
	Log("DEBUG", "Connection string: ", connstr)
	connConfig, err := pgxpool.ParseConfig(connstr)
	if err != nil {
		Log("ERROR", err)
		return nil
	}
	connConfig.ConnConfig.OnNotice = func(c *pgconn.PgConn, n *pgconn.Notice) {
		//use background context without deadline for async notifications handler
		LogToDB(context.Background(), "USER", "Severity: ", n.Severity, "; Message: ", n.Message)
	}

	connConfig.AfterConnect = func(ctx context.Context, pgconn *pgx.Conn) (err error) {
		if err = TryLockClientName(ctx, pgconn); err != nil {
			return err
		}
		_, err = pgconn.Exec(ctx, "LISTEN "+ClientName)
		return err
	}
	if !cmdOpts.Debug { //will handle notification in HandleNotifications directly
		connConfig.ConnConfig.OnNotification = NotificationHandler
	}

	connConfig.ConnConfig.Logger = Logger{}
	if VerboseLogLevel {
		connConfig.ConnConfig.LogLevel = pgx.LogLevelDebug
	} else {
		connConfig.ConnConfig.LogLevel = pgx.LogLevelWarn
	}
	connConfig.ConnConfig.PreferSimpleProtocol = true
	return connConfig
}

// InitAndTestConfigDBConnection opens connection and creates schema
func InitAndTestConfigDBConnection(ctx context.Context, cmdOpts cmdparser.CmdOptions) bool {
	ClientName = cmdOpts.ClientName
	NoProgramTasks = cmdOpts.NoShellTasks || cmdOpts.NoProgramTasks
	VerboseLogLevel = cmdOpts.Verbose
	Log("DEBUG", fmt.Sprintf("Starting new session... %s", &cmdOpts))
	var wt int = WaitTime
	var err error
	config := getPgxConnConfig(cmdOpts)
	ConfigDb, err = pgxpool.ConnectConfig(ctx, config)
	for err != nil {
		Log("ERROR", err)
		Log("LOG", "Reconnecting in ", wt, " sec...")
		select {
		case <-time.After(time.Duration(wt) * time.Second):
			ConfigDb, err = pgxpool.ConnectConfig(ctx, config)
		case <-ctx.Done():
			Log("ERROR", "Connection request cancelled: ", ctx.Err())
			return false
		}
		if wt < maxWaitTime {
			wt = wt * 2
		}
	}
	Log("LOG", "Connection established...")
	Log("LOG", fmt.Sprintf("Proceeding as '%s' with client PID %d", ClientName, os.Getpid()))
	if !ExecuteSchemaScripts(ctx) {
		return false
	}
	if cmdOpts.File != "" {
		if !ExecuteCustomScripts(ctx, cmdOpts.File) {
			return false
		}
	}
	return true
}

// ExecuteCustomScripts executes SQL scripts in files
func ExecuteCustomScripts(ctx context.Context, filename ...string) bool {
	for _, f := range filename {
		sql, err := ioutil.ReadFile(f)
		if err != nil {
			Log("PANIC", err)
			return false
		}
		Log("LOG", "Executing script: ", f)
		if _, err = ConfigDb.Exec(ctx, string(sql)); err != nil {
			Log("PANIC", err)
			return false
		}
		Log("LOG", "Script file executed: ", f)
	}
	return true
}

// ExecuteSchemaScripts executes initial schema scripts
func ExecuteSchemaScripts(ctx context.Context) bool {
	var exists bool
	err := ConfigDb.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'timetable')").Scan(&exists)
	if err != nil {
		return false
	}
	if !exists {
		for i, sql := range sqls {
			sqlName := sqlNames[i]
			Log("LOG", "Executing script: ", sqlName)
			if _, err = ConfigDb.Exec(ctx, sql); err != nil {
				Log("PANIC", err)
				Log("PANIC", "Dropping \"timetable\" schema...")
				_, err = ConfigDb.Exec(ctx, "DROP SCHEMA IF EXISTS timetable CASCADE")
				if err != nil {
					Log("PANIC", err)
				}
				return false
			}
			Log("LOG", "Schema file executed: "+sqlName)
		}
		Log("LOG", "Configuration schema created...")
	}
	return true
}

// FinalizeConfigDBConnection closes session
func FinalizeConfigDBConnection() {
	Log("LOG", "Closing session")
	_, err := ConfigDb.Exec(context.Background(), "DELETE FROM timetable.active_session WHERE client_pid = $1 AND client_name = $2", os.Getpid(), ClientName)
	if err != nil {
		Log("ERROR", "Cannot finalize database session: ", err)
	}
	ConfigDb = nil
}

//ReconnectDbAndFixLeftovers keeps trying reconnecting every `waitTime` seconds till connection established
func ReconnectDbAndFixLeftovers(ctx context.Context) bool {
	for ConfigDb.Ping(ctx) != nil {
		Log("REPAIR", "Connection to the server was lost. Waiting for ", WaitTime, " sec...")
		select {
		case <-time.After(WaitTime * time.Second):
			Log("REPAIR", "Reconnecting...")
		case <-ctx.Done():
			Log("ERROR", fmt.Sprintf("request cancelled: %v", ctx.Err()))
			return false
		}
	}
	Log("LOG", "Connection reestablished...")
	FixSchedulerCrash(ctx)
	return true
}
