package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPanelHasBorderAndTitle(t *testing.T) {
	s := newStyles(false)
	out := s.panel("Chains [3]", true, 30, 6, "row one\nrow two")
	// Rounded border corners present.
	for _, want := range []string{"╭", "╮", "╰", "╯", "Chains [3]", "row one", "row two"} {
		if !strings.Contains(out, want) {
			t.Fatalf("panel missing %q:\n%s", want, out)
		}
	}
}

func TestPanelExactSize(t *testing.T) {
	s := newStyles(false)
	out := s.panel("T", false, 20, 5, "x")
	if got := lipgloss.Height(out); got != 5 {
		t.Fatalf("panel height = %d, want 5", got)
	}
	if got := lipgloss.Width(out); got != 20 {
		t.Fatalf("panel width = %d, want 20", got)
	}
}

func TestPanelNarrowTitleSkipped(t *testing.T) {
	s := newStyles(false)
	// Title longer than the border can host: must not panic or corrupt size.
	out := s.panel("a very long title indeed", false, 8, 4, "x")
	if got := lipgloss.Width(out); got != 8 {
		t.Fatalf("narrow panel width = %d, want 8", got)
	}
}

func TestTableHasColumnSeparators(t *testing.T) {
	s := newStyles(false)
	cols := []column{{title: "A", min: 4}, {title: "B", min: 4}}
	rows := [][]cell{{plainCell("1"), plainCell("2")}}
	out := s.renderTable(cols, rows, -1, 20, 4)
	if !strings.Contains(out, "│") {
		t.Fatalf("table missing column separator:\n%s", out)
	}
}

func TestInnerSize(t *testing.T) {
	s := newStyles(false)
	w, h := s.innerSize(20, 6)
	if w != 18 || h != 4 {
		t.Fatalf("innerSize(20,6) = %d,%d, want 18,4", w, h)
	}
	// Clamps to >= 1.
	w, h = s.innerSize(1, 1)
	if w != 1 || h != 1 {
		t.Fatalf("innerSize(1,1) = %d,%d, want 1,1", w, h)
	}
}
