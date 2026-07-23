package tui

import (
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleSessions() []client.Session {
	return []client.Session{
		{ClientName: "w1", ClientPID: 100, ServerPID: 200, StartedAt: "2026-06-23 09:00:00",
			State: "active", Query: "SELECT timetable.get_chain()"},
		{ClientName: "w2", ClientPID: 101, ServerPID: 201, StartedAt: "2026-06-23 09:05:00",
			State: "idle in transaction"},
		{ClientName: "w3", ClientPID: 102, ServerPID: 202, StartedAt: "2026-06-23 09:06:00",
			State: "idle"},
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
	uv, _ := v.Update(sessionsSnapshotMsg{sessions: sampleSessions(), active: sampleActive()})
	return uv.(*sessionsView)
}

func TestSessionsLoad(t *testing.T) {
	v := loadedSessionsView()
	if len(v.sessions) != 3 || len(v.active) != 1 {
		t.Fatalf("sessions=%d active=%d, want 3/1", len(v.sessions), len(v.active))
	}
	out := v.Body(120, 18)
	for _, want := range []string{"Connections", "Running chains", "w1", "w2", "BACKEND PID", "CHAIN", "NAME", "nightly-etl", "ACTIVITY", "<IDLE>", "<IDLE IN TX>"} {
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

	_, cmd = v.Update(sessionsSnapshotMsg{err: errTest("down3")})
	if _, ok := cmd().(errMsg); !ok {
		t.Fatal("expected errMsg from snapshot error")
	}
}

func TestSessionsStatusCountsBoth(t *testing.T) {
	v := newSessionsView(nil, newStyles(false))
	_, cmd := v.Update(sessionsSnapshotMsg{sessions: sampleSessions(), active: sampleActive()})
	msg := cmd()
	sm, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("expected statusMsg, got %T", msg)
	}
	if !strings.Contains(string(sm), "3 sessions") || !strings.Contains(string(sm), "1 running") {
		t.Fatalf("status = %q, want counts", string(sm))
	}
}

func TestSessionActivity(t *testing.T) {
	cases := []struct {
		name string
		s    client.Session
		want string
	}{
		{"running query", client.Session{State: "active", Query: "SELECT 1"}, "SELECT 1"},
		{"active no query", client.Session{State: "active"}, "<ACTIVE>"},
		{"idle in transaction ignores stale query", client.Session{State: "idle in transaction", Query: "BEGIN"}, "<IDLE IN TX>"},
		{"idle in transaction aborted", client.Session{State: "idle in transaction (aborted)"}, "<IDLE IN TX (ABORTED)>"},
		{"fastpath", client.Session{State: "fastpath function call"}, "<FASTPATH>"},
		{"idle ignores stale query", client.Session{State: "idle", Query: "SELECT 1"}, "<IDLE>"},
		{"idle no query", client.Session{State: "idle"}, "<IDLE>"},
		{"no info", client.Session{}, "<IDLE>"},
		{"no state ignores stale query", client.Session{Query: "SELECT 1"}, "<IDLE>"},
		{"multiline collapsed", client.Session{State: "active", Query: "SELECT\n  a,\n  b"}, "SELECT a, b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionActivity(tc.s); got != tc.want {
				t.Fatalf("sessionActivity() = %q, want %q", got, tc.want)
			}
		})
	}
}
