package pgengine

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"

	pgconn "github.com/jackc/pgconn"
	pgx "github.com/jackc/pgx/v4"
	pgxpool "github.com/jackc/pgx/v4/pgxpool"
)

// WaitTime specifies amount of time in seconds to wait before reconnecting to DB
const WaitTime = 5

// maximum wait time before reconnect attempts
const maxWaitTime = WaitTime * 16

// PgxIface is common interface for every pgx class
type PgxIface interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
	Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error)
	Ping(ctx context.Context) error
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

// PgxConnIface is interface representing pgx connection
type PgxConnIface interface {
	PgxIface
	Close(ctx context.Context) error
}

// PgxPoolIface is interface representing pgx pool
type PgxPoolIface interface {
	PgxIface
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
	Close()
}

// PgEngine is responsible for every database-related action
type PgEngine struct {
	l        log.LoggerHookerIface
	ConfigDb PgxPoolIface
	config.CmdOptions
	// NOTIFY messages passed verification are pushed to this channel
	chainSignalChan chan ChainSignal
}

var sqls = []string{sqlDDL, sqlJSONSchema, sqlCronFunctions, sqlJobFunctions}
var sqlNames = []string{"DDL", "JSON Schema", "Cron Functions", "Job Functions"}

// New opens connection and creates schema
func New(ctx context.Context, cmdOpts config.CmdOptions, logger log.LoggerHookerIface) (*PgEngine, error) {
	var wt int = WaitTime
	var err error
	pge := &PgEngine{
		logger,
		nil,
		cmdOpts,
		make(chan ChainSignal, 64),
	}
	pge.l.WithField("PID", os.Getpid()).Debug("Starting new session... ")
	connctx, conncancel := context.WithTimeout(ctx, time.Duration(cmdOpts.Connection.Timeout)*time.Second)
	defer conncancel()

	config := pge.getPgxConnConfig()
	pge.ConfigDb, err = pgxpool.ConnectConfig(connctx, config)
	if connctx.Err() != nil {
		pge.l.WithError(connctx.Err()).Error("Connection cancelled")
		return nil, connctx.Err()
	}
	for err != nil {
		pge.l.WithError(err).Error("Connection failed")
		pge.l.Info("Reconnecting in ", wt, " sec...")
		select {
		case <-time.After(time.Duration(wt) * time.Second):
			pge.ConfigDb, err = pgxpool.ConnectConfig(connctx, config)
		case <-connctx.Done():
			pge.l.WithError(connctx.Err()).Error("Connection request cancelled")
			return nil, connctx.Err()
		}
		if wt < maxWaitTime {
			wt = wt * 2
		}
	}
	pge.l.Info("Database connection established")
	if err := pge.ExecuteSchemaScripts(ctx); err != nil {
		return nil, err
	}
	pge.AddLogHook(ctx) //schema exists, we can log now
	if cmdOpts.Start.File != "" {
		if err := pge.ExecuteCustomScripts(ctx, cmdOpts.Start.File); err != nil {
			return nil, err
		}
	}
	return pge, nil
}

// NewDB creates pgengine instance for already opened database connection, allowing to bypass a parameters based credentials.
// We assume here all checks for proper schema validation are done beforehannd
func NewDB(DB PgxPoolIface, args ...string) *PgEngine {
	return &PgEngine{log.Init(config.LoggingOpts{LogLevel: "error"}), DB, *config.NewCmdOptions(args...), make(chan ChainSignal, 64)}
}

// getPgxConnConfig transforms standard connestion string to pgx specific one with
func (pge *PgEngine) getPgxConnConfig() *pgxpool.Config {
	var connstr string
	if pge.Connection.PgURL != "" {
		connstr = pge.Connection.PgURL
	} else {
		connstr = fmt.Sprintf("host='%s' port='%d' dbname='%s' sslmode='%s' user='%s'",
			pge.Connection.Host, pge.Connection.Port, pge.Connection.DBName, pge.Connection.SSLMode, pge.Connection.User)
		if pge.Connection.Password != "" {
			connstr = connstr + fmt.Sprintf(" password='%s'", pge.Connection.Password)
		}
	}
	connConfig, err := pgxpool.ParseConfig(connstr)
	if err != nil {
		pge.l.WithError(err).Error("Cannot parse connection string")
		return nil
	}
	// in the worst scenario we need separate connections for each of workers,
	// separate connection for Scheduler.retrieveChainsAndRun(),
	// separate connection for Scheduler.retrieveIntervalChainsAndRun(),
	// and another connection for LogHook.send()
	connConfig.MaxConns = int32(pge.Resource.CronWorkers) + int32(pge.Resource.IntervalWorkers) + 3
	connConfig.ConnConfig.RuntimeParams["application_name"] = "pg_timetable"
	connConfig.ConnConfig.OnNotice = func(c *pgconn.PgConn, n *pgconn.Notice) {
		pge.l.WithField("severity", n.Severity).WithField("notice", n.Message).Info("Notice received")
	}
	connConfig.AfterConnect = func(ctx context.Context, pgconn *pgx.Conn) (err error) {
		pge.l.WithField("ConnPID", pgconn.PgConn().PID()).
			WithField("client", pge.ClientName).
			Debug("Trying to get lock for the session")
		if err = pge.TryLockClientName(ctx, pgconn); err != nil {
			return err
		}
		_, err = pgconn.Exec(ctx, "LISTEN "+quoteIdent(pge.ClientName))
		return err
	}
	if !pge.Start.Debug { //will handle notification in HandleNotifications directly
		connConfig.ConnConfig.OnNotification = pge.NotificationHandler
	}
	connConfig.ConnConfig.Logger = log.NewPgxLogger(pge.l)
	if pge.Verbose() {
		connConfig.ConnConfig.LogLevel = pgx.LogLevelDebug
	} else {
		connConfig.ConnConfig.LogLevel = pgx.LogLevelWarn
	}
	return connConfig
}

