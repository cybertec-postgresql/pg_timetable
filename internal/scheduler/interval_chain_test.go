package scheduler

import (
	"context"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/internal/config"
	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/cybertec-postgresql/pg_timetable/internal/pgengine"
	"github.com/pashagolub/pgxmock/v2"
	"github.com/stretchr/testify/assert"
)

func TestIntervalChain(t *testing.T) {
	mock, err := pgxmock.NewPool(pgxmock.MonitorPingsOption(true))
	assert.NoError(t, err)
	pge := pgengine.NewDB(mock, "scheduler_unit_test")
	sch := New(pge, log.Init(config.LoggingOpts{LogLevel: "error"}))

	ichain := IntervalChain{Interval: 42}
	assert.True(t, ichain.IsListed([]IntervalChain{ichain}))
	assert.False(t, ichain.IsListed([]IntervalChain{}))

	assert.False(t, sch.isValid(ichain))
	sch.intervalChains[ichain.ChainID] = ichain
	assert.True(t, sch.isValid(ichain))

	t.Run("Check reschedule if self destructive", func(t *testing.T) {
		mock.ExpectExec("INSERT INTO timetable\\.log").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		mock.ExpectExec("DELETE").WillReturnResult(pgxmock.NewResult("EXECUTE", 1))
		ichain.SelfDestruct = true
		sch.reschedule(context.Background(), ichain)
	})

	t.Run("Check reschedule if context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ichain.SelfDestruct = false
		sch.reschedule(ctx, ichain)
	})

	t.Run("Check reschedule if everything fine", func(t *testing.T) {
		ichain.Interval = 1
		sch.reschedule(context.Background(), ichain)
	})
}
