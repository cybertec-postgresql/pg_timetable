package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
)

// helpModel wraps the bubbles help component. It renders either the short
// (single-line) hint in the footer or, when toggled, the full key grid as an
// overlay body.
type helpModel struct {
	inner    help.Model
	showFull bool
}

func newHelp(enabled bool) helpModel {
	h := help.New()
	if !enabled {
		// Plain styling: clear the separators' color so output is monochrome.
		h.Styles.ShortKey = h.Styles.ShortKey.UnsetForeground()
		h.Styles.ShortDesc = h.Styles.ShortDesc.UnsetForeground()
		h.Styles.ShortSeparator = h.Styles.ShortSeparator.UnsetForeground()
		h.Styles.FullKey = h.Styles.FullKey.UnsetForeground()
		h.Styles.FullDesc = h.Styles.FullDesc.UnsetForeground()
		h.Styles.FullSeparator = h.Styles.FullSeparator.UnsetForeground()
	}
	return helpModel{inner: h}
}

func (h *helpModel) toggle() { h.showFull = !h.showFull }

func (h *helpModel) setWidth(w int) { h.inner.Width = w }

// short renders the compact footer hint.
func (h helpModel) short(k keyMap) string {
	h.inner.ShowAll = false
	return h.inner.View(k)
}

// full renders the expanded help grid (overlay body), with a heading.
func (h helpModel) full(k keyMap, title string) string {
	h.inner.ShowAll = true
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(h.inner.View(k))
	return b.String()
}
