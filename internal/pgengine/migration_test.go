package pgengine_test

import (
	"context"
	_ "embed"
	"testing"

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
	assert.True(t, pge.MigrateDb(ctx), "Migrations should be applied")
	_, err = pge.ConfigDb.Exec(ctx, "DROP SCHEMA IF EXISTS timetable CASCADE")
	assert.NoError(t, err)
}
