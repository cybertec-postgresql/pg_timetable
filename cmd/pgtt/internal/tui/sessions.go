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

// sessionsSnapshotMsg carries a combined sessions+chains snapshot fetched in a
// single round-trip, so both panels reflect the same moment in time.
type sessionsSnapshotMsg struct {
	sessions []client.Session
	active   []client.ActiveChain
	err      error
}

// sessionsView shows, top, the live database connections each running
// pg_timetable instance holds (timetable.active_session — one row per backend,
// so a single worker appears once per pooled connection) and, bottom, the
// chains currently executing (timetable.active_chain). It is the operator's
// "which instances are connected / what's running now" screen and the source
// the control verbs (T6) draw their worker list from.
//
// Note: an active_session row is a *connection*, not an instance run. The
// running instance is identified by its client_name (worker); a worker holds
// several connections, hence several rows.
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
		Switch: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "connections/running")),
	}
}

func newSessionsView(c client.Client, s styles) *sessionsView {
	return &sessionsView{client: c, styles: s, keys: defaultSessionsKeyMap()}
}

func (v *sessionsView) Title() string { return "Sessions" }

func (v *sessionsView) Init() tea.Cmd { return v.fetchSnapshot() }

func (v *sessionsView) SetSize(w, h int) { v.width, v.height = w, h }

// fetchSnapshot loads connections and running chains together in one DB
// round-trip so the two panels never show mismatched snapshots.
func (v *sessionsView) fetchSnapshot() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		s, a, err := c.ListSessionsAndChains(context.Background())
		return sessionsSnapshotMsg{sessions: s, active: a, err: err}
	}
}

func (v *sessionsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		return v, v.fetchSnapshot()

	case sessionsSnapshotMsg:
		if msg.err != nil {
			return v, func() tea.Msg { return errMsg{msg.err} }
		}
		v.sessions = msg.sessions
		v.active = msg.active
		v.sessCursor = clamp(v.sessCursor, len(v.sessions))
		v.activeCursor = clamp(v.activeCursor, len(v.active))
		return v, func() tea.Msg {
			return statusMsg(fmt.Sprintf("%d sessions · %d running", len(v.sessions), len(v.active)))
		}

	// Kept for backward-compatibility / direct injection in tests.
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
	sessH := height/2 + height%2
	activeH := height - sessH
	if sessH < 3 {
		sessH = 3
	}
	if activeH < 3 {
		activeH = 3
	}

	sessInnerW, sessInnerH := v.styles.innerSize(width, sessH)
	activeInnerW, activeInnerH := v.styles.innerSize(width, activeH)

	sessTitle := fmt.Sprintf("Connections [%d]", len(v.sessions))
	activeTitle := fmt.Sprintf("Running chains [%d]", len(v.active))

	top := v.styles.panel(sessTitle, v.focus == paneSessions, width, sessH,
		v.sessionsTable(sessInnerW, sessInnerH))
	bottom := v.styles.panel(activeTitle, v.focus == paneActive, width, activeH,
		v.activeTable(activeInnerW, activeInnerH))
	return top + "\n" + bottom
}

func (v *sessionsView) sessionsTable(width, height int) string {
	cols := []column{
		{title: "WORKER", min: 12, flex: 1}, // client_name = instance/worker name
		{title: "SCHED PID", min: 10},       // client_pid = scheduler process PID
		{title: "BACKEND PID", min: 11},     // server_pid = PostgreSQL backend PID
		{title: "CONNECTED", min: 19},
		{title: "ACTIVITY", min: 12, flex: 3}, // current query or <IDLE>
	}
	rows := make([][]cell, len(v.sessions))
	for i, s := range v.sessions {
		rows[i] = []cell{
			plainCell(orDash(s.ClientName)),
			plainCell(strconv.FormatInt(s.ClientPID, 10)),
			plainCell(strconv.FormatInt(s.ServerPID, 10)),
			plainCell(orDash(s.StartedAt)),
			plainCell(sessionActivity(s)),
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
		{title: "NAME", min: 12, flex: 2},
		{title: "WORKER", min: 12, flex: 2},
		{title: "STARTED", min: 19, flex: 1},
	}
	rows := make([][]cell, len(v.active))
	for i, a := range v.active {
		rows[i] = []cell{
			plainCell(strconv.Itoa(a.ChainID)),
			plainCell(orDash(a.ChainName)),
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

// sessionActivity returns a single-line description of what a backend is doing,
// straight from pg_stat_activity.state.
//
// pg_timetable uses a connection pool: most connections are LISTEN'ers sitting
// "idle", the chain-executing backend spends most of its life "idle in
// transaction" between tasks, and it is "active" only while a statement runs.
// We surface the real backend state so the operator sees the truth:
//   - "active"              → the running SQL (or <ACTIVE> if the query text is hidden)
//   - "idle in transaction" → <IDLE IN TX>
//   - "idle" / unknown      → <IDLE>
func sessionActivity(s client.Session) string {
	switch strings.TrimSpace(s.State) {
	case "active":
		if q := strings.Join(strings.Fields(s.Query), " "); q != "" {
			return q
		}
		return "<ACTIVE>"
	case "idle in transaction":
		return "<IDLE IN TX>"
	case "idle in transaction (aborted)":
		return "<IDLE IN TX (ABORTED)>"
	case "fastpath function call":
		return "<FASTPATH>"
	default:
		return "<IDLE>"
	}
}
