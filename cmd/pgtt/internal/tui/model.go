package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// model is the root Bubble Tea model. It owns the client, window dimensions,
// the shared style set, the navigation stack of views, the auto-refresh engine,
// and the status/help chrome. Concrete views implement the view interface and
// are pushed/popped on the stack for drill-down navigation (T1-1).
type model struct {
	client client.Client
	opts   Options
	styles styles
	keys   keyMap
	help   helpModel

	width  int
	height int

	// stack is the view navigation stack; the last element is active. It is
	// seeded on the first message (seedMsg) and always holds at least one view
	// thereafter.
	stack []view

	// refresh engine state
	refresh  time.Duration
	lastTick time.Time
	nextTick time.Time

	// status is a transient one-line message shown in the footer (last action
	// result). err, when non-nil, is shown in an error style and takes priority.
	status string
	err    error

	quitting bool
}

func newModel(c client.Client, o Options) model {
	enabled := !o.NoColor
	return model{
		client:  c,
		opts:    o,
		styles:  newStyles(enabled),
		keys:    defaultKeyMap(),
		help:    newHelp(enabled),
		refresh: o.Refresh,
		status:  "connected",
	}
}

func (m *model) active() view {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

// chromeHeight is the number of lines consumed by header + footer + spacers.
const chromeHeight = 4

func (m *model) bodySize() (int, int) {
	h := m.height - chromeHeight
	if h < 0 {
		h = 0
	}
	return m.width, h
}

// seedMsg triggers one-time stack initialization inside Update, where pointer
// state on the returned model persists (Init runs on a value receiver, so
// seeding there would be discarded).
type seedMsg struct{}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(m.refresh), func() tea.Msg { return seedMsg{} })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case seedMsg:
		if len(m.stack) == 0 {
			root := newPlaceholderView("Chains", m.client, m.styles)
			m.stack = []view{root}
			w, h := m.bodySize()
			root.SetSize(w, h)
			if m.refresh > 0 {
				m.nextTick = time.Now().Add(m.refresh)
			}
			return m, tea.Batch(root.Init(), func() tea.Msg { return refreshMsg{} })
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.setWidth(msg.Width)
		if v := m.active(); v != nil {
			w, h := m.bodySize()
			v.SetSize(w, h)
		}
		return m, nil

	case tickMsg:
		m.lastTick = time.Time(msg)
		var cmds []tea.Cmd
		if m.refresh > 0 {
			m.nextTick = m.lastTick.Add(m.refresh)
			cmds = append(cmds, tickCmd(m.refresh))
		}
		cmds = append(cmds, func() tea.Msg { return refreshMsg{} })
		return m, tea.Batch(cmds...)

	case statusMsg:
		m.status = string(msg)
		m.err = nil
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case pushViewMsg:
		return m.pushView(msg.v)

	case popViewMsg:
		return m.popView()

	case replaceRootMsg:
		return m.replaceRoot(msg.v)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Route everything else (refreshMsg, data messages) to the active view.
	return m.routeToActive(msg)
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.help.toggle()
		return m, nil

	case key.Matches(msg, m.keys.Refresh):
		m.status = "refreshing…"
		m.err = nil
		return m, func() tea.Msg { return refreshMsg{} }

	case key.Matches(msg, m.keys.Back):
		if len(m.stack) > 1 {
			return m.popView()
		}
		if m.help.showFull {
			m.help.toggle()
		}
		return m, nil

	case key.Matches(msg, m.keys.Chains):
		return m.switchTop("Chains")
	case key.Matches(msg, m.keys.Sessions):
		return m.switchTop("Sessions")
	case key.Matches(msg, m.keys.Activity):
		return m.switchTop("Activity")
	}

	return m.routeToActive(msg)
}

// switchTop replaces the stack with a fresh top-level view by name. Later
// phases swap the placeholder constructors for the real views.
func (m model) switchTop(name string) (tea.Model, tea.Cmd) {
	return m.replaceRoot(newPlaceholderView(name, m.client, m.styles))
}

func (m model) pushView(v view) (tea.Model, tea.Cmd) {
	w, h := m.bodySize()
	v.SetSize(w, h)
	m.stack = append(m.stack, v)
	return m, tea.Batch(v.Init(), func() tea.Msg { return refreshMsg{} })
}

func (m model) popView() (tea.Model, tea.Cmd) {
	if len(m.stack) <= 1 {
		return m, nil
	}
	m.stack = m.stack[:len(m.stack)-1]
	if v := m.active(); v != nil {
		w, h := m.bodySize()
		v.SetSize(w, h)
	}
	return m, func() tea.Msg { return refreshMsg{} }
}

func (m model) replaceRoot(v view) (tea.Model, tea.Cmd) {
	w, h := m.bodySize()
	v.SetSize(w, h)
	m.stack = []view{v}
	return m, tea.Batch(v.Init(), func() tea.Msg { return refreshMsg{} })
}

func (m model) routeToActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	v := m.active()
	if v == nil {
		return m, nil
	}
	updated, cmd := v.Update(msg)
	m.stack[len(m.stack)-1] = updated
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteByte('\n')
	b.WriteString(m.bodyView())
	b.WriteByte('\n')
	b.WriteString(m.footerView())
	return b.String()
}

func (m model) headerView() string {
	target := m.opts.Host
	if target == "" {
		target = "(libpq env)"
	}
	schema := m.opts.SchemaVersion
	if schema == "" {
		schema = "?"
	}
	left := m.styles.header.Render("pgtt")
	info := m.styles.headerKey.Render(target) +
		m.styles.dim.Render(fmt.Sprintf("  schema %s", schema))
	crumb := m.styles.dim.Render(m.breadcrumb())
	line := left + "  " + info
	if crumb != "" {
		line += "  " + crumb
	}
	return line
}

// breadcrumb renders the view-stack path, e.g. "› Chains › demo › run 1234".
func (m model) breadcrumb() string {
	if len(m.stack) == 0 {
		return ""
	}
	titles := make([]string, 0, len(m.stack))
	for _, v := range m.stack {
		titles = append(titles, v.Title())
	}
	return "› " + strings.Join(titles, " › ")
}

func (m model) bodyView() string {
	w, h := m.bodySize()
	if m.help.showFull {
		return m.help.full(m.keys, m.styles.title.Render("Key bindings"))
	}
	if v := m.active(); v != nil {
		return v.Body(w, h)
	}
	return m.styles.dim.Render("loading…")
}

func (m model) footerView() string {
	help := m.styles.help.Render(m.help.short(m.keys))

	var status string
	switch {
	case m.err != nil:
		status = m.styles.statusErr.Render("✖ " + m.err.Error())
	case m.status != "":
		status = m.styles.statusOK.Render(m.status)
	}

	countdown := m.styles.dim.Render(m.refreshLabel())

	// Status (left) … countdown (right) on one line, help beneath.
	top := status
	if countdown != "" {
		gap := m.width - lipgloss.Width(status) - lipgloss.Width(countdown)
		if gap < 1 {
			gap = 1
		}
		top = status + strings.Repeat(" ", gap) + countdown
	}
	if strings.TrimSpace(top) == "" {
		return help
	}
	return top + "\n" + help
}

// refreshLabel shows the time until the next auto-refresh, or "manual" when
// auto-refresh is disabled.
func (m model) refreshLabel() string {
	if m.refresh <= 0 {
		return "refresh: manual"
	}
	if m.nextTick.IsZero() {
		return fmt.Sprintf("refresh: %s", m.refresh)
	}
	d := time.Until(m.nextTick).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("next refresh in %s", d)
}
