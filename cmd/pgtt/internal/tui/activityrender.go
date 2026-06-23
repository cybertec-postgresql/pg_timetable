package tui

import (
	"fmt"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	"github.com/charmbracelet/lipgloss"
)

// identityTokens builds the bracketed context tokens for an activity entry,
// omitting any that are absent (empty != zero). Order mirrors the CLI's
// cmd/pgtt/cmd.logrender.identityTokens so the TUI and `pgtt log` agree:
// chain, task, vxid, then exec-only ms/rc, then client.
func identityTokens(e client.ActivityEntry) []string {
	toks := make([]string, 0, 6)
	if e.ChainID > 0 {
		if e.ChainName != "" {
			toks = append(toks, fmt.Sprintf("[chain:%d|%s]", e.ChainID, e.ChainName))
		} else {
			toks = append(toks, fmt.Sprintf("[chain:%d]", e.ChainID))
		}
	}
	if e.TaskID > 0 {
		toks = append(toks, fmt.Sprintf("[task:%d]", e.TaskID))
	}
	if e.Vxid != "" {
		toks = append(toks, fmt.Sprintf("[vxid:%s]", e.Vxid))
	}
	if e.Source == "exec" {
		if e.DurationMS > 0 {
			toks = append(toks, fmt.Sprintf("[ms:%d]", e.DurationMS))
		}
		if e.Returncode != 0 {
			toks = append(toks, fmt.Sprintf("[rc:%d]", e.Returncode))
		}
	}
	if e.ClientName != "" {
		toks = append(toks, fmt.Sprintf("[client:%s]", e.ClientName))
	}
	return toks
}

// renderActivityLine renders one entry as a single identity-first line:
//
//	TS  LEVEL  [chain:..] [task:..] [vxid:..] …  message
//
// Tokens are colored blue, the level badge by level, the timestamp dimmed. The
// trailing message is clamped to width (0 = no clamp). Styling is suppressed
// when the style set is monochrome (NoColor / non-TTY).
func (s styles) renderActivityLine(e client.ActivityEntry, width int) string {
	tokenStyle := lipgloss.NewStyle()
	if s.enabled {
		tokenStyle = tokenStyle.Foreground(colorBlue)
	}

	var b strings.Builder
	b.WriteString(s.dim.Render(e.TS))
	b.WriteByte(' ')

	badge := fmt.Sprintf("%-7s", e.Level)
	b.WriteString(s.level(e.Level).Bold(s.enabled).Render(badge))
	b.WriteByte(' ')

	for _, tok := range identityTokens(e) {
		b.WriteString(tokenStyle.Render(tok))
		b.WriteByte(' ')
	}

	msg := strings.ReplaceAll(e.Message, "\n", " ")
	if width > 0 {
		used := lipgloss.Width(b.String())
		msg = fit(msg, maxInt(0, width-used))
		msg = strings.TrimRight(msg, " ")
	}
	b.WriteString(msg)
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
