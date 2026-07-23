package tui

import (
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleSessions() []client.Session {
	return []client.Session{
		{ClientName: "w1", ClientPID: 100, ServerPID: 200, StartedAt: "2026-06-23 09:00:00"},
		{ClientName: "w2", ClientPID: 101, ServerPID: 201, StartedAt: "2026-06-23 09:05:00"},
	}
}

func sampleActive() []client.ActiveChain {
	return []client.ActiveChain{
		{ChainID: 5, ChainName: "nightly-etl", ClientName: "w1", StartedAt: "2026-06-23 10:00:00"},
	}
}

func loadedSessionsView() *sessionsView {
	v := newSessionsView(nil, newStyles(false))
	v.SetSize(120, 20)
	uv, _ := v.Update(sessionsLoadedMsg{sessions: sampleSessions()})
	v = uv.(*sessionsView)
	uv, _ = v.Update(activeLoadedMsg{active: sampleActive()})
	return uv.(*sessionsView)
}

func TestSessionsLoad(t *testing.T) {
	v := loadedSessionsView()
	if len(v.sessions) != 2 || len(v.active) != 1 {
		t.Fatalf("sessions=%d active=%d, want 2/1", len(v.sessions), len(v.active))
	}
	out := v.Body(120, 18)
	for _, want := range []string{"Connections", "Running chains", "w1", "w2", "BACKEND PID", "CHAIN", "NAME", "nightly-etl"} {
		if !strings.Contains(out, want) {
			t.Fatalf("body missing %q", want)
		}
	}
}

func TestSessionsFocusSwitchAndMove(t *testing.T) {
	v := loadedSessionsView()
	if v.focus != paneSessions {
		t.Fatal("default focus should be sessions")
	}
	v.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v.sessCursor != 1 {
		t.Fatalf("sessCursor = %d, want 1", v.sessCursor)
	}
	// Tab to active chains.
	v.Update(tea.KeyMsg{Type: tea.KeyTab})
	if v.focus != paneActive {
		t.Fatal("tab did not switch to active")
	}
	// Down clamps (only 1 active chain).
	v.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v.activeCursor != 0 {
		t.Fatalf("activeCursor = %d, want 0 (clamped)", v.activeCursor)
	}
	if v.sessCursor != 1 {
		t.Fatalf("sessCursor changed while focus on active: %d", v.sessCursor)
	}
}

func TestSessionsRefreshFetches(t *testing.T) {
	v := loadedSessionsView()
	_, cmd := v.Update(refreshMsg{})
	if cmd == nil {
		t.Fatal("refresh returned no fetch command")
	}
}

func TestSessionsErrorSurfaces(t *testing.T) {
	v := newSessionsView(nil, newStyles(false))
	_, cmd := v.Update(sessionsLoadedMsg{err: errTest("down")})
	if cmd == nil {
		t.Fatal("no command on error")
	}
	if _, ok := cmd().(errMsg); !ok {
		t.Fatal("expected errMsg")
	}

	_, cmd = v.Update(activeLoadedMsg{err: errTest("down2")})
	if _, ok := cmd().(errMsg); !ok {
		t.Fatal("expected errMsg from active error")
	}
}

func TestSessionsStatusCountsBoth(t *testing.T) {
	v := newSessionsView(nil, newStyles(false))
	uv, _ := v.Update(activeLoadedMsg{active: sampleActive()})
	v = uv.(*sessionsView)
	_, cmd := v.Update(sessionsLoadedMsg{sessions: sampleSessions()})
	msg := cmd()
	sm, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("expected statusMsg, got %T", msg)
	}
	if !strings.Contains(string(sm), "2 sessions") || !strings.Contains(string(sm), "1 running") {
		t.Fatalf("status = %q, want counts", string(sm))
	}
}
