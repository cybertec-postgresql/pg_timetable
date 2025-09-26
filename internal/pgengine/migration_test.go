package pgengine_test

import (
	"context"
	_ "embed"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	migrator "github.com/cybertec-postgresql/pgx-migrator"
	"github.com/stretchr/testify/assert"
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
	_, err = pge.ConfigDb.Exec(ctx, "DROP SCHEMA IF EXISTS timetable CASCADE")
	assert.NoError(t, err)

	_, err = pge.CheckNeedMigrateDb(ctx)
	assert.NoError(t, err)
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
