package testutils

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupPostgresContainer(t *testing.T) {
	container, cleanup := SetupPostgresContainer(t)
	defer cleanup()

	// Verify container is created and running
	require.NotNil(t, container, "Container should not be nil")
	require.NotNil(t, container.Container, "PostgreSQL container should not be nil")
	require.NotNil(t, container.Engine, "PgEngine should not be nil")
	require.NotEmpty(t, container.ConnStr, "Connection string should not be empty")

	// Verify connection string format
	assert.Contains(t, container.ConnStr, "postgres://")
	assert.Contains(t, container.ConnStr, "scheduler")
	assert.Contains(t, container.ConnStr, "timetable")
	assert.Contains(t, container.ConnStr, "sslmode=disable")

	// Verify database connection works
	ctx := context.Background()
	conn, err := container.Engine.ConfigDb.Acquire(ctx)
	require.NoError(t, err, "Should be able to acquire database connection")
	defer conn.Release()

	// Test basic query
	var result int
	err = conn.QueryRow(ctx, "SELECT 1").Scan(&result)
	require.NoError(t, err, "Should be able to execute simple query")
	assert.Equal(t, 1, result, "Query should return 1")

	// Verify pg_timetable schema is initialized
	var schemaExists bool
	err = conn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = 'timetable')").Scan(&schemaExists)
	require.NoError(t, err, "Should be able to check schema existence")
	assert.True(t, schemaExists, "timetable schema should exist")

	// Verify some core tables exist
	tables := []string{"chain", "task", "log"}
	for _, table := range tables {
		var tableExists bool
		err = conn.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = 'timetable' AND table_name = $1)",
			table).Scan(&tableExists)
		require.NoError(t, err, "Should be able to check table existence for %s", table)
		assert.True(t, tableExists, "Table %s should exist in timetable schema", table)
	}
}

func TestSetupPostgresContainerWithOptions(t *testing.T) {
	customClientName := "custom_test_client"
	customDebug := true

	container, cleanup := SetupPostgresContainerWithOptions(t, func(opts *config.CmdOptions) {
		opts.ClientName = customClientName
		opts.Start.Debug = customDebug
	})
	defer cleanup()

	// Verify custom options were applied
	require.NotNil(t, container.Engine, "PgEngine should not be nil")
	assert.Equal(t, customClientName, container.Engine.ClientName, "Custom client name should be set")

	// Verify database functionality with custom options
	ctx := context.Background()
	conn, err := container.Engine.ConfigDb.Acquire(ctx)
	require.NoError(t, err, "Should be able to acquire database connection")
	defer conn.Release()

	// Test that we can perform database operations
	var dbName string
	err = conn.QueryRow(ctx, "SELECT current_database()").Scan(&dbName)
	require.NoError(t, err, "Should be able to get current database name")
	assert.Equal(t, "timetable", dbName, "Database name should be 'timetable'")
}

func TestSetupPostgresContainerWithNilCustomizer(t *testing.T) {
	// Test that nil customizer doesn't cause issues
	container, cleanup := SetupPostgresContainerWithOptions(t, nil)
	defer cleanup()

	require.NotNil(t, container, "Container should not be nil even with nil customizer")
	require.NotNil(t, container.Engine, "PgEngine should not be nil")

	// Verify default client name is used
	assert.Equal(t, "testcontainers_unit_test", container.Engine.ClientName, "Default client name should be used")
}

func TestContainerIsolation(t *testing.T) {
	// Test that containers are isolated from each other
	container1, cleanup1 := SetupPostgresContainer(t)
	defer cleanup1()

	container2, cleanup2 := SetupPostgresContainer(t)
	defer cleanup2()

	// Verify they have different connection strings (different ports)
	assert.NotEqual(t, container1.ConnStr, container2.ConnStr, "Containers should have different connection strings")

	ctx := context.Background()

	// Create a test table in container1
	conn1, err := container1.Engine.ConfigDb.Acquire(ctx)
	require.NoError(t, err)
	defer conn1.Release()

	_, err = conn1.Exec(ctx, "CREATE TABLE test_isolation (id int)")
	require.NoError(t, err)

	// Verify table exists in container1
	var exists1 bool
	err = conn1.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'test_isolation')").Scan(&exists1)
	require.NoError(t, err)
	assert.True(t, exists1, "Table should exist in container1")

	// Verify table does NOT exist in container2
	conn2, err := container2.Engine.ConfigDb.Acquire(ctx)
	require.NoError(t, err)
	defer conn2.Release()

	var exists2 bool
	err = conn2.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'test_isolation')").Scan(&exists2)
	require.NoError(t, err)
	assert.False(t, exists2, "Table should NOT exist in container2 (containers are isolated)")
}
