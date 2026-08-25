//go:build !windows

package notify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cybertec-postgresql/pg_timetable/internal/notify"
)

func TestNotifyIsNoop(t *testing.T) {
	assert.NotPanics(t, func() {
		notify.SetGlobalStatus(nil)
		notify.Ready()
		notify.Stopping()
	})
}
