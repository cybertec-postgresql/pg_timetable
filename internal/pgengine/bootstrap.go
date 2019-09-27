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
	ConfigDb = sqlx.MustConnect("postgres", fmt.Sprintf("host=%s port=%s dbname=%s sslmode=%s user=%s password=%s",
		host, port, dbname, sslmode, user, password))

	var exists bool
	err := ConfigDb.Get(&exists, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'timetable')")
	if err != nil || !exists {
		for _, schemafile := range schemafiles {
			CreateConfigDBSchema(schemafile)
		}
		LogToDB("LOG", "Configuration schema created...")
	}
	LogToDB("LOG", "Connection established...")
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
	if err := ConfigDb.Close(); err != nil {
		log.Fatalln("Cannot close database connection:", err)
	}
	ConfigDb = nil
}

//ReconnectDbAndFixLeftOvers try to reconnect to database forever after each 5 seconds
func ReconnectDbAndFixLeftOvers() bool {
	//Ping the remote server to make sure it's alive. Non-nil return value means that there is no active connection.
	connectionError := ConfigDb.Ping()
	if connectionError == nil {
		return false
	}
	//In case of DB Outage, Keep trying reconnecting after each 5 second till connection established
	sslMode := "disable"
	for connectionError != nil {
		log.Println("Connection to Postgres was lost. Waiting for 5s ...")
		time.Sleep(waitTime * time.Second)
		log.Println("Reconnecting...")
		ConfigDb, err := sqlx.Connect("postgres", fmt.Sprintf("host=%s port=%s dbname=%s sslmode=%s user=%s password=%s",
			Host, Port, DbName, sslMode, User, Password))
		if err == nil {
			if connectionError = ConfigDb.Ping(); connectionError == nil {
				LogToDB("LOG", "Connection established...")
				/* cleanup potential database leftovers */
				FixSchedulerCrash()
			}
		}
	}
	return true
}
