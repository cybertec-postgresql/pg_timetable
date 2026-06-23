package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// sessionsPane identifies which sub-table holds the cursor.
type sessionsPane int

const (
	paneSessions sessionsPane = iota
	paneActive
)

// sessionsLoadedMsg carries the ListSessions result.
type sessionsLoadedMsg struct {
	sessions []client.Session
	err      error
}

// activeLoadedMsg carries the ListActiveChains result.
type activeLoadedMsg struct {
	active []client.ActiveChain
	err    error
}

// sessionsView shows the scheduler's active worker sessions (top) and the
// currently running chains (bottom), each in its own table. It is the
// operator's "who's connected / what's running" screen and the source the
// control verbs (T6) draw their worker list from.
type sessionsView struct {
	client client.Client
	styles styles

	sessions []client.Session
	active   []client.ActiveChain

	focus         sessionsPane
	sessCursor    int
	activeCursor  int
	width, height int

	keys sessionsKeyMap
}

type sessionsKeyMap struct {
	Switch key.Binding
}

func defaultSessionsKeyMap() sessionsKeyMap {
	return sessionsKeyMap{
		Switch: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "sessions/active")),
	}
}

func newSessionsView(c client.Client, s styles) *sessionsView {
	return &sessionsView{client: c, styles: s, keys: defaultSessionsKeyMap()}
}

func (v *sessionsView) Title() string { return "Sessions" }

func (v *sessionsView) Init() tea.Cmd { return tea.Batch(v.fetchSessions(), v.fetchActive()) }

func (v *sessionsView) SetSize(w, h int) { v.width, v.height = w, h }

func (v *sessionsView) fetchSessions() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		s, err := c.ListSessions(context.Background())
		return sessionsLoadedMsg{sessions: s, err: err}
	}
}

func (v *sessionsView) fetchActive() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		a, err := c.ListActiveChains(context.Background())
		return activeLoadedMsg{active: a, err: err}
	}
}

func (v *sessionsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		return v, tea.Batch(v.fetchSessions(), v.fetchActive())

	case sessionsLoadedMsg:
		if msg.err != nil {
			return v, func() tea.Msg { return errMsg{msg.err} }
		}
		v.sessions = msg.sessions
		v.sessCursor = clamp(v.sessCursor, len(v.sessions))
		return v, func() tea.Msg {
			return statusMsg(fmt.Sprintf("%d sessions · %d running", len(v.sessions), len(v.active)))
		}

	case activeLoadedMsg:
		if msg.err != nil {
			return v, func() tea.Msg { return errMsg{msg.err} }
		}
		v.active = msg.active
		v.activeCursor = clamp(v.activeCursor, len(v.active))
		return v, nil

	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

func (v *sessionsView) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	switch {
	case key.Matches(msg, v.keys.Switch):
		if v.focus == paneSessions {
			v.focus = paneActive
		} else {
			v.focus = paneSessions
		}
	case key.Matches(msg, defaultKeyMap().Up):
		v.move(-1)
	case key.Matches(msg, defaultKeyMap().Down):
		v.move(1)
	}
	return v, nil
}

func (v *sessionsView) move(d int) {
	if v.focus == paneSessions {
		v.sessCursor = clamp(v.sessCursor+d, len(v.sessions))
	} else {
		v.activeCursor = clamp(v.activeCursor+d, len(v.active))
	}
}

func (v *sessionsView) Body(width, height int) string {
	remaining := height - 2 // two pane titles
	if remaining < 4 {
		remaining = 4
	}
	sessH := remaining/2 + remaining%2
	activeH := remaining - sessH

	var b strings.Builder
	b.WriteString(v.paneTitle("Worker sessions", v.focus == paneSessions))
	b.WriteByte('\n')
	b.WriteString(v.sessionsTable(width, sessH))
	b.WriteByte('\n')
	b.WriteString(v.paneTitle("Running chains", v.focus == paneActive))
	b.WriteByte('\n')
	b.WriteString(v.activeTable(width, activeH))
	return b.String()
}

func (v *sessionsView) paneTitle(label string, focused bool) string {
	if focused {
		return v.styles.title.Render("▌ " + label)
	}
	return v.styles.dim.Render("  " + label)
}

func (v *sessionsView) sessionsTable(width, height int) string {
	cols := []column{
		{title: "CLIENT", min: 12, flex: 2},
		{title: "CLIENT PID", min: 10},
		{title: "SERVER PID", min: 10},
		{title: "STARTED", min: 19, flex: 1},
	}
	rows := make([][]cell, len(v.sessions))
	for i, s := range v.sessions {
		rows[i] = []cell{
			plainCell(orDash(s.ClientName)),
			plainCell(strconv.FormatInt(s.ClientPID, 10)),
			plainCell(strconv.FormatInt(s.ServerPID, 10)),
			plainCell(orDash(s.StartedAt)),
		}
	}
	sel := -1
	if v.focus == paneSessions {
		sel = v.sessCursor
	}
	return v.styles.renderTable(cols, rows, sel, width, height)
}

func (v *sessionsView) activeTable(width, height int) string {
	cols := []column{
		{title: "CHAIN", min: 8},
		{title: "CLIENT", min: 12, flex: 2},
		{title: "STARTED", min: 19, flex: 1},
	}
	rows := make([][]cell, len(v.active))
	for i, a := range v.active {
		rows[i] = []cell{
			plainCell(strconv.Itoa(a.ChainID)),
			plainCell(orDash(a.ClientName)),
			plainCell(orDash(a.StartedAt)),
		}
	}
	sel := -1
	if v.focus == paneActive {
		sel = v.activeCursor
	}
	return v.styles.renderTable(cols, rows, sel, width, height)
}
