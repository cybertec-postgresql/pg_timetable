package pgengine

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/lib/pq"
)

// wait for 5 sec before reconnecting to DB
const waitTime = 5

// maximum wait time before reconnect attempts
const maxWaitTime = waitTime * 16

// ConfigDb is the global database object
var ConfigDb *sqlx.DB

// Host is used to reconnect to data base
var Host string = "localhost"

// Port is used to reconnect to data base
var Port string = "5432"

// DbName is used to reconnect to data base
var DbName string = "timetable"

// User is used to reconnect to data base
var User string = "scheduler"

// Password is used to Reconnect Data base
var Password string = "somestrong"

// ClientName is unique ifentifier of the scheduler application running
var ClientName string

// SSLMode parameter determines whether or with what priority a secure SSL TCP/IP connection will
// be negotiated with the server
var SSLMode string = "disable"

// Upgrade parameter specifies if database should be upgraded to latest version
var Upgrade bool

// NoShellTasks parameter disables SHELL tasks executing
var NoShellTasks bool

var sqls = []string{sqlDDL, sqlJSONSchema, sqlTasks, sqlJobFunctions}
var sqlNames = []string{"DDL", "JSON Schema", "Built-in Tasks", "Job Functions"}

// InitAndTestConfigDBConnection opens connection and creates schema
func InitAndTestConfigDBConnection(ctx context.Context) bool {
	var wt int = waitTime
	var err error
	connstr := fmt.Sprintf("application_name=pg_timetable host='%s' port='%s' dbname='%s' sslmode='%s' user='%s' password='%s'",
		Host, Port, DbName, SSLMode, User, Password)

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
		fmt.Printf(GetLogPrefixLn("ERROR")+"\n", err)
		fmt.Printf(GetLogPrefixLn("LOG"), fmt.Sprintf("Reconnecting in %d sec...", wt))
		select {
		case <-time.After(time.Duration(wt) * time.Second):
			err = db.PingContext(ctx)
		case <-ctx.Done():
			// If the request gets cancelled, log it
			LogToDB("ERROR", "request cancelled\n")
			return false
		}
		if wt < maxWaitTime {
			wt = wt * 2
		}
	}

	ConfigDb = sqlx.NewDb(db, "postgres")
	LogToDB("LOG", "Connection established...")
	LogToDB("LOG", fmt.Sprintf("Proceeding as '%s' with client PID %d", ClientName, os.Getpid()))

	var exists bool
	err = ConfigDb.GetContext(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'timetable')")
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
			} else {
				LogToDB("LOG", "Schema file executed: "+sqlName)
			}
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
func ReconnectDbAndFixLeftovers() {
	var err error
	for {
		fmt.Printf(GetLogPrefixLn("REPAIR"), fmt.Sprintf("Connection to the server was lost. Waiting for %d sec...", waitTime))
		time.Sleep(waitTime * time.Second)
		fmt.Printf(GetLogPrefix("REPAIR"), "Reconnecting...\n")
		err = ConfigDb.Ping()
		if err == nil {
			LogToDB("LOG", "Connection reestablished...")
			FixSchedulerCrash()
			break
		}
	}
}
