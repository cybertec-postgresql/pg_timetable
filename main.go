package main

import (
	"fmt"
	"os"
	"strings"

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
	ClientName string `short:"c" long:"name" description:"Unique name for application instance"`
	Verbose    bool   `short:"v" long:"verbose" description:"Show verbose debug information" env:"PGTT_VERBOSE"`
	Host       string `short:"h" long:"host" description:"PG config DB host" default:"localhost" env:"PGTT_PGHOST"`
	Port       string `short:"p" long:"port" description:"PG config DB port" default:"5432" env:"PGTT_PGPORT"`
	Dbname     string `short:"d" long:"dbname" description:"PG config DB dbname" default:"timetable" env:"PGTT_PGDATABASE"`
	User       string `short:"u" long:"user" description:"PG config DB user" default:"scheduler" env:"PGTT_PGUSER"`
	File       string `short:"f" long:"file" description:"Config file only mode"`
	Password   string `long:"password" description:"PG config DB password" env:"PGCB_PGPASSWORD"`
	SSLMode    string `long:"sslmode" default:"disable" description:"What SSL priority use for connection" choice:"disable" choice:"require"`
}

var cmdOpts cmdOptions

func main() {
	parser := flags.NewParser(&cmdOpts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		panic(err)
	}
	if len(os.Args) < 2 {
		parser.WriteHelp(os.Stdout)
		return
	}
	if strings.TrimSpace(cmdOpts.ClientName) == "" {
		fmt.Printf(pgengine.GetLogPrefix("VALIDATE"), "Worker is manadtory, Please enter a worker.\n")
		return
	}
	pgengine.ClientName = cmdOpts.ClientName
	pgengine.VerboseLogLevel = cmdOpts.Verbose
	pgengine.Host = cmdOpts.Host
	pgengine.Port = cmdOpts.Port
	pgengine.DbName = cmdOpts.Dbname
	pgengine.User = cmdOpts.User
	pgengine.Password = cmdOpts.Password
	pgengine.SSLMode = cmdOpts.SSLMode
	if cmdOpts.Verbose {
		fmt.Printf("%+v\n", cmdOpts)
	}
	pgengine.PrefixSchemaFiles("sql/")
	pgengine.InitAndTestConfigDBConnection(cmdOpts.Host, cmdOpts.Port,
		cmdOpts.Dbname, cmdOpts.User, cmdOpts.Password, cmdOpts.SSLMode, pgengine.SQLSchemaFiles)
	pgengine.LogToDB("LOG", fmt.Sprintf("Starting new session with options: %+v", cmdOpts))
	defer pgengine.FinalizeConfigDBConnection()

	// Setup our Ctrl+C handler
	pgengine.SetupCloseHandler()

	/*Only one worker can run with a client name */
	if running := pgengine.IsWorkerRunning(); running == true {
		fmt.Printf(pgengine.GetLogPrefix("VALIDATE"), fmt.Sprintf("%s is already running, You can not run duplicate worker.\n", cmdOpts.ClientName))
		return
	}
	pgengine.AddWorkerDetail()
	scheduler.Run()
	return
}
