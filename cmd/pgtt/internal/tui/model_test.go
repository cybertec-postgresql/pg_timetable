package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// newTestModel builds a model with a nil client (the placeholder views used in
// T1 never dereference it) and a fixed window size.
func newTestModel(refresh time.Duration) model {
	m := newModel(nil, Options{Refresh: refresh, Host: "h:5432/db", NoColor: true})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(model)
}

// seed drives the one-time seedMsg so the stack is populated, mirroring the
// program loop.
func seed(t *testing.T, m model) model {
	t.Helper()
	tm, _ := m.Update(seedMsg{})
	return tm.(model)
}

func TestSeedPopulatesStack(t *testing.T) {
	m := seed(t, newTestModel(0))
	if got := len(m.stack); got != 1 {
		t.Fatalf("stack len = %d, want 1", got)
	}
	if got := m.active().Title(); got != "Chains" {
		t.Fatalf("root title = %q, want Chains", got)
	}
}

func TestQuitKey(t *testing.T) {
	m := seed(t, newTestModel(0))
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !tm.(model).quitting {
		t.Fatal("q did not set quitting")
	}
	if cmd == nil {
		t.Fatal("q did not return a quit command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("quit command produced nil msg")
	}
}

func TestSwitchTopReplacesRoot(t *testing.T) {
	m := seed(t, newTestModel(0))
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = tm.(model)
	if got := len(m.stack); got != 1 {
		t.Fatalf("after switch, stack len = %d, want 1", got)
	}
	if got := m.active().Title(); got != "Sessions" {
		t.Fatalf("active title = %q, want Sessions", got)
	}
}

func TestPushPopView(t *testing.T) {
	m := seed(t, newTestModel(0))
	child := newPlaceholderView("Detail", nil, m.styles)

	tm, _ := m.Update(pushViewMsg{v: child})
	m = tm.(model)
	if got := len(m.stack); got != 2 {
		t.Fatalf("after push, stack len = %d, want 2", got)
	}
	if got := m.active().Title(); got != "Detail" {
		t.Fatalf("active title = %q, want Detail", got)
	}

	// Esc pops back to the root.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(model)
	if got := len(m.stack); got != 1 {
		t.Fatalf("after pop, stack len = %d, want 1", got)
	}
	if got := m.active().Title(); got != "Chains" {
		t.Fatalf("active title = %q, want Chains", got)
	}
}

func TestEscAtRootDoesNotPop(t *testing.T) {
	m := seed(t, newTestModel(0))
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := len(tm.(model).stack); got != 1 {
		t.Fatalf("esc at root changed stack to %d, want 1", got)
	}
}

func TestHelpToggle(t *testing.T) {
	m := seed(t, newTestModel(0))
	if m.help.showFull {
		t.Fatal("help should start collapsed")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = tm.(model)
	if !m.help.showFull {
		t.Fatal("? did not open help overlay")
	}
	if !strings.Contains(m.bodyView(), "Key bindings") {
		t.Fatal("help overlay body missing heading")
	}
}

func TestViewFillsTerminalHeight(t *testing.T) {
	// The footer must be pinned to the last row: the full View() height equals
	// the terminal height regardless of how little the active view renders.
	m := seed(t, newTestModel(0))
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(model)

	got := lipgloss.Height(m.View())
	if got != 24 {
		t.Fatalf("View height = %d, want 24 (footer not pinned to bottom)", got)
	}

	// And with a different size.
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = tm.(model)
	if got := lipgloss.Height(m.View()); got != 40 {
		t.Fatalf("View height = %d, want 40", got)
	}
}

func TestStatusAndErrorMessages(t *testing.T) {
	m := seed(t, newTestModel(0))

	tm, _ := m.Update(statusMsg("done"))
	m = tm.(model)
	if m.status != "done" || m.err != nil {
		t.Fatalf("statusMsg not applied: status=%q err=%v", m.status, m.err)
	}
	if !strings.Contains(m.footerView(), "done") {
		t.Fatal("footer missing status text")
	}

	tm, _ = m.Update(errMsg{err: errTest("boom")})
	m = tm.(model)
	if m.err == nil {
		t.Fatal("errMsg not applied")
	}
	if !strings.Contains(m.footerView(), "boom") {
		t.Fatal("footer missing error text")
	}
}

func TestRefreshLabel(t *testing.T) {
	if got := newTestModel(0); !strings.Contains(got.refreshLabel(), "manual") {
		t.Fatalf("refresh=0 label = %q, want manual", got.refreshLabel())
	}
	m := newTestModel(5 * time.Second)
	m.nextTick = time.Now().Add(3 * time.Second)
	if got := m.refreshLabel(); !strings.Contains(got, "refresh in") {
		t.Fatalf("auto-refresh label = %q, want countdown", got)
	}
}

func TestRefreshMsgReachesActiveView(t *testing.T) {
	m := seed(t, newTestModel(0))
	if _, ok := m.active().(*chainsView); !ok {
		t.Fatalf("root view type = %T, want *chainsView", m.active())
	}
	// refreshMsg routed to the chains view yields a fetch command.
	_, cmd := m.Update(refreshMsg{})
	if cmd == nil {
		t.Fatal("refreshMsg to chains view returned no fetch command")
	}
}

// TestRefreshMsgReachesPlaceholder verifies routing still drives non-root
// placeholder views (used for Sessions/Activity until later phases).
func TestRefreshMsgReachesPlaceholder(t *testing.T) {
	m := seed(t, newTestModel(0))
	child := newPlaceholderView("Detail", nil, m.styles)
	tm, _ := m.Update(pushViewMsg{v: child})
	m = tm.(model)
	tm, _ = m.Update(refreshMsg{})
	m = tm.(model)
	pv, ok := m.active().(*placeholderView)
	if !ok {
		t.Fatalf("active view type = %T", m.active())
	}
	if pv.refreshes != 1 {
		t.Fatalf("placeholder refreshes = %d, want 1", pv.refreshes)
	}
}

func TestTickReschedulesAndRefreshes(t *testing.T) {
	m := seed(t, newTestModel(5*time.Second))
	tm, cmd := m.Update(tickMsg(time.Now()))
	m = tm.(model)
	if m.nextTick.IsZero() {
		t.Fatal("tick did not schedule nextTick")
	}
	if cmd == nil {
		t.Fatal("tick returned no command (expected reschedule + refresh)")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
