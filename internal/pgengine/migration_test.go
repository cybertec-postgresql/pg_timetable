package pgengine_test

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	migrator "github.com/cybertec-postgresql/pgx-migrator"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed sql/migrations/00000.sql
var initialsql string

func TestMigrations(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	pge := container.Engine
	_, err := pge.ConfigDb.Exec(ctx, "DROP SCHEMA IF EXISTS timetable CASCADE")
	assert.NoError(t, err)
	_, err = pge.ConfigDb.Exec(ctx, string(initialsql))
	assert.NoError(t, err)
	ok, err := pge.CheckNeedMigrateDb(ctx)
	assert.NoError(t, err)
	assert.True(t, ok, "Should need migrations")
	assert.NoError(t, pge.MigrateDb(ctx), "Migrations should be applied")

	// 00820 applies over every prior migration and the
	// timetable.secret store is created.
	var hasSecret bool
	assert.NoError(t, pge.ConfigDb.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_class c
		                JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		                WHERE n.nspname='timetable' AND c.relname='secret')`).Scan(&hasSecret))
	assert.True(t, hasSecret, "00820 must create timetable.secret")

	// 00850 replaces timetable.cron_split_to_arrays on existing installs.
	assertCronSplitRejectsValue(t, pge)
}

// TestCronSplitToArraysUnknownValue checks the function created by cron.sql on a fresh install.
func TestCronSplitToArraysUnknownValue(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	assertCronSplitRejectsValue(t, container.Engine)
}

// assertCronSplitRejectsValue expects the "not recognized" exception, not the
// 42725 error the old `text + text` hint raised.
func assertCronSplitRejectsValue(t *testing.T, pge *pgengine.PgEngine) {
	t.Helper()
	_, err := pge.ConfigDb.Exec(context.Background(), "SELECT timetable.cron_split_to_arrays('foo * * * *')")
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected a PostgreSQL error, got %v", err)
	assert.Equal(t, "P0001", pgErr.Code)
	assert.Equal(t, `Value ("foo") not recognized`, pgErr.Message)
	assert.Contains(t, pgErr.Hint, "fields separated by space or tab. Values allowed")
}
func TestExecuteMigrationScript(t *testing.T) {
	assert.Error(t, pgengine.ExecuteMigrationScript(context.Background(), nil, "foo"), "File does not exist")
}

func TestInitMigrator(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	pgengine.Migrations = func() migrator.Option {
		return migrator.Migrations()
	}

	ctx := context.Background()
	pge := container.Engine
	err := pge.MigrateDb(ctx)
	assert.Error(t, err, "Empty migrations")
	_, err = pge.CheckNeedMigrateDb(ctx)
	assert.Error(t, err, "Empty migrations")
}
