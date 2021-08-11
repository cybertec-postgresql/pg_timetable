package pgengine_test

import (
	"context"
	_ "embed"
	"os"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
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
	f, err := os.Create("sql/migrations/empty.sql")
	assert.NoError(t, err)
	f.Close()
	assert.Error(t, pgengine.ExecuteMigrationScript(context.Background(), nil, "empty.sql"), "File is empty")
	assert.NoError(t, os.Remove("sql/migrations/empty.sql"))
}