// AddLogHook adds a new pgx log hook to logrus logger
func (pge *PgEngine) AddLogHook(ctx context.Context) {
	pge.l.AddHook(NewHook(ctx, pge.ConfigDb, pge.ClientName, 500, pge.Logging.LogDBLevel))
}

// QueryRowIface specifies interface to use QueryRow method
type QueryRowIface interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

// TryLockClientName obtains lock on the server to prevent another client with the same name
func (pge *PgEngine) TryLockClientName(ctx context.Context, conn QueryRowIface) error {
	// check if the schema is available already first
	var procoid int
	err := conn.QueryRow(ctx, "SELECT COALESCE(to_regproc('timetable.try_lock_client_name')::int4, 0)").Scan(&procoid)
	if err != nil {
		return err
	}
	if procoid == 0 {
		//there is no schema yet, will lock after bootstrapping
		pge.l.Debug("There is no schema yet, will lock after bootstrapping")
		return nil
	}
	var wt int = WaitTime
	for {
		sql := fmt.Sprintf("SELECT timetable.try_lock_client_name(%d, $worker$%s$worker$)", os.Getpid(), pge.ClientName)
		var locked bool
		err = conn.QueryRow(ctx, sql).Scan(&locked)
		if err != nil {
			pge.l.WithError(err).Error("Client name locking failed")
			return err
		} else if !locked {
			pge.l.Info("Cannot obtain lock for a session")
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
			pge.l.WithError(err).Error("Cannot read command file")
			return err
		}
		pge.l.Info("Executing script: ", f)
		if _, err = pge.ConfigDb.Exec(ctx, string(sql)); err != nil {
			pge.l.WithError(err).Error("Script execution failed")
			return err
		}
		pge.l.Info("Script file executed: ", f)
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
			pge.l.Info("Executing script: ", sqlName)
			if _, err = pge.ConfigDb.Exec(ctx, sql); err != nil {
				pge.l.WithError(err).Error("Script execution failed")
				pge.l.Warn("Dropping \"timetable\" schema...")
				_, err = pge.ConfigDb.Exec(ctx, "DROP SCHEMA IF EXISTS timetable CASCADE")
				if err != nil {
					pge.l.WithError(err).Error("Schema dropping failed")
				}
				return err
			}
			pge.l.Info("Schema file executed: " + sqlName)
		}
		pge.l.Info("Configuration schema created...")
	}
	return nil
}

// Finalize closes session
func (pge *PgEngine) Finalize() {
	pge.l.Info("Closing session")
	_, err := pge.ConfigDb.Exec(context.Background(), "DELETE FROM timetable.active_session WHERE client_pid = $1 AND client_name = $2", os.Getpid(), pge.ClientName)
	if err != nil {
		pge.l.WithError(err).Error("Cannot finalize database session")
	}
	pge.ConfigDb.Close()
	pge.ConfigDb = nil
}

//ReconnectAndFixLeftovers keeps trying reconnecting every `waitTime` seconds till connection established
func (pge *PgEngine) ReconnectAndFixLeftovers(ctx context.Context) bool {
	for pge.ConfigDb.Ping(ctx) != nil {
		pge.l.Info("Connection to the server was lost. Waiting for ", WaitTime, " sec...")
		select {
		case <-time.After(WaitTime * time.Second):
			pge.l.Info("Reconnecting...")
		case <-ctx.Done():
			pge.l.WithError(ctx.Err()).Info("Request cancelled")
			return false
		}
	}
	pge.l.Info("Connection reestablished...")
	pge.FixSchedulerCrash(ctx)
	return true
}
