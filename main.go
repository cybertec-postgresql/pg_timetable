package main

import (
	"fmt"
	"net"
	"net/url"
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
	ClientName  string `short:"c" long:"clientname" description:"Unique name for application instance" required:"True"`
	Verbose     bool   `short:"v" long:"verbose" description:"Show verbose debug information" env:"PGTT_VERBOSE"`
	Host        string `short:"h" long:"host" description:"PG config DB host" default:"localhost" env:"PGTT_PGHOST"`
	Port        string `short:"p" long:"port" description:"PG config DB port" default:"5432" env:"PGTT_PGPORT"`
	Dbname      string `short:"d" long:"dbname" description:"PG config DB dbname" default:"timetable" env:"PGTT_PGDATABASE"`
	User        string `short:"u" long:"user" description:"PG config DB user" default:"scheduler" env:"PGTT_PGUSER"`
	File        string `short:"f" long:"file" description:"Config file only mode" hidden:"TODO"`
	Password    string `long:"password" description:"PG config DB password" env:"PGCB_PGPASSWORD"`
	SSLMode     string `long:"sslmode" default:"disable" description:"What SSL priority use for connection" choice:"disable" choice:"require"`
	PostgresURL DbURL  `long:"pgurl" description:"Postgres url" env:"POSTGRES_URL" default:"user=scheduler host=localhost port=5432 dbname=timetable sslmode=disable"`
}

//DbURL PostgreSQL connection URL
type DbURL struct {
	pgurl *url.URL
}

var cmdOpts cmdOptions
var nonOptionArgs []string

//UnmarshalFlag parses commandline string in to url
func (d *DbURL) UnmarshalFlag(s string) error {
	var err error
	d.pgurl, err = url.Parse(s)
	return err
}

//ParseCurl parses URL structure into cmdOptions
func (c *cmdOptions) ParseCurl(cmdURL *url.URL) {
	var err error
	c.Host, c.Port, err = net.SplitHostPort(cmdURL.Host)
	// Restore default values
	if err != nil {
		c.Host = "localhost"
		c.Port = "5432"
	}
	if cmdURL.User != nil {
		c.User = cmdURL.User.Username()
		c.Password, _ = cmdURL.User.Password()
	}

	if strings.TrimSpace(cmdURL.Path) != "" {
		c.Dbname = cmdURL.Path[1:]
	} else {
		//restore default
		c.Dbname = "timetable"
	}
}

func main() {
	parser := flags.NewParser(&cmdOpts, flags.PrintErrors)
	var err error
	if nonOptionArgs, err = parser.Parse(); err != nil {
		if !flags.WroteHelp(err) {
			parser.WriteHelp(os.Stdout)
			os.Exit(2)
		}
	}
	//--postgres_url option
	if cmdOpts.PostgresURL.pgurl.IsAbs() {
		cmdOpts.ParseCurl(cmdOpts.PostgresURL.pgurl)
	}
	//non option arguments
	if len(nonOptionArgs) > 0 {
		nonOptionURL, err := url.Parse(strings.Join(nonOptionArgs, ""))
		if err == nil {
			cmdOpts.ParseCurl(nonOptionURL)
		}
	}
	//connection string in dbname
	if strings.Contains(cmdOpts.Dbname, "postgres") || strings.Contains(cmdOpts.Dbname, "postgresql") {
		connStrInDb, err := url.Parse(cmdOpts.Dbname)
		if err == nil {
			cmdOpts.ParseCurl(connStrInDb)
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
	pgengine.InitAndTestConfigDBConnection(pgengine.SQLSchemaFiles)
	pgengine.LogToDB("LOG", fmt.Sprintf("Starting new session with options: %+v", cmdOpts))
	defer pgengine.FinalizeConfigDBConnection()

	// Setup our Ctrl+C handler
	pgengine.SetupCloseHandler()

	scheduler.Run()
	return
}
