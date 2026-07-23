package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
)

// This file implements the rich, identity-first renderer for the unified
// activity feed (`log list` / `log tail`). It deliberately mirrors the look of
// the scheduler's own logger (internal/log/formatter.go) instead of the flat,
// zero-padded table the activity feed used to emit:
//
//	pgtt.output (old): log  INFO   0  0  0  demo_worker  Chain executed successfully
//	new:               2026-06-23 16:08:43.387 INFO  [chain:1|notify_every_minute] [task:1] [vxid:21474836598] Starting task
//
// Empty context tokens are omitted entirely (empty != zero), MS/RC are shown
// only for exec rows where they are meaningful, and levels/statuses are
// color-coded on a TTY (P7-5/P7-6/P7-7).

// ANSI color codes, matching internal/log/formatter.go (getColorByLevel).
const (
	ansiReset    = "\x1b[0m"
	ansiBold     = "\x1b[1m"
	ansiDim      = "\x1b[2m"
	colorRed     = 31
	colorGreen   = 32
	colorYellow  = 33
	colorBlue    = 36
	colorGray    = 37
	colorMagenta = 35
)

// logColorEnabled controls whether the renderer emits ANSI escapes. It is set
// once per command invocation by configureLogColor and consulted by colorize.
var logColorEnabled bool

// configureLogColor decides whether color should be emitted for this run.
// Color is enabled only when:
//   - the output format is not JSON, AND
//   - NO_COLOR is not set (https://no-color.org/), AND
//   - --no-color was not passed, AND
//   - the destination looks like a terminal.
//
// w is the actual writer the renderer will use (usually cmd.OutOrStdout()),
// so tests that pass a bytes.Buffer get deterministic, color-free output.
func configureLogColor(w io.Writer, jsonOutput, noColorFlag bool) {
	logColorEnabled = false
	treeChars = treeASCII // safe default; upgraded below when Unicode is supported
	if jsonOutput || noColorFlag {
		return
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return
	}
	logColorEnabled = isTerminalWriter(w)
	if supportsUnicode() {
		treeChars = treeUnicode
	}
}

// supportsUnicode reports whether the terminal is likely to render UTF-8
// correctly. It checks the LANG/LC_ALL/LC_CTYPE locale variables (POSIX) and
// the TERM_PROGRAM / WT_SESSION hints (macOS Terminal, Windows Terminal).
// Legacy Windows consoles on non-UTF-8 code pages are excluded via the absence
// of any UTF-8 marker, which keeps the ASCII fallback for cmd.exe / PowerShell
// on non-UTF-8 code pages while allowing Windows Terminal (which sets WT_SESSION).
func supportsUnicode() bool {
	for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		val := strings.ToUpper(os.Getenv(v))
		if strings.Contains(val, "UTF-8") || strings.Contains(val, "UTF8") {
			return true
		}
	}
	// Windows Terminal and VS Code terminal set WT_SESSION; iTerm2 / macOS
	// Terminal set TERM_PROGRAM. Both reliably support UTF-8.
	if os.Getenv("WT_SESSION") != "" || os.Getenv("TERM_PROGRAM") != "" {
		return true
	}
	return false
}

