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
	ClientName string `short:"c" long:"clientname" description:"Unique name for application instance" required:"True"`
	Verbose    bool   `short:"v" long:"verbose" description:"Show verbose debug information" env:"PGTT_VERBOSE"`
	Host       string `short:"h" long:"host" description:"PG config DB host" default:"localhost" env:"PGTT_PGHOST"`
	Port       string `short:"p" long:"port" description:"PG config DB port" default:"5432" env:"PGTT_PGPORT"`
	Dbname     string `short:"d" long:"dbname" description:"PG config DB dbname" default:"timetable" env:"PGTT_PGDATABASE"`
	User       string `short:"u" long:"user" description:"PG config DB user" default:"scheduler" env:"PGTT_PGUSER"`
	File       string `short:"f" long:"file" description:"Config file only mode" hidden:"TODO"`
	Password   string `long:"password" description:"PG config DB password" env:"PGCB_PGPASSWORD"`
	SSLMode    string `long:"sslmode" default:"disable" description:"What SSL priority use for connection" choice:"disable" choice:"require"`
}

var cmdOpts cmdOptions

func main() {
	parser := flags.NewParser(&cmdOpts, flags.PrintErrors)
	if _, err := parser.Parse(); err != nil {
		if !flags.WroteHelp(err) {
			parser.WriteHelp(os.Stdout)
			os.Exit(2)
		}
	}
	pgengine.ClientName = cmdOpts.ClientName
	pgengine.VerboseLogLevel = cmdOpts.Verbose
	pgengine.Host = cmdOpts.Host
	pgengine.Port = cmdOpts.Port
	pgengine.DbName = cmdOpts.Dbname
	pgengine.User = cmdOpts.User
	pgengine.Password = cmdOpts.Password
	pgengine.SSLMode = cmdOpts.SSLMode
	pgengine.PrefixSchemaFiles("sql/")
	pgengine.InitAndTestConfigDBConnection(cmdOpts.Host, cmdOpts.Port,
		cmdOpts.Dbname, cmdOpts.User, cmdOpts.Password, cmdOpts.SSLMode, pgengine.SQLSchemaFiles)
	pgengine.LogToDB("LOG", fmt.Sprintf("Starting new session with options: %+v", cmdOpts))
	defer pgengine.FinalizeConfigDBConnection()

	// Setup our Ctrl+C handler
	pgengine.SetupCloseHandler()

	scheduler.Run()
	return
}
