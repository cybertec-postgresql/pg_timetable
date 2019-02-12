package pgengine

import (
	"fmt"
	"io/ioutil"
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // postgresql driver blank import
)

// ConfigDb is the global database object
var ConfigDb *sqlx.DB

// SQLSchemaFile contains the name of the file should be executed during bootstrap
const SQLSchemaFile string = "ddl.sql"

// InitAndTestConfigDBConnection opens connection and creates schema
func InitAndTestConfigDBConnection(host, port, dbname, user, password, sslmode, schemafile string) {
	ConfigDb = sqlx.MustConnect("postgres", fmt.Sprintf("host=%s port=%s dbname=%s sslmode=%s user=%s password=%s",
		host, port, dbname, sslmode, user, password))

	var exists bool
	err := ConfigDb.Get(&exists, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'timetable')")
	if err != nil || !exists {
		createConfigDBSchema(schemafile)
		LogToDB(0, "LOG", "Configuration schema created...")
	}
	LogToDB(0, "LOG", "Connection established...")
}

func createConfigDBSchema(schemafile string) {
	b, err := ioutil.ReadFile(schemafile) // nolint: gosec
	if err != nil {
		log.Fatalln("Cannot open schema file.", err)
	}
	_, err = ConfigDb.Exec(string(b))
	if err != nil {
		log.Fatalln(err)
	}
	LogToDB(0, "LOG", fmt.Sprintf("Created timetable schema from file: %s", schemafile))
}

// FinalizeConfigDBConnection closes session
func FinalizeConfigDBConnection() {
	LogToDB(0, "LOG", "Closing session")
	if err := ConfigDb.Close(); err != nil {
		log.Fatalln("Cannot close database connection:", err)
	}
}
