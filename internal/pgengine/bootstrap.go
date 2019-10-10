package pgengine

import (
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // postgresql driver blank import
)

// wait for 5 sec before reconnecting to DB
const waitTime = 5

// maximum wait time before reconnect attempts
const maxWaitTime = waitTime * 16

// ConfigDb is the global database object
var ConfigDb *sqlx.DB

// Host is used to reconnect to data base
var Host string

// Port is used to reconnect to data base
var Port string

// DbName is used to reconnect to data base
var DbName string

// User is used to reconnect to data base
var User string

// Password is used to Reconnect Data base
var Password string

// ClientName is unique ifentifier of the scheduler application running
var ClientName string

// SSLMode parameter determines whether or with what priority a secure SSL TCP/IP connection will
// be negotiated with the server
var SSLMode string

// SQLSchemaFiles contains the names of the files should be executed during bootstrap
var SQLSchemaFiles = []string{"ddl.sql", "json-schema.sql", "tasks.sql", "job-functions.sql"}

//PrefixSchemaFiles adds specific path for bootstrap SQL schema files
func PrefixSchemaFiles(prefix string) {
	for i := 0; i < len(SQLSchemaFiles); i++ {
		SQLSchemaFiles[i] = prefix + SQLSchemaFiles[i]
	}
}

// InitAndTestConfigDBConnection opens connection and creates schema
func InitAndTestConfigDBConnection(host, port, dbname, user, password, sslmode string, schemafiles []string) {
	var wt int = waitTime
	connstr := fmt.Sprintf("application_name=pg_timetable host='%s' port='%s' dbname='%s' sslmode='%s' user='%s' password='%s'",
		host, port, dbname, sslmode, user, password)
	LogToDB("DEBUG", "Connection string: ", connstr)
	ConfigDb, err := sqlx.Connect("postgres", connstr)
	for err != nil {
		fmt.Printf(GetLogPrefix("ERROR")+"\n", err)
		fmt.Printf(GetLogPrefix("LOG"), fmt.Sprintf("Reconnecting in %d sec...\n", wt))
		time.Sleep(time.Duration(wt) * time.Second)
		ConfigDb, err = sqlx.Connect("postgres", connstr)
		if wt < maxWaitTime {
			wt = wt * 2
		}
	}
	LogToDB("LOG", "Connection established...")

	var exists bool
	err = ConfigDb.Get(&exists, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'timetable')")
	if err != nil || !exists {
		for _, schemafile := range schemafiles {
			CreateConfigDBSchema(schemafile)
		}
		LogToDB("LOG", "Configuration schema created...")
	}
}

// CreateConfigDBSchema executes SQL script from file
func CreateConfigDBSchema(schemafile string) {
	b, err := ioutil.ReadFile(schemafile) // nolint: gosec
	if err != nil {
		panic(err)
	}
	ConfigDb.MustExec(string(b))
	LogToDB("LOG", fmt.Sprintf("Schema file executed: %s", schemafile))
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
		fmt.Printf(GetLogPrefix("REPAIR"), fmt.Sprintf("Connection to the server was lost. Waiting for %d sec...\n", waitTime))
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
