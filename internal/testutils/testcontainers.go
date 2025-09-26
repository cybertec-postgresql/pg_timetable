package testutils

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
)

// PostgresTestContainer wraps the postgres container with pg_timetable engine
type PostgresTestContainer struct {
	Container *postgres.PostgresContainer
	Engine    *pgengine.PgEngine
	ConnStr   string
}

// SetupPostgresContainer creates a new PostgreSQL container for testing
func SetupPostgresContainer(t *testing.T) (*PostgresTestContainer, func()) {
	t.Helper()
	return SetupPostgresContainerWithOptions(t, nil)
}

// SetupPostgresContainerWithOptions creates a PostgreSQL container with custom options
func SetupPostgresContainerWithOptions(t *testing.T, customizer func(*config.CmdOptions)) (*PostgresTestContainer, func()) {
	t.Helper()

	ctx := context.Background()

	postgresContainer, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("timetable"),
		postgres.WithUsername("scheduler"),
		postgres.WithPassword("somestrong"),
		testcontainers.WithWaitStrategyAndDeadline(30*time.Second, wait.ForLog("database system is ready to accept connections")),
	)
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}

	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get connection string: %v", err)
	}

	cmdOpts := config.NewCmdOptions("--clientname=testcontainers_unit_test", "--connstr="+connStr)

	if customizer != nil {
		customizer(cmdOpts)
	}

	var pge *pgengine.PgEngine
	timeout := time.After(3 * time.Minute)
	done := make(chan bool)
	go func() {
		pge, err = pgengine.New(ctx, *cmdOpts, log.Init(config.LoggingOpts{LogLevel: "panic", LogDBLevel: "none"}))
		done <- true
	}()

	select {
	case <-timeout:
		t.Fatal("Cannot connect and initialize test database in time")
	case <-done:
		if err != nil {
			t.Fatalf("Failed to initialize pg_timetable engine: %v", err)
		}
	}

	testContainer := &PostgresTestContainer{
		Container: postgresContainer,
		Engine:    pge,
		ConnStr:   connStr,
	}

	cleanup := func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate PostgreSQL container: %v", err)
		}
		t.Log("PostgreSQL test container terminated")
	}

	return testContainer, cleanup
}
