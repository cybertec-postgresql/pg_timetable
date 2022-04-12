package pgengine_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyFromFile(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)
	ctx := context.Background()
	_, err := pge.CopyFromFile(ctx, "fake.csv", "COPY location FROM STDIN")
	assert.Error(t, err, "Should fail for missing file")
	_, err = pge.ConfigDb.Exec(ctx, "CREATE TEMP TABLE csv_test(id integer, val text)")
	assert.NoError(t, err, "Should create temporary table")
	assert.NoError(t, os.WriteFile("test.csv", []byte("1,foo\n2,bar"), 0666), "Should create source CSV file")
	cnt, err := pge.CopyFromFile(ctx, "test.csv", "COPY csv_test FROM STDIN (FORMAT csv)")
	assert.NoError(t, err, "Should copy from file")
	assert.True(t, cnt == 2, "Should copy exactly 2 rows")
	assert.NoError(t, os.RemoveAll("test.csv"), "Test output should be removed")
}

func TestCopyToFile(t *testing.T) {
	teardownTestCase := SetupTestCase(t)
	defer teardownTestCase(t)
	ctx := context.Background()
	_, err := pge.CopyToFile(ctx, "", "COPY location TO STDOUT")
	assert.Error(t, err, "Should fail for empty file name")
	cnt, err := pge.CopyToFile(ctx, "test.csv", "COPY (SELECT generate_series(1,5)) TO STDOUT (FORMAT csv)")
	assert.NoError(t, err, "Should copy to file")
	assert.True(t, cnt == 5, "Should copy exactly 5 rows")
	assert.NoError(t, os.RemoveAll("test.csv"), "Test output should be removed")
}
