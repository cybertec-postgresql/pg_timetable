package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// tickMsg is delivered on every auto-refresh interval. It carries the time it
// fired so the model can schedule the next tick and drive the countdown.
type tickMsg time.Time

// statusMsg sets a transient success/info line in the footer.
type statusMsg string

// errMsg surfaces a (already DSN-redacted) error in the footer's error style.
type errMsg struct{ err error }

// refreshMsg asks the active view to refetch its data. It is emitted both by
// the auto-refresh tick and by the manual refresh key, so views have a single
// path to reload.
type refreshMsg struct{}

// tickCmd schedules the next auto-refresh tick after d. A non-positive d
// disables auto-refresh (returns nil, so no further ticks are scheduled).
func tickCmd(d time.Duration) tea.Cmd {
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}
