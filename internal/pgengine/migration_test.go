package pgengine_test

import (
	"context"
	_ "embed"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	migrator "github.com/cybertec-postgresql/pgx-migrator"
	"github.com/stretchr/testify/assert"
)

//go:embed sql/migrations/00000.sql
var initialsql string

func TestMigrations(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)

	ctx := context.Background()
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
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)
	pgengine.Migrations = func() migrator.Option {
		return migrator.Migrations()
	}

	ctx := context.Background()
	err := pge.MigrateDb(ctx)
	assert.Error(t, err, "Empty migrations")
	_, err = pge.CheckNeedMigrateDb(ctx)
	assert.Error(t, err, "Empty migrations")
}
