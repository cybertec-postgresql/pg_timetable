package tui

import (
	"fmt"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

// placeholderView is a temporary view used in T1 to exercise navigation,
// refresh and the view stack before the real screens land (T2+). It simply
// shows its name and a refresh counter. It is replaced view-by-view as later
// phases implement chains/sessions/activity.
type placeholderView struct {
	name   string
	client client.Client
	styles styles

	refreshes int
	width     int
	height    int
}

func newPlaceholderView(name string, c client.Client, s styles) *placeholderView {
	return &placeholderView{name: name, client: c, styles: s}
}

func (v *placeholderView) Title() string { return v.name }

func (v *placeholderView) Init() tea.Cmd { return nil }

func (v *placeholderView) SetSize(w, h int) { v.width, v.height = w, h }

func (v *placeholderView) Update(msg tea.Msg) (view, tea.Cmd) {
	if _, ok := msg.(refreshMsg); ok {
		v.refreshes++
	}
	return v, nil
}

func (v *placeholderView) Body(width, _ int) string {
	line1 := v.styles.title.Render(v.name + " view")
	line2 := v.styles.dim.Render("(placeholder — implemented in a later phase)")
	line3 := v.styles.dim.Render(fmt.Sprintf("refreshes: %d", v.refreshes))
	_ = width
	return line1 + "\n\n" + line2 + "\n" + line3
}
