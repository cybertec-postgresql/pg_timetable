package cmdparser

import (
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFail(t *testing.T) {
	tests := [][]string{
		{0: "go-test"},
		{0: "go-test", "-c", "client01", "foo"},
		{0: "go-test", "-c", "client01", "http://foo.bar/baz"},
		{0: "go-test", "-c", "client01", "--pgurl=http://foo.bar/baz"},
		{0: "go-test", "-c", "client01", "-d", "postgres:// "},
		{0: "go-test", "-c", "client01", "postgres:// "},
		{0: "go-test", "-c", "client01", "postgres://foo@bar:5432:5432/"},
		{0: "go-test", "-c", "client01", "-f", "foo"},
	}
	for _, d := range tests {
		os.Args = d
		_, err := Parse()
		assert.Error(t, err)
	}
}

type data struct {
	args   []string
	result CmdOptions
	msg    string
}

func DbURLFromString(s string) DbURL {
	dburl := DbURL{}
	dburl.pgurl, _ = url.Parse(s)
	return dburl
}

func TestParseSuccessful(t *testing.T) {
	tests := []data{
		{
			args:   []string{0: "go-test", "-c", "client01", "-u", "user", "--password=pwd", "--host=host", "-d", "db"},
			result: CmdOptions{ClientName: "client01", User: "user", Password: "pwd", Port: "5432", Host: "host", Dbname: "db"},
			msg:    "Stimple arguments without URL failed",
		},
		{
			args: []string{0: "go-test", "-c", "client01", "postgres://user:pwd@host/db"},
			result: CmdOptions{ClientName: "client01", User: "user", Password: "pwd", Port: "5432", Host: "host", Dbname: "db",
				PostgresURL: DbURLFromString("postgres://user:pwd@host/db")},
			msg: "Standalone URL without port failed",
		},
		{
			args: []string{0: "go-test", "-c", "client01", "postgresql://user:pwd@host:5455/db"},
			result: CmdOptions{ClientName: "client01", User: "user", Password: "pwd", Port: "5455", Host: "host", Dbname: "db",
				PostgresURL: DbURLFromString("postgresql://user:pwd@host:5455/db")},
			msg: "Standalone URL with port failed",
		},
		{
			args: []string{0: "go-test", "-c", "client01", "-d", "postgres://user:pwd@host/db"},
			result: CmdOptions{ClientName: "client01", User: "user", Password: "pwd", Port: "5432", Host: "host", Dbname: "db",
				PostgresURL: DbURLFromString("postgres://user:pwd@host/db")},
			msg: "URL specified in --dbname argument failed",
		},
		{
			args: []string{0: "go-test", "-c", "client01", "--pgurl=postgres://user:pwd@host/db"},
			result: CmdOptions{ClientName: "client01", User: "user", Password: "pwd", Port: "5432", Host: "host", Dbname: "db",
				PostgresURL: DbURLFromString("postgres://user:pwd@host/db")},
			msg: "URL in --pgurl without port failed",
		},
		{
			args: []string{"go-test", "-c", "client01", "--pgurl=postgres://user:pwd@host/db?sslmode=require"},
			result: CmdOptions{ClientName: "client01", User: "user", Password: "pwd", Port: "5432", Host: "host", Dbname: "db",
				PostgresURL: DbURLFromString("postgres://user:pwd@host/db?sslmode=require")},
			msg: "URL in --pgurl without port and with sslmode failed",
		},
	}
	for _, d := range tests {
		os.Args = d.args
		c, err := Parse()
		assert.NoError(t, err, d.msg)
		assert.Equal(t, d.result.String(), c.String(), d.msg)
	}
}

func TestNewCmdOptions(t *testing.T) {
	c := NewCmdOptions()
	assert.NotNil(t, c)
}