// isTerminalWriter reports whether w is a *os.File attached to a char device.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// colorize wraps s in an ANSI SGR sequence when color is enabled.
func colorize(s string, code int, extra ...string) string {
	if !logColorEnabled || s == "" {
		return s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[%dm", code)
	for _, e := range extra {
		b.WriteString(e)
	}
	b.WriteString(s)
	b.WriteString(ansiReset)
	return b.String()
}

// levelColor returns the ANSI color for a log level or exec status, mirroring
// the scheduler's getColorByLevel plus the exec-status palette.
func levelColor(level string) int {
	switch strings.ToUpper(level) {
	case "DEBUG", "TRACE", "RUNNING":
		return colorBlue
	case "WARN", "WARNING":
		return colorMagenta
	case "ERROR", "FATAL", "PANIC", "FAIL", "FAILED":
		return colorRed
	case "OK", "INFO", "NOTICE", "LOG", "SUCCESS", "SUCCEEDED":
		return colorGreen
	default:
		return colorGreen
	}
}

// tokenOpts controls which identity tokens are emitted. In tree mode, child
// lines suppress the chain, client and vxid tokens because the group header
// already establishes them (they would be visually redundant on every line).
type tokenOpts struct {
	omitChain  bool
	omitClient bool
	omitVxid   bool
}

// renderActivityText writes a single activity entry in the rich, identity-first
// format. width is the terminal width used to clamp the trailing message; pass
// 0 to disable clamping.
func renderActivityText(w io.Writer, e client.ActivityEntry, width int) {
	renderActivityLine(w, e, width, tokenOpts{})
}

// renderActivityLine is the shared rendering core; opts lets callers (tree mode)
// suppress tokens that are redundant in their layout.
func renderActivityLine(w io.Writer, e client.ActivityEntry, width int, opts tokenOpts) {
	var b strings.Builder

	// Timestamp (dimmed when color is on).
	b.WriteString(colorize(e.TS, colorGray, ansiDim))
	b.WriteByte(' ')

	// Level / status badge, left-padded to a stable width so messages align.
	badge := fmt.Sprintf("%-7s", e.Level)
	b.WriteString(colorize(badge, levelColor(e.Level), ansiBold))
	b.WriteByte(' ')

	// Identity tokens — only emitted when present (empty != zero).
	for _, tok := range identityTokens(e, opts) {
		b.WriteString(colorize(tok, colorBlue))
		b.WriteByte(' ')
	}

	// Message (clamped to remaining terminal width).
	msg := e.Message
	if width > 0 {
		msg = clampToWidth(msg, width-visibleLen(b.String()))
	}
	b.WriteString(msg)

	fmt.Fprintln(w, b.String())
}

// identityTokens builds the bracketed context tokens for an entry, omitting any
// that are absent. Order mirrors the scheduler formatter: chain, task, vxid,
// then exec-only ms/rc, then client. opts may suppress the chain/client tokens
// (tree mode child lines).
func identityTokens(e client.ActivityEntry, opts tokenOpts) []string {
	toks := make([]string, 0, 6)

	if e.ChainID > 0 && !opts.omitChain {
		if e.ChainName != "" {
			toks = append(toks, fmt.Sprintf("[chain:%d|%s]", e.ChainID, e.ChainName))
		} else {
			toks = append(toks, fmt.Sprintf("[chain:%d]", e.ChainID))
		}
	}
	if e.TaskID > 0 {
		toks = append(toks, fmt.Sprintf("[task:%d]", e.TaskID))
	}
	if e.Vxid != "" && !opts.omitVxid {
		toks = append(toks, fmt.Sprintf("[vxid:%s]", e.Vxid))
	}
	// MS / RC are meaningful only for exec rows; suppress the zeros log rows carry.
	if e.Source == "exec" {
		if e.DurationMS > 0 {
			toks = append(toks, fmt.Sprintf("[ms:%d]", e.DurationMS))
		}
		if e.Returncode != 0 {
			toks = append(toks, fmt.Sprintf("[rc:%d]", e.Returncode))
		}
	}
	if e.ClientName != "" && !opts.omitClient {
		toks = append(toks, fmt.Sprintf("[client:%s]", e.ClientName))
	}
	return toks
}

// clampToWidth trims s to at most n display columns, appending an ellipsis when
// truncated. n <= 0 disables clamping.
func clampToWidth(s string, n int) string {
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// visibleLen returns the printable length of s, ignoring ANSI SGR sequences so
// width clamping is based on what the user actually sees.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			n++
		}
	}
	return n
}

// effectiveLogFormat resolves the format for the log commands. The log commands
// default to the rich "text" view unless the user explicitly set --output to
// something else (cobra Changed). When --output was left at its global default
// ("table"), text wins because it is the better default for logs.
func effectiveLogFormat(outputChanged bool) outputFormat {
	if !outputChanged {
		return outputText
	}
	f, err := parseOutputFormat(opts.output)
	if err != nil {
		return outputText
	}
	return f
}

