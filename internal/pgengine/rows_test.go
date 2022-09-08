package pgengine_test

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

func TestRowToStructByNameEmbeddedStruct(t *testing.T) {
	type Name struct {
		Last  string `db:"last_name"`
		First string `db:"first_name"`
	}

	type person struct {
		Ignore bool `db:"-"`
		Name
		Age int32
	}

	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)
	ctx := context.Background()

	rows, _ := pge.ConfigDb.Query(ctx, `select 'John' as first_name, 'Smith' as last_name, n as age from generate_series(0, 9) n`)
	slice, err := pgx.CollectRows(rows, pgengine.RowToStructByName[person])
	assert.NoError(t, err)

	assert.Len(t, slice, 10)
	for i := range slice {
		assert.Equal(t, "Smith", slice[i].Name.Last)
		assert.Equal(t, "John", slice[i].Name.First)
		assert.EqualValues(t, i, slice[i].Age)
	}

	// check missing fields in a returned row
	rows, _ = pge.ConfigDb.Query(ctx, `select 'Smith' as last_name, n as age from generate_series(0, 9) n`)
	_, err = pgx.CollectRows(rows, pgengine.RowToStructByName[person])
	assert.ErrorContains(t, err, "cannot find field first_name in returned row")

	// check missing field in a destination struct
	rows, _ = pge.ConfigDb.Query(ctx, `select 'John' as first_name, 'Smith' as last_name, n as age, null as ignore from generate_series(0, 9) n`)
	_, err = pgx.CollectRows(rows, pgengine.RowToAddrOfStructByName[person])
	assert.ErrorContains(t, err, "struct doesn't have corresponding row field ignore")
}
