package client_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// currentSchema is the schema version the test database is seeded with.
// testutils applies the latest embedded schema, matching main.go's dbapi.
const currentSchema = "00733"

// TestCheckSchemaVersion_Compatible verifies a freshly initialized database
// (seeded by testutils with the latest schema) is accepted (REQ-016).
func TestCheckSchemaVersion_Compatible(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	c := client.New(currentSchema)
	require.NoError(t, c.Connect(context.Background(), tc.ConnStr))
	defer c.Close()

	assert.NoError(t, c.CheckSchemaVersion(context.Background()))
}

// TestCheckSchemaVersion_Incompatible verifies a database whose schema differs
// from the version pgtt was built for is refused (AC-009).
func TestCheckSchemaVersion_Incompatible(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	c := client.New("99999") // pretend pgtt requires a newer schema
	require.NoError(t, c.Connect(context.Background(), tc.ConnStr))
	defer c.Close()

	err := c.CheckSchemaVersion(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, client.ErrSchemaIncompatible))
}

// TestCheckSchemaVersion_Absent verifies that a database without the timetable
// schema is reported as absent rather than crashing (AC-009 / spec §9).
func TestCheckSchemaVersion_Absent(t *testing.T) {
	tc, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()

	// Drop the schema to simulate a database that never ran pg_timetable.
	_, err := tc.Engine.ConfigDb.Exec(context.Background(), "DROP SCHEMA timetable CASCADE")
	require.NoError(t, err)

	c := client.New(currentSchema)
	require.NoError(t, c.Connect(context.Background(), tc.ConnStr))
	defer c.Close()

	err = c.CheckSchemaVersion(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, client.ErrSchemaAbsent))
}

// TestConnect_DoesNotLeakPassword verifies a failed connection never exposes
// the password from the DSN (SEC-002 / AC-010).
func TestConnect_DoesNotLeakPassword(t *testing.T) {
	const secret = "sup3r-s3cret-pw"
	dsn := "postgres://user:" + secret + "@127.0.0.1:1/doesnotexist?connect_timeout=1&sslmode=disable"

	c := client.New(currentSchema)
	err := c.Connect(context.Background(), dsn)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret, "error must not contain the password")
	assert.False(t, strings.Contains(err.Error(), dsn), "error must not contain the full DSN")
}

// TestConnect_InvalidDSN verifies a malformed DSN yields a generic error with
// no credential content (SEC-002).
func TestConnect_InvalidDSN(t *testing.T) {
	c := client.New(currentSchema)
	err := c.Connect(context.Background(), "postg://bad:pw@@@host")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "pw")
}