// terminalWidth returns the column width to clamp messages to. To stay
// dependency-free it reads the COLUMNS environment variable (exported by most
// interactive shells); when w is not a terminal or COLUMNS is unset/invalid it
// returns 0, which disables clamping so piped/redirected output stays complete.
func terminalWidth(w io.Writer) int {
	if !isTerminalWriter(w) {
		return 0
	}
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(c)); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// renderActivityList renders a full slice of entries for `log list` according
// to the effective format. entries is rendered newest-first as returned.
func renderActivityList(w io.Writer, entries []client.ActivityEntry, format outputFormat) error {
	switch format {
	case outputJSON:
		return renderJSON(w, entries)
	case outputTable:
		rows := make([][]string, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, activityRow(e))
		}
		return renderTable(w, activityHeaders(), rows)
	case outputTree:
		configureLogColor(w, false, opts.noColor)
		renderActivityTree(w, entries, terminalWidth(w))
		return nil
	default: // outputText
		configureLogColor(w, false, opts.noColor)
		width := terminalWidth(w)
		for _, e := range entries {
			renderActivityText(w, e, width)
		}
		return nil
	}
}

// Tree connectors printed before each grouped child line: a "tee" for every
// child except the last in its group, which uses the "corner" so the branch
// visibly terminates. Two character sets are provided: Unicode box-drawing
// (default) and a plain-ASCII fallback for terminals that cannot render UTF-8
// (e.g. a Windows console on a legacy code page). The active pair is chosen per
// run by configureLogColor → treeChars.
type treeConnectors struct {
	child string // intermediate child
	last  string // final child of a group
}

var (
	treeUnicode = treeConnectors{child: "├─ ", last: "└─ "}
	treeASCII   = treeConnectors{child: "|- ", last: "`- "}

	// treeChars is the connector set used by the renderer. It defaults to the
	// ASCII set so tests (and any caller that skips configureLogColor) are
	// deterministic; configureLogColor upgrades it to Unicode when supported.
	treeChars = treeASCII
)

// treeChildPrefix/treeLastPrefix expose the currently selected connectors.
func treeChildPrefix() string { return treeChars.child }
func treeLastPrefix() string  { return treeChars.last }

// renderActivityTree renders the run-grouped feed produced by
// client.ListActivityTree. All grouping/ordering is done in SQL: a chain run's
// lines arrive contiguously (header first, entry.IsHeader == true), and runs
// are interleaved with standalone chain-less scheduler rows by time (newest
// first). The renderer therefore only has to:
//   - render a run header with the full chain+client+vxid identity;
//   - render run children with chain/client/vxid suppressed (constant within
//     the run, already on the header);
//   - render chain-less rows (ChainID == 0) as standalone lines, keeping their
//     client token, so a system message can be spotted in its chronological
//     place between chain branches.
//
// A blank line separates each run branch and each chain-less system block from
// what precedes it, so every block reads as its own group. Consecutive
// chain-less rows are buffered and emitted in ascending time order, so a block
// of system messages reads chronologically even though the feed arrives
// newest-first (SQL anchors each system row by its own ts, which can leave them
// in descending order within an adjacent block).
func renderActivityTree(w io.Writer, entries []client.ActivityEntry, width int) {
	childOpts := tokenOpts{omitChain: true, omitClient: true, omitVxid: true}
	first := true

	// childPrefix returns the connector for a child at index i: the corner
	// (treeLastPrefix) when it is the last line of its group, else the tee. A
	// child is the last of its group when the next entry begins a new group
	// (a run header or a chain-less system row) or the feed ends.
	childPrefix := func(i int) string {
		if i+1 >= len(entries) || entries[i+1].IsHeader || entries[i+1].ChainID == 0 {
			return treeLastPrefix()
		}
		return treeChildPrefix()
	}

	var sysBuf []client.ActivityEntry
	flushSys := func() {
		if len(sysBuf) == 0 {
			return
		}
		// Ascending by timestamp within the block (TS is lexically sortable).
		sort.SliceStable(sysBuf, func(i, j int) bool { return sysBuf[i].TS < sysBuf[j].TS })
		if !first {
			fmt.Fprintln(w)
		}
		first = false
		for j, e := range sysBuf {
			prefix := treeChildPrefix()
			if j == len(sysBuf)-1 {
				prefix = treeLastPrefix()
			}
			renderTreeLine(w, e, prefix, width, tokenOpts{})
		}
		sysBuf = sysBuf[:0]
	}

	for i, e := range entries {
		switch {
		case e.ChainID == 0:
			// Buffer the contiguous system block; flushed (sorted) when a chain
			// branch interrupts it or at the end.
			sysBuf = append(sysBuf, e)
		case e.IsHeader:
			flushSys()
			if !first {
				fmt.Fprintln(w)
			}
			first = false
			renderTreeLine(w, e, "", width, tokenOpts{})
		default:
			flushSys()
			renderTreeLine(w, e, childPrefix(i), width, childOpts)
		}
	}
	flushSys()
}

