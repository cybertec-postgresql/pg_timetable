package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// column describes one table column: a header label and a fixed or flexible
// width. A flex column (flex > 0) absorbs leftover horizontal space in
// proportion to its flex weight; fixed columns keep their min width.
type column struct {
	title string
	min   int // minimum width (also the width for non-flex columns)
	flex  int // flex weight; 0 = fixed
}

// cell is one rendered table cell. style, when non-nil, colorises the text
// (e.g. a status level). text is the raw, unstyled content (used for width
// math so ANSI codes never throw off alignment).
type cell struct {
	text  string
	style *lipgloss.Style
}

func plainCell(text string) cell { return cell{text: text} }

// renderTable lays out a header + rows within the given width/height, drawing a
// selection highlight on the selected row and scrolling so the selection stays
// visible. It returns the rendered block (header line + up to height-1 rows).
func (s styles) renderTable(cols []column, rows [][]cell, selected, width, height int) string {
	if width <= 0 || len(cols) == 0 {
		return ""
	}
	widths := computeWidths(cols, width)

	sep := " " + s.colSep + " " // " │ "

	var b strings.Builder
	// Header.
	b.WriteString(s.tableHeader.Render(layoutRow(cols, widths, headerCells(cols), sep)))

	if len(rows) == 0 {
		b.WriteByte('\n')
		b.WriteString(s.dim.Render("  (no rows)"))
		return b.String()
	}

	// Vertical scroll so the selected row stays in view. The header consumes
	// one line, leaving height-1 for rows.
	visible := height - 1
	if visible < 1 {
		visible = 1
	}
	start := scrollStart(selected, len(rows), visible)
	end := start + visible
	if end > len(rows) {
		end = len(rows)
	}

	rowWidth := sum(widths) + visibleLenTUI(sep)*(len(widths)-1)
	for i := start; i < end; i++ {
		b.WriteByte('\n')
		line := layoutRow(cols, widths, rows[i], sep)
		if i == selected {
			// Selection highlight spans the full row width.
			line = s.rowSelected.Width(rowWidth).Render(stripStyles(cols, widths, rows[i], sep))
		}
		b.WriteString(line)
	}
	return b.String()
}

// visibleLenTUI returns the rune length of a (style-free) separator string.
func visibleLenTUI(s string) int { return len([]rune(s)) }

func headerCells(cols []column) []cell {
	cs := make([]cell, len(cols))
	for i, c := range cols {
		cs[i] = plainCell(c.title)
	}
	return cs
}

// sepWidth is the visible width of the column separator " │ ".
const sepWidth = 3

// computeWidths distributes width across columns: fixed columns take their min,
// flex columns split the remainder by weight (never below their min).
func computeWidths(cols []column, width int) []int {
	widths := make([]int, len(cols))
	gaps := (len(cols) - 1) * sepWidth // " │ " between columns
	avail := width - gaps
	if avail < 0 {
		avail = 0
	}

	fixedTotal, flexTotal := 0, 0
	for i, c := range cols {
		widths[i] = c.min
		if c.flex > 0 {
			flexTotal += c.flex
		} else {
			fixedTotal += c.min
		}
	}
	// Also reserve the min of flex columns.
	flexMin := 0
	for _, c := range cols {
		if c.flex > 0 {
			flexMin += c.min
		}
	}

	remainder := avail - fixedTotal - flexMin
	if remainder < 0 {
		remainder = 0
	}
	if flexTotal > 0 && remainder > 0 {
		for i, c := range cols {
			if c.flex > 0 {
				widths[i] = c.min + remainder*c.flex/flexTotal
			}
		}
	}
	return widths
}

// layoutRow renders cells into fixed-width columns joined by sep, truncating
// overlong content with an ellipsis and padding short content.
func layoutRow(cols []column, widths []int, cells []cell, sep string) string {
	parts := make([]string, len(cols))
	for i := range cols {
		w := widths[i]
		var c cell
		if i < len(cells) {
			c = cells[i]
		}
		txt := fit(c.text, w)
		if c.style != nil {
			txt = c.style.Render(txt)
		}
		parts[i] = txt
	}
	return strings.Join(parts, sep)
}

// stripStyles renders a row's cells without per-cell coloring (used under a
// selection highlight, where the reverse-video background owns the look).
func stripStyles(cols []column, widths []int, cells []cell, sep string) string {
	plain := make([]cell, len(cells))
	for i, c := range cells {
		plain[i] = plainCell(c.text)
	}
	return layoutRow(cols, widths, plain, sep)
}

// fit pads or truncates s to exactly w display cells (ASCII-oriented; the data
// here is identifiers/timestamps, so rune width == byte-ish is fine, but we use
// rune counts to be safe).
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) == w {
		return s
	}
	if len(r) < w {
		return s + strings.Repeat(" ", w-len(r))
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// scrollStart returns the first visible row index so that selected is in view.
func scrollStart(selected, total, visible int) int {
	if total <= visible {
		return 0
	}
	start := selected - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}
