package pgengine

import (
	"database/sql"
	"fmt"
	"io/ioutil"
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

// SQLSchemaFiles contains the names of the files should be executed during bootstrap
var SQLSchemaFiles = []string{"ddl.sql", "json-schema.sql", "tasks.sql", "job-functions.sql"}

//PrefixSchemaFiles adds specific path for bootstrap SQL schema files
func PrefixSchemaFiles(prefix string) {
	for i := 0; i < len(SQLSchemaFiles); i++ {
		SQLSchemaFiles[i] = prefix + SQLSchemaFiles[i]
	}
}

// InitAndTestConfigDBConnection opens connection and creates schema
func InitAndTestConfigDBConnection(schemafiles []string) {
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

	err = db.Ping()
	for err != nil {
		fmt.Printf(GetLogPrefixLn("ERROR")+"\n", err)
		fmt.Printf(GetLogPrefixLn("LOG"), fmt.Sprintf("Reconnecting in %d sec...", wt))
		time.Sleep(time.Duration(wt) * time.Second)
		err = db.Ping()
		if wt < maxWaitTime {
			wt = wt * 2
		}
	}

	ConfigDb = sqlx.NewDb(db, "postgres")
	LogToDB("LOG", "Connection established...")

	var exists bool
	err = ConfigDb.Get(&exists, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'timetable')")
	if err != nil || !exists {
		for _, schemafile := range schemafiles {
			fmt.Printf(GetLogPrefixLn("LOG"), "Executing script: "+schemafile)
			if err = CreateConfigDBSchema(schemafile); err != nil {
				fmt.Printf(GetLogPrefixLn("PANIC"), err)
				fmt.Printf(GetLogPrefixLn("PANIC"), "Dropping \"timetable\" schema")
				_, err = ConfigDb.Exec("DROP SCHEMA IF EXISTS timetable CASCADE")
				os.Exit(2)
			} else {
				LogToDB("LOG", "Schema file executed: "+schemafile)
			}
		}
		LogToDB("LOG", "Configuration schema created...")
	}
}

// CreateConfigDBSchema executes SQL script from file
func CreateConfigDBSchema(schemafile string) (err error) {
	b, err := ioutil.ReadFile(schemafile) // nolint: gosec
	if err != nil {
		return
	}
	_, err = ConfigDb.Exec(string(b))
	return
}

// FinalizeConfigDBConnection closes session
func FinalizeConfigDBConnection() {
	LogToDB("LOG", "Closing session")
	_, err := ConfigDb.Exec("SELECT pg_advisory_unlock_all()")
	if err != nil {
		log.Println("Error occured during locks releasing: ", err)
	}
	if err = ConfigDb.Close(); err != nil {
		log.Println("Error occured during connection closing: ", err)
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
		ConfigDb, err = sqlx.Connect("postgres", fmt.Sprintf("host=%s port=%s dbname=%s sslmode=%s user=%s password=%s",
			Host, Port, DbName, SSLMode, User, Password))
		if err == nil {
			LogToDB("LOG", "Connection reestablished...")
			FixSchedulerCrash()
			break
		}
	}
}
