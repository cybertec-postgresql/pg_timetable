package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette. These mirror the CLI's logrender.go level→color semantics so
// that the TUI and `pgtt log` agree on what INFO/WARN/ERROR/OK/FAIL look like.
// Level-aware coloring of status cells arrives with the chains/activity views
// (phase T2+); the palette lives here so both stay in one place.
var (
	colorRed   = lipgloss.Color("1")
	colorGreen = lipgloss.Color("2")
	colorBlue  = lipgloss.Color("6")
	colorGray  = lipgloss.Color("7")
)

// styles holds the lipgloss styles used across views. Built once per model so
// color can be globally disabled (NoColor / non-TTY).
type styles struct {
	enabled bool

	header    lipgloss.Style
	headerKey lipgloss.Style
	footer    lipgloss.Style
	statusOK  lipgloss.Style
	statusErr lipgloss.Style
	help      lipgloss.Style

	tableHeader lipgloss.Style
	rowSelected lipgloss.Style
	rowNormal   lipgloss.Style
	dim         lipgloss.Style
	title       lipgloss.Style
}

func newStyles(enabled bool) styles {
	s := styles{enabled: enabled}
	s.header = lipgloss.NewStyle().Bold(true).Foreground(colorGray)
	s.headerKey = lipgloss.NewStyle().Foreground(colorBlue)
	s.footer = lipgloss.NewStyle().Foreground(colorGray)
	s.statusOK = lipgloss.NewStyle().Foreground(colorGreen)
	s.statusErr = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	s.help = lipgloss.NewStyle().Foreground(colorGray)
	s.tableHeader = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	s.rowSelected = lipgloss.NewStyle().Bold(true).Reverse(true)
	s.rowNormal = lipgloss.NewStyle()
	s.dim = lipgloss.NewStyle().Faint(true)
	s.title = lipgloss.NewStyle().Bold(true)

	if !enabled {
		// Strip all coloring/attributes when disabled.
		plain := lipgloss.NewStyle()
		s.header = plain
		s.headerKey = plain
		s.footer = plain
		s.statusOK = plain
		s.statusErr = plain
		s.help = plain
		s.tableHeader = plain
		s.rowSelected = lipgloss.NewStyle().Reverse(true)
		s.rowNormal = plain
		s.dim = plain
		s.title = lipgloss.NewStyle().Bold(true)
	}
	return s
}
