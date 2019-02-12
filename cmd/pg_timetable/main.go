package main

import (
	"fmt"
	"os"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/scheduler"
	flags "github.com/jessevdk/go-flags"
)

/**
 * pg_timetable is the daemon application responsible to execute scheduled SQL tasks that cannot be triggered by the
 * PostgreSQL server (PostgreSQL does not support time triggers).
 *
 * This application may run in the same machine as PostgreSQL server and must grant full access permission to the
 * timetable tables.
 */

type cmdOptions struct {
	Verbose    bool   `short:"v" long:"verbose" description:"Show verbose debug information" env:"PGTT_VERBOSE"`
	Host       string `short:"h" long:"host" description:"PG config DB host" default:"localhost" env:"PGTT_PGHOST"`
	Port       string `short:"p" long:"port" description:"PG config DB port" default:"5432" env:"PGTT_PGPORT"`
	Dbname     string `short:"d" long:"dbname" description:"PG config DB dbname" default:"timetable" env:"PGTT_PGDATABASE"`
	User       string `short:"u" long:"user" description:"PG config DB user" default:"scheduler" env:"PGTT_PGUSER"`
	File       string `short:"f" long:"file" description:"Config file only mode"`
	Password   string `long:"password" description:"PG config DB password" env:"PGCB_PGPASSWORD"`
	ClientName string `short:"c" long:"name" description:"Unique name for application instance"`
}

var cmdOpts cmdOptions

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	parser := flags.NewParser(&cmdOpts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		panic(err)
	}
	pgengine.VerboseLogLevel = cmdOpts.Verbose
	if cmdOpts.Verbose {
		fmt.Printf("%+v\n", cmdOpts)
	}
	pgengine.InitAndTestConfigDBConnection(cmdOpts.Host, cmdOpts.Port, cmdOpts.Dbname, cmdOpts.User, cmdOpts.Password, "disable", pgengine.SQLSchemaFile)
	pgengine.LogToDB("LOG", fmt.Sprintf("Starting new session with options: %+v\n", cmdOpts))
	defer pgengine.FinalizeConfigDBConnection()
	scheduler.Run()
	return
}

/* display the syntax of this program */
func usage() {
	fmt.Print(`
pg_timetable - task scheduler and executor
Usage: pg_timetable [options] 
  -f: configuration file used to configure the connect string.
  -d  defines the number of days we want to keep in the run log
  -h  display usage
Reports bugs to https://github.com/cybertec-postgresql/pg_timetable`)
	return
}