// renderTreeLine renders one activity entry with an optional tree prefix,
// reusing the identity-first formatting. opts lets tree children suppress the
// chain/client/vxid tokens established by their run header. The prefix is
// dimmed and counts against the width budget so clamping stays accurate.
func renderTreeLine(w io.Writer, e client.ActivityEntry, prefix string, width int, opts tokenOpts) {
	if prefix == "" {
		renderActivityLine(w, e, width, opts)
		return
	}
	fmt.Fprint(w, colorize(prefix, colorGray, ansiDim))
	childWidth := width
	if childWidth > 0 {
		// Subtract the prefix's display width (runes), not its byte length —
		// the box-drawing connectors are multi-byte UTF-8.
		childWidth -= visibleLen(prefix)
	}
	renderActivityLine(w, e, childWidth, opts)
}

// newTailEmitter returns the per-entry callback used by `log tail`. For the
// text format it prints the Ctrl-C banner once and renders each entry richly;
// for JSON it streams NDJSON (one compact object per line) suitable for piping.
func newTailEmitter(w io.Writer, format outputFormat) func(client.ActivityEntry) {
	if format == outputJSON {
		enc := json.NewEncoder(w) // compact, newline-delimited
		return func(e client.ActivityEntry) {
			_ = enc.Encode(e)
		}
	}
	// text (and table, which is list-only and degrades to text here).
	fmt.Fprintln(w, "# pgtt log tail — press Ctrl-C to stop")
	configureLogColor(w, false, opts.noColor)
	width := terminalWidth(w)
	return func(e client.ActivityEntry) {
		renderActivityText(w, e, width)
	}
}

// activityHeaders/activityRow build the legacy aligned-table representation,
// kept for `--output table`. Identical columns to the original implementation
// but sourced from the renderer so both paths stay in sync.
func activityHeaders() []string {
	return []string{"TS", "SRC", "LEVEL", "CHAIN", "TASK", "MS", "RC", "CLIENT", "MESSAGE"}
}

func activityRow(e client.ActivityEntry) []string {
	chainStr := ""
	if e.ChainID > 0 {
		chainStr = strconv.FormatInt(e.ChainID, 10)
		if e.ChainName != "" {
			chainStr += "|" + e.ChainName
		}
	}
	taskStr := ""
	if e.TaskID > 0 {
		taskStr = strconv.FormatInt(e.TaskID, 10)
	}
	msStr := ""
	rcStr := ""
	if e.Source == "exec" {
		msStr = strconv.FormatInt(e.DurationMS, 10)
		rcStr = strconv.Itoa(e.Returncode)
	}
	return []string{
		e.TS, e.Source, e.Level,
		chainStr, taskStr, msStr, rcStr,
		e.ClientName, e.Message,
	}
}
