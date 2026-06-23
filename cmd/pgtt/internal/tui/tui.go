// Package tui implements a k9s-style terminal UI for pgtt, built entirely on
// top of the internal client.Client interface (PAT-003). It re-implements no
// data access: every read and control action is delegated to the client.
//
// See spec/plan-pgtt-tui.md for the phased plan.
package tui

import (
	"context"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

// Options configures a TUI session.
type Options struct {
	// Refresh is the auto-refresh interval for list views. A non-positive
	// value disables auto-refresh (manual 'r' only).
	Refresh time.Duration
	// Host is a human-readable connection target shown in the header
	// (e.g. "localhost:5432/timetable"). Optional.
	Host string
	// NoColor disables ANSI styling (also honored when stdout is not a TTY).
	NoColor bool
}

const defaultRefresh = 5 * time.Second

// Run starts the TUI against the given (already connected, schema-checked)
// client and blocks until the user quits. The client is NOT closed here; the
// caller owns its lifecycle.
func Run(ctx context.Context, c client.Client, o Options) error {
	if o.Refresh <= 0 {
		o.Refresh = defaultRefresh
	}
	m := newModel(c, o)
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}
