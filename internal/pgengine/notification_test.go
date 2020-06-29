package pgengine_test

import (
	"context"

	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/cybertec-postgresql/pg_timetable/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestNotifications(t *testing.T) {
	teardownTestCase := testutils.SetupTestCase(t)
	defer teardownTestCase(t)

	t.Run("Check Notifications", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go assert.Equal(t, 42, pgengine.WaitForAsyncChain(ctx))
		pgengine.ConfigDb.MustExec("NOTIFY pgengine_unit_test, '42'")
	})
}
