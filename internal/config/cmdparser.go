package config

import (
	"io"
	"os"

	flags "github.com/jessevdk/go-flags"
)

// ConnectionOpts specifies the database connection options
type ConnectionOpts struct {
	Host     string `short:"h" long:"host" description:"PostgreSQL host" default:"localhost" env:"PGTT_PGHOST"`
	Port     int    `short:"p" long:"port" description:"PostgreSQL port" default:"5432" env:"PGTT_PGPORT"`
	DBName   string `short:"d" long:"dbname" description:"PostgreSQL database name" default:"timetable" env:"PGTT_PGDATABASE"`
	User     string `short:"u" long:"user" description:"PostgreSQL user" default:"scheduler" env:"PGTT_PGUSER"`
	Password string `long:"password" description:"PostgreSQL user password" env:"PGTT_PGPASSWORD"`
	SSLMode  string `long:"sslmode" default:"disable" description:"What SSL priority use for connection" choice:"disable" choice:"require"`
	PgURL    string `long:"pgurl" description:"PostgreSQL connection URL" env:"PGTT_URL"`
}

// LoggingOpts specifies the logging configuration
type LoggingOpts struct {
	LogLevel      string `long:"log-level" description:"Verbosity level for stdout and log file" choice:"debug" choice:"info" choice:"error" default:"info"`
	LogDBLevel    string `long:"log-database-level" description:"Verbosity level for database storing" choice:"debug" choice:"info" choice:"error" default:"info"`
	LogFile       string `long:"log-file" description:"File name to store logs"`
	LogFileFormat string `long:"log-file-format" description:"Format of file logs" choice:"json" choice:"text" default:"json"`
}

// StartOpts specifies the application startup options
type StartOpts struct {
	File    string `short:"f" long:"file" description:"SQL script file to execute during startup"`
	Init    bool   `long:"init" description:"Initialize database schema to the latest version and exit. Can be used with --upgrade"`
	Upgrade bool   `long:"upgrade" description:"Upgrade database to the latest version"`
	Debug   bool   `long:"debug" description:"Run in debug mode. Only asynchronous chains will be executed"`
}

// ResourceOpts specifies the maximum resources available to application
type ResourceOpts struct {
	CronWorkers     int `long:"cron-workers" description:"Number of parallel workers for scheduled chains" default:"16"`
	IntervalWorkers int `long:"interval-workers" description:"Number of parallel workers for interval chains" default:"16"`
	ChainTimeout    int `long:"chain-timeout" description:"Abort any chain that takes more than the specified number of milliseconds"`
}

// CmdOptions holds command line options passed
type CmdOptions struct {
	ClientName     string         `short:"c" long:"clientname" description:"Unique name for application instance" env:"PGTT_CLIENTNAME"`
	Config         string         `long:"config" description:"YAML configuration file"`
	Connection     ConnectionOpts `group:"Connection" mapstructure:"Connection"`
	Logging        LoggingOpts    `group:"Logging" mapstructure:"Logging"`
	Start          StartOpts      `group:"Start" mapstructure:"Start"`
	Resource       ResourceOpts   `group:"Resource" mapstructure:"Resource"`
	NoProgramTasks bool           `long:"no-program-tasks" mapstructure:"no-program-tasks" description:"Disable executing of PROGRAM tasks" env:"PGTT_NOPROGRAMTASKS"`
	NoHelpMessage  bool           `long:"no-help" mapstructure:"no-help" hidden:"system use"`
}

// Verbose returns true if the debug log is enabled
func (c CmdOptions) Verbose() bool {
	return c.Logging.LogLevel == "debug"
}

// NewCmdOptions returns a new instance of CmdOptions with default values
func NewCmdOptions(args ...string) *CmdOptions {
	cmdOpts := new(CmdOptions)
	_, _ = flags.NewParser(cmdOpts, flags.PrintErrors).ParseArgs(args)
	return cmdOpts
}

var nonOptionArgs []string

// Parse will parse command line arguments and initialize pgengine
func Parse(writer io.Writer) (*flags.Parser, error) {
	cmdOpts := new(CmdOptions)
	parser := flags.NewParser(cmdOpts, flags.PrintErrors)
	var err error
	if nonOptionArgs, err = parser.Parse(); err != nil {
		if !flags.WroteHelp(err) && !cmdOpts.NoHelpMessage {
			parser.WriteHelp(writer)
			return nil, err
		}
	}
	if cmdOpts.Start.File != "" {
		if _, err := os.Stat(cmdOpts.Start.File); os.IsNotExist(err) {
			return nil, err
		}
	}
	//non-option arguments
	if len(nonOptionArgs) > 0 && cmdOpts.Connection.PgURL == "" {
		cmdOpts.Connection.PgURL = nonOptionArgs[0]
	}
	return parser, nil
}
