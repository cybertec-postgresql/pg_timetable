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

// InitAndTestConfigDBConnection opens connection and creates schema
func InitAndTestConfigDBConnection(host, port, dbname, user, password, sslmode, schemafile string) {
	var err error

	ConfigDb, err = sqlx.Connect("postgres", fmt.Sprintf("host=%s port=%s dbname=%s sslmode=%s user=%s password=%s",
		host, port, dbname, sslmode, user, password))
	if err != nil {
		log.Fatalln("Could not open configDb connection! Exit.", err)
	}

	if err := ConfigDb.Ping(); err != nil {
		log.Fatalln(err)
	}

	var exists bool
	row := ConfigDb.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'pg_timetable')")
	if err := row.Scan(&exists); err != nil {
		log.Fatalln(err)
	}
	if !exists {
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
	LogToDB(0, "LOG", fmt.Sprintf("Created pg_timetable schema from file: %s", schemafile))
}

// FinalizeConfigDBConnection closes session
func FinalizeConfigDBConnection() {
	LogToDB(0, "LOG", "Closing session")
	if err := ConfigDb.Close(); err != nil {
		log.Fatalln("Cannot close database connection:", err)
	}
}
