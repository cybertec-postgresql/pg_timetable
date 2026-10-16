package pgengine_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/require"
)

func yamlSamples(t *testing.T) []string {
	paths, err := filepath.Glob("../../samples/yaml/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	return paths
}

func TestYamlSamplesParse(t *testing.T) {
	for _, path := range yamlSamples(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			_, err := pgengine.ParseYamlFile(path)
			require.NoError(t, err)
		})
	}
}

// Loads every sample into a real database without running it, so the
// check_parameter trigger and schema constraints see the samples too.
func TestYamlSamplesLoad(t *testing.T) {
	container, cleanup := testutils.SetupPostgresContainer(t)
	defer cleanup()
	for _, path := range yamlSamples(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			require.NoError(t, container.Engine.LoadYamlChains(context.Background(), path, true))
		})
	}
	assertScalarParameterRejected(t, container.Engine)
}
