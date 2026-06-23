package tui

import (
	"fmt"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

// model is the root Bubble Tea model. It owns the client, window dimensions,
// the shared style set, and a status/error line. Concrete views are introduced
// in phase T1+ and pushed onto a view stack for drill-down navigation.
type model struct {
	client client.Client
	opts   Options
	styles styles

	width  int
	height int

	// status is a transient one-line message shown in the footer (last action
	// result). err, when non-nil, is shown in an error style.
	status string
	err    error

	quitting bool
}

func newModel(c client.Client, o Options) model {
	return model{
		client: c,
		opts:   o,
		styles: newStyles(!o.NoColor),
		status: "connected",
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
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
	return left + "  " + info
}

func (m model) bodyView() string {
	// Placeholder until T1+ introduces concrete views.
	return m.styles.dim.Render("No view yet — press q to quit.")
}

func (m model) footerView() string {
	help := m.styles.help.Render("q quit  ? help  r refresh")
	var status string
	switch {
	case m.err != nil:
		status = m.styles.statusErr.Render(m.err.Error())
	case m.status != "":
		status = m.styles.statusOK.Render(m.status)
	}
	if status == "" {
		return help
	}
	return status + "  " + m.styles.dim.Render("·") + "  " + help
}
