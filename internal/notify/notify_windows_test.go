//go:build windows

package notify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows/svc"

	"github.com/cybertec-postgresql/pg_timetable/internal/notify"
)

func TestNotifyWithoutGlobalStatus(t *testing.T) {
	assert.NotPanics(t, func() {
		notify.Ready()
		notify.Stopping()
	})
}

func TestNotifyStatusTransitions(t *testing.T) {
	statuses := make(chan svc.Status, 4)
	notify.SetGlobalStatus(statuses)
	defer notify.SetGlobalStatus(nil)

	notify.Ready()
	st := <-statuses
	assert.Equal(t, svc.Running, st.State)
	assert.Equal(t, svc.AcceptStop|svc.AcceptShutdown, st.Accepts)

	notify.Stopping()
	st = <-statuses
	assert.Equal(t, svc.StopPending, st.State)
}
