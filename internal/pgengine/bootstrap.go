package pgengine

import (
	"context"
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

// PgxIface is common interface for every pgx classes
type PgxIface interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
	Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error)
	Ping(ctx context.Context) error
}
type PgxConnIface interface {
	PgxIface
	Close(ctx context.Context) error
}
type PgxPoolIface interface {
	PgxIface
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
	Close()
}

// PgEngine is responsible for every database-related action
type PgEngine struct {
	ConfigDb PgxPoolIface
	cmdparser.CmdOptions
	// NOTIFY messages passed verification are pushed to this channel
	chainSignalChan chan ChainSignal
}

var sqls = []string{sqlDDL, sqlJSONSchema, sqlTasks, sqlJobFunctions}
var sqlNames = []string{"DDL", "JSON Schema", "Built-in Tasks", "Job Functions"}

// New opens connection and creates schema
func New(ctx context.Context, cmdOpts cmdparser.CmdOptions) (*PgEngine, error) {
	Log("DEBUG", fmt.Sprintf("Starting new session... %s", &cmdOpts))
	var wt int = WaitTime
	var err error
	pge := &PgEngine{nil, cmdOpts, make(chan ChainSignal, 64)}
	config := pge.getPgxConnConfig()
	pge.ConfigDb, err = pgxpool.ConnectConfig(ctx, config)
	for err != nil {
		Log("ERROR", err)
		Log("LOG", "Reconnecting in ", wt, " sec...")
		select {
		case <-time.After(time.Duration(wt) * time.Second):
			pge.ConfigDb, err = pgxpool.ConnectConfig(ctx, config)
		case <-ctx.Done():
			Log("ERROR", "Connection request cancelled: ", ctx.Err())
			return nil, ctx.Err()
		}
		if wt < maxWaitTime {
			wt = wt * 2
		}
	}
	Log("LOG", "Connection established...")
	Log("LOG", fmt.Sprintf("Proceeding as '%s' with client PID %d", cmdOpts.ClientName, os.Getpid()))
	if err := pge.ExecuteSchemaScripts(ctx); err != nil {
		return nil, err
	}
	if cmdOpts.File != "" {
		if err := pge.ExecuteCustomScripts(ctx, cmdOpts.File); err != nil {
			return nil, err
		}
	}
	return pge, nil
}

// getPgxConnConfig transforms standard connestion string to pgx specific one with
func (pge *PgEngine) getPgxConnConfig() *pgxpool.Config {
	connstr := fmt.Sprintf("application_name='pg_timetable' host='%s' port='%s' dbname='%s' sslmode='%s' user='%s' password='%s' pool_max_conns=32",
		pge.Host, pge.Port, pge.Dbname, pge.SSLMode, pge.User, pge.Password)
	Log("DEBUG", "Connection string: ", connstr)
	connConfig, err := pgxpool.ParseConfig(connstr)
	if err != nil {
		Log("ERROR", err)
		return nil
	}
	connConfig.ConnConfig.OnNotice = func(c *pgconn.PgConn, n *pgconn.Notice) {
		//use background context without deadline for async notifications handler
		Log("USER", "Severity: ", n.Severity, "; Message: ", n.Message)
	}
	connConfig.AfterConnect = func(ctx context.Context, pgconn *pgx.Conn) (err error) {
		if err = TryLockClientName(ctx, pge.ClientName, pgconn); err != nil {
			return err
		}
		_, err = pgconn.Exec(ctx, "LISTEN "+quoteIdent(pge.ClientName))
		return err
	}
	if !pge.Debug { //will handle notification in HandleNotifications directly
		connConfig.ConnConfig.OnNotification = pge.NotificationHandler
	}
	connConfig.ConnConfig.Logger = Logger{}
	if VerboseLogLevel {
		connConfig.ConnConfig.LogLevel = pgx.LogLevelDebug
	} else {
		connConfig.ConnConfig.LogLevel = pgx.LogLevelWarn
	}
	return connConfig
}

// TryLockClientName obtains lock on the server to prevent another client with the same name
func TryLockClientName(ctx context.Context, clientName string, conn *pgx.Conn) error {
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
		Log("DEBUG", fmt.Sprintf("Trying to get lock for '%s', client pid %d, server pid %d", clientName, os.Getpid(), conn.PgConn().PID()))
		sql := fmt.Sprintf("SELECT timetable.try_lock_client_name(%d, $worker$%s$worker$)", os.Getpid(), clientName)
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

// ExecuteCustomScripts executes SQL scripts in files
func (pge *PgEngine) ExecuteCustomScripts(ctx context.Context, filename ...string) error {
	for _, f := range filename {
		sql, err := ioutil.ReadFile(f)
		if err != nil {
			Log("PANIC", err)
			return err
		}
		Log("LOG", "Executing script: ", f)
		if _, err = pge.ConfigDb.Exec(ctx, string(sql)); err != nil {
			Log("PANIC", err)
			return err
		}
		Log("LOG", "Script file executed: ", f)
	}
	return nil
}

// ExecuteSchemaScripts executes initial schema scripts
func (pge *PgEngine) ExecuteSchemaScripts(ctx context.Context) error {
	var exists bool
	err := pge.ConfigDb.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'timetable')").Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		for i, sql := range sqls {
			sqlName := sqlNames[i]
			Log("LOG", "Executing script: ", sqlName)
			if _, err = pge.ConfigDb.Exec(ctx, sql); err != nil {
				Log("PANIC", err)
				Log("PANIC", "Dropping \"timetable\" schema...")
				_, err = pge.ConfigDb.Exec(ctx, "DROP SCHEMA IF EXISTS timetable CASCADE")
				if err != nil {
					Log("PANIC", err)
				}
				return err
			}
			Log("LOG", "Schema file executed: "+sqlName)
		}
		Log("LOG", "Configuration schema created...")
	}
	return nil
}

// Finalize closes session
func (pge *PgEngine) Finalize() {
	Log("LOG", "Closing session")
	_, err := pge.ConfigDb.Exec(context.Background(), "DELETE FROM timetable.active_session WHERE client_pid = $1 AND client_name = $2", os.Getpid(), pge.ClientName)
	if err != nil {
		Log("ERROR", "Cannot finalize database session: ", err)
	}
	pge.ConfigDb = nil
}

//ReconnectAndFixLeftovers keeps trying reconnecting every `waitTime` seconds till connection established
func (pge *PgEngine) ReconnectAndFixLeftovers(ctx context.Context) bool {
	for pge.ConfigDb.Ping(ctx) != nil {
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
	pge.FixSchedulerCrash(ctx)
	return true
}
