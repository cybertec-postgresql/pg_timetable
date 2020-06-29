package cmdparser

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	flags "github.com/jessevdk/go-flags"
)

type CmdOptions struct {
	ClientName    string `short:"c" long:"clientname" description:"Unique name for application instance" required:"True"`
	Verbose       bool   `short:"v" long:"verbose" description:"Show verbose debug information" env:"PGTT_VERBOSE"`
	Host          string `short:"h" long:"host" description:"PG config DB host" default:"localhost" env:"PGTT_PGHOST"`
	Port          string `short:"p" long:"port" description:"PG config DB port" default:"5432" env:"PGTT_PGPORT"`
	Dbname        string `short:"d" long:"dbname" description:"PG config DB dbname" default:"timetable" env:"PGTT_PGDATABASE"`
	User          string `short:"u" long:"user" description:"PG config DB user" default:"scheduler" env:"PGTT_PGUSER"`
	File          string `short:"f" long:"file" description:"SQL script file to execute during startup"`
	Password      string `long:"password" description:"PG config DB password" default:"somestrong" env:"PGTT_PGPASSWORD"`
	SSLMode       string `long:"sslmode" default:"disable" description:"What SSL priority use for connection" choice:"disable" choice:"require"`
	Init          bool   `long:"init" description:"Initialize database schema to the latest version and exit. Can be used with --upgrade"`
	Upgrade       bool   `long:"upgrade" description:"Upgrade database to the latest version"`
	NoShellTasks  bool   `long:"no-shell-tasks" description:"Disable executing of shell tasks" env:"PGTT_NOSHELLTASKS"`
	NoHelpMessage bool   `long:"no-help" hidden:"system use"`
	Debug         bool   `long:"debug" description:"Run in debug mode. Only asynchronous chains will be executed"`
	PostgresURL   DbURL  `long:"pgurl" description:"PG config DB url" env:"PGTT_URL"`
}

// NewCmdOptions returns a new instance of CmdOptions with default values
func NewCmdOptions(ClientName string) *CmdOptions {
	cmdOpts := new(CmdOptions)
	_, _ = flags.NewParser(cmdOpts, flags.Default).ParseArgs([]string{"-c", ClientName})
	return cmdOpts
}

func (c *CmdOptions) String() string {
	s := fmt.Sprintf("Client:%s Verbose:%t Host:%s:%s DB:%s User:%s ",
		c.ClientName, c.Verbose, c.Host, c.Port, c.Dbname, c.User)
	if c.PostgresURL.pgurl != nil {
		s = s + "URL:" + c.PostgresURL.pgurl.String()
	}
	return s
}

//DbURL PostgreSQL connection URL
type DbURL struct {
	pgurl *url.URL
}

var nonOptionArgs []string

//UnmarshalFlag parses commandline string in to url
func (d *DbURL) UnmarshalFlag(s string) error {
	var err error
	d.pgurl, err = url.Parse(s)
	return err
}

//ParseCurl parses URL structure into CmdOptions
func (c *CmdOptions) ParseCurl(cmdURL *url.URL) error {
	var err error
	if cmdURL == nil {
		return nil
	}
	if !strings.HasPrefix(cmdURL.Scheme, "postgres") {
		return fmt.Errorf("Incorrect URI scheme: %s. "+
			"The URI scheme designator can be either postgresql:// or postgres://", cmdURL.Scheme)
	}
	if strings.Contains(cmdURL.Host, ":") {
		c.Host, c.Port, err = net.SplitHostPort(cmdURL.Host)
		if err != nil {
			return err
		}
	} else {
		c.Host = cmdURL.Host
	}

	if cmdURL.User != nil {
		c.User = cmdURL.User.Username()
		c.Password, _ = cmdURL.User.Password()
	}

	if strings.TrimSpace(cmdURL.Path) != "" {
		c.Dbname = cmdURL.Path[1:]
	}

	a, _ := url.ParseQuery(cmdURL.RawQuery)
	if len(a["sslmode"]) > 0 {
		c.SSLMode = a["sslmode"][0]
	}
	return nil
}

func isPostgresURI(s string) bool {
	return strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://")
}

// Parse will parse command line arguments and initialize pgengine
func Parse() (*CmdOptions, error) {
	cmdOpts := new(CmdOptions)
	parser := flags.NewParser(cmdOpts, flags.PrintErrors)
	var err error
	if nonOptionArgs, err = parser.Parse(); err != nil {
		if !flags.WroteHelp(err) && !cmdOpts.NoHelpMessage {
			parser.WriteHelp(os.Stdout)
			return nil, err
		}
	}
	if cmdOpts.File != "" {
		if _, err := os.Stat(cmdOpts.File); os.IsNotExist(err) {
			return nil, err
		}
	}
	//non option arguments
	if len(nonOptionArgs) > 0 && cmdOpts.PostgresURL.pgurl == nil {
		cmdOpts.PostgresURL.pgurl, err = url.Parse(strings.Join(nonOptionArgs, ""))
		if err != nil {
			return nil, err
		}

	}
	//connection string in dbname
	if isPostgresURI(cmdOpts.Dbname) && cmdOpts.PostgresURL.pgurl == nil {
		cmdOpts.PostgresURL.pgurl, err = url.Parse(cmdOpts.Dbname)
		if err != nil {
			return nil, err
		}
	}
	err = cmdOpts.ParseCurl(cmdOpts.PostgresURL.pgurl)
	if err != nil {
		return nil, err
	}
	return cmdOpts, nil
}
