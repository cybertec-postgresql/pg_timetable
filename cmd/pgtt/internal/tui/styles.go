package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Color palette. These mirror the CLI's logrender.go level→color semantics so
// that the TUI and `pgtt log` agree on what INFO/WARN/ERROR/OK/FAIL look like.
var (
	colorRed     = lipgloss.Color("1")
	colorGreen   = lipgloss.Color("2")
	colorBlue    = lipgloss.Color("6")
	colorGray    = lipgloss.Color("7")
	colorMagenta = lipgloss.Color("5")
)

// levelColor maps a log level / PG severity / OK|FAIL status to a palette
// color, matching cmd/pgtt/cmd.logrender.levelColor so the TUI and CLI agree.
func levelColor(level string) lipgloss.Color {
	switch strings.ToUpper(level) {
	case "DEBUG", "TRACE", "RUNNING":
		return colorBlue
	case "WARN", "WARNING":
		return colorMagenta
	case "ERROR", "FATAL", "PANIC", "FAIL":
		return colorRed
	case "OK", "INFO", "NOTICE", "LOG":
		return colorGreen
	default:
		return colorGreen
	}
}

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

	border        lipgloss.Style // unfocused panel border
	borderFocused lipgloss.Style // focused panel border (accent)
	colSep        string         // column separator glyph
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
	s.border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorGray)
	s.borderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBlue)
	s.colSep = "│"

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
		s.border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
		s.borderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	}
	return s
}

// panel wraps content in a rounded box with the title embedded in the top
// border (k9s style). It produces a block exactly outerW wide and outerH tall
// (including the 1-cell border on each side). Use innerSize to compute the
// content area to pass to the table/body renderer.
func (s styles) panel(title string, focused bool, outerW, outerH int, content string) string {
	bs := s.border
	if focused {
		bs = s.borderFocused
	}
	innerW, innerH := s.innerSize(outerW, outerH)
	// Fix the content box to the inner size so the border is flush and stable.
	body := lipgloss.NewStyle().Width(innerW).Height(innerH).MaxHeight(innerH).Render(content)
	box := bs.Render(body)
	return s.overlayTitle(box, title, focused)
}

// innerSize returns the content width/height available inside a panel of the
// given outer dimensions (subtracting the 1-cell border on each edge).
func (s styles) innerSize(outerW, outerH int) (int, int) {
	w := outerW - 2
	h := outerH - 2
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// overlayTitle writes a " title " label into the top border line of a rendered
// box, just after the top-left corner.
func (s styles) overlayTitle(box, title string, focused bool) string {
	if title == "" {
		return box
	}
	lines := strings.Split(box, "\n")
	if len(lines) == 0 {
		return box
	}
	top := []rune(lines[0])
	label := " " + title + " "
	titleStyle := s.dim
	if focused {
		titleStyle = lipgloss.NewStyle().Foreground(colorBlue).Bold(s.enabled)
	}
	styled := titleStyle.Render(label)
	// Insert after the corner rune (index 0). Replace the plain border runes the
	// label covers so total visible width is unchanged.
	labelLen := len([]rune(label))
	if labelLen+2 > len(top) {
		return box // too narrow to host a title
	}
	// Rebuild: corner + styled label + remaining border (from labelLen+1).
	prefix := string(top[0])
	suffix := string(top[1+labelLen:])
	lines[0] = prefix + styled + suffix
	return strings.Join(lines, "\n")
}

// level returns a style colored for the given log level / status, used to
// emphasise status cells (e.g. OK/FAIL, INFO/WARN/ERROR). When color is
// disabled it returns a plain style so the text is unchanged.
func (s styles) level(level string) lipgloss.Style {
	if !s.enabled {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(levelColor(level))
}
