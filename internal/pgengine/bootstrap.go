package pgengine

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/jmoiron/sqlx"

	"github.com/lib/pq"
)

// WaitTime specifies amount of time in seconds to wait before reconnecting to DB
const WaitTime = 5

// maximum wait time before reconnect attempts
const maxWaitTime = WaitTime * 16

// ConfigDb is the global database object
var ConfigDb *sqlx.DB

// ClientName is unique ifentifier of the scheduler application running
var ClientName string

// NoShellTasks parameter disables SHELL tasks executing
var NoShellTasks bool

var sqls = []string{sqlDDL, sqlJSONSchema, sqlTasks, sqlJobFunctions}
var sqlNames = []string{"DDL", "JSON Schema", "Built-in Tasks", "Job Functions"}

// InitAndTestConfigDBConnection opens connection and creates schema
func InitAndTestConfigDBConnection(ctx context.Context, cmdOpts cmdparser.CmdOptions) bool {
	ClientName = cmdOpts.ClientName
	NoShellTasks = cmdOpts.NoShellTasks
	VerboseLogLevel = cmdOpts.Verbose
	LogToDB("DEBUG", fmt.Sprintf("Starting new session... %s", &cmdOpts))
	var wt int = WaitTime
	var err error
	connstr := fmt.Sprintf("application_name='pg_timetable' host='%s' port='%s' dbname='%s' sslmode='%s' user='%s' password='%s'",
		cmdOpts.Host, cmdOpts.Port, cmdOpts.Dbname, cmdOpts.SSLMode, cmdOpts.User, cmdOpts.Password)
	// Base connector to wrap
	base, err := pq.NewConnector(connstr)
	if err != nil {
		log.Fatal(err)
	}
	// Wrap the connector to simply print out the message
	connector := pq.ConnectorWithNoticeHandler(base, func(notice *pq.Error) {
		LogToDB("USER", "Severity: ", notice.Severity, "; Message: ", notice.Message)
	})
	db := sql.OpenDB(connector)
	LogToDB("DEBUG", "Connection string: ", connstr)

	err = db.PingContext(ctx)
	for err != nil {
		LogToDB("ERROR", err)
		LogToDB("LOG", "Reconnecting in ", wt, " sec...")
		select {
		case <-time.After(time.Duration(wt) * time.Second):
			err = db.PingContext(ctx)
		case <-ctx.Done():
			LogToDB("ERROR", "Connection request cancelled: ", ctx.Err())
			return false
		}
		if wt < maxWaitTime {
			wt = wt * 2
		}
	}

	LogToDB("LOG", "Connection established...")
	LogToDB("LOG", fmt.Sprintf("Proceeding as '%s' with client PID %d", ClientName, os.Getpid()))
	ConfigDb = sqlx.NewDb(db, "postgres")

	if !executeSchemaScripts(ctx) {
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
			fmt.Printf(GetLogPrefixLn("PANIC"), err)
			return false
		}
		fmt.Printf(GetLogPrefixLn("LOG"), "Executing script: "+f)
		if _, err = ConfigDb.ExecContext(ctx, string(sql)); err != nil {
			fmt.Printf(GetLogPrefixLn("PANIC"), err)
			return false
		}
		LogToDB("LOG", "Script file executed: "+f)
	}
	return true
}

func executeSchemaScripts(ctx context.Context) bool {
	var exists bool
	err := ConfigDb.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'timetable')")
	if err != nil || !exists {
		for i, sql := range sqls {
			sqlName := sqlNames[i]
			fmt.Printf(GetLogPrefixLn("LOG"), "Executing script: "+sqlName)
			if _, err = ConfigDb.ExecContext(ctx, sql); err != nil {
				fmt.Printf(GetLogPrefixLn("PANIC"), err)
				fmt.Printf(GetLogPrefixLn("PANIC"), "Dropping \"timetable\" schema")
				_, err = ConfigDb.ExecContext(ctx, "DROP SCHEMA IF EXISTS timetable CASCADE")
				if err != nil {
					fmt.Printf(GetLogPrefixLn("PANIC"), err)
				}
				return false
			}
			LogToDB("LOG", "Schema file executed: "+sqlName)
		}
		LogToDB("LOG", "Configuration schema created...")
	}
	return true
}

// FinalizeConfigDBConnection closes session
func FinalizeConfigDBConnection() {
	fmt.Printf(GetLogPrefixLn("LOG"), "Closing session")
	if _, err := ConfigDb.Exec("SELECT pg_advisory_unlock_all()"); err != nil {
		fmt.Printf(GetLogPrefixLn("ERROR"), fmt.Sprintf("Error occurred during locks releasing: %v", err))
	}
	if err := ConfigDb.Close(); err != nil {
		fmt.Printf(GetLogPrefixLn("ERROR"), fmt.Sprintf("Error occurred during connection closing: %v", err))
	}
	ConfigDb = nil
}

//ReconnectDbAndFixLeftovers keeps trying reconnecting every `waitTime` seconds till connection established
func ReconnectDbAndFixLeftovers(ctx context.Context) bool {
	for ConfigDb.PingContext(ctx) != nil {
		fmt.Printf(GetLogPrefixLn("REPAIR"),
			fmt.Sprintf("Connection to the server was lost. Waiting for %d sec...", WaitTime))
		select {
		case <-time.After(WaitTime * time.Second):
			fmt.Printf(GetLogPrefix("REPAIR"), "Reconnecting...\n")
		case <-ctx.Done():
			fmt.Printf(GetLogPrefixLn("ERROR"), fmt.Sprintf("request cancelled: %v", ctx.Err()))
			return false
		}
	}
	LogToDB("LOG", "Connection reestablished...")
	FixSchedulerCrash(ctx)
	return true
}
