package testutils

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	"github.com/cybertec-postgresql/pg_timetable/internal/cmdparser"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

// setup environment variable runDocker to true to run testcases using postgres docker images
var runDocker bool

var cmdOpts *cmdparser.CmdOptions = cmdparser.NewCmdOptions("pgengine_unit_test")

func TestMain(m *testing.M) {
	runDocker, _ = strconv.ParseBool(os.Getenv("RUN_DOCKER"))
	ctx := context.Background()
	//Create Docker image and run postgres docker image
	if runDocker {
		pgengine.LogToDB(ctx, "LOG", "Running in docker mode...")

		pool, err := dockertest.NewPool("")
		if err != nil {
			panic("Could not connect to docker")
		}
		pgengine.LogToDB(ctx, "LOG", "Connetion to docker established...")

		runOpts := dockertest.RunOptions{
			Repository: "postgres",
			Tag:        "latest",
			Env: []string{
				"POSTGRES_USER=" + cmdOpts.User,
				"POSTGRES_PASSWORD=" + cmdOpts.Password,
				"POSTGRES_DB=" + cmdOpts.Dbname,
			},
		}

		resource, err := pool.RunWithOptions(&runOpts)
		if err != nil {
			panic("Could start postgres container")
		}
		pgengine.LogToDB(ctx, "LOG", "Postgres container is running...")

		defer func() {
			err = pool.Purge(resource)
			if err != nil {
				panic("Could not purge resource")
			}
		}()

		cmdOpts.Host = resource.Container.NetworkSettings.IPAddress

		logWaiter, err := pool.Client.AttachToContainerNonBlocking(docker.AttachToContainerOptions{
			Container: resource.Container.ID,
			// OutputStream: log.Writer(),
			// ErrorStream:  log.Writer(),
			Stderr: true,
			Stdout: true,
			Stream: true,
		})
		if err != nil {
			panic("Could not connect to postgres container log output")
		}

		defer func() {
			err = logWaiter.Close()
			if err != nil {
				pgengine.LogToDB(ctx, "ERROR", "Could not close container log")
			}
			err = logWaiter.Wait()
			if err != nil {
				pgengine.LogToDB(ctx, "ERROR", "Could not wait for container log to close")
			}
		}()

		pool.MaxWait = 10 * time.Second
		err = pool.Retry(func() error {
			db, err := sqlx.Open("postgres", fmt.Sprintf("host='%s' port='%s' sslmode='%s' dbname='%s' user='%s' password='%s'",
				cmdOpts.Host, cmdOpts.Port, cmdOpts.SSLMode, cmdOpts.Dbname, cmdOpts.User, cmdOpts.Password))
			if err != nil {
				return err
			}
			return db.Ping()
		})
		if err != nil {
			panic("Could not connect to postgres server")
		}
		pgengine.LogToDB(ctx, "LOG", "Connetion to postgres established at ",
			fmt.Sprintf("host='%s' port='%s' sslmode='%s' dbname='%s' user='%s' password='%s'",
				cmdOpts.Host, cmdOpts.Port, cmdOpts.SSLMode, cmdOpts.Dbname, cmdOpts.User, cmdOpts.Password))
	}
	os.Exit(m.Run())
}

//SetupTestCase used to connect and to initialize test PostgreSQL database
func SetupTestCase(t *testing.T) func(t *testing.T) {
	cmdOpts.Verbose = testing.Verbose()
	t.Log("Setup test case")
	timeout := time.After(5 * time.Second)
	done := make(chan bool)
	go func() {
		pgengine.InitAndTestConfigDBConnection(context.Background(), *cmdOpts)
		done <- true
	}()
	select {
	case <-timeout:
		t.Fatal("Cannot connect and initialize test database in time")
	case <-done:
	}
	return func(t *testing.T) {
		pgengine.ConfigDb.MustExec("DROP SCHEMA IF EXISTS timetable CASCADE")
		pgengine.FinalizeConfigDBConnection()
		t.Log("Test schema dropped")
	}
}
