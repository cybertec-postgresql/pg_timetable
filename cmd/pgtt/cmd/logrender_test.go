package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bytes.Buffer is not an *os.File, so configureLogColor disables color and the
// renderer output is deterministic — perfect for golden assertions.

func renderOneText(e client.ActivityEntry) string {
	var buf bytes.Buffer
	configureLogColor(&buf, false, false) // buffer => color off
	renderActivityText(&buf, e, 0)        // width 0 => no clamping
	return buf.String()
}

// TestRenderActivityText_Golden asserts the exact identity-first rendering for a
// representative set of entries, with color off (P7-5/P7-11).
func TestRenderActivityText_Golden(t *testing.T) {
	for _, tt := range []struct {
		name  string
		entry client.ActivityEntry
		want  string
	}{
		{
			name: "log row with chain name + task + vxid",
			entry: client.ActivityEntry{
				TS: "2026-06-23 16:08:43.387", Source: "log", Level: "INFO",
				ChainID: 1, ChainName: "notify_every_minute", TaskID: 1,
				Vxid: "21474836598", ClientName: "demo_worker", Message: "Starting task",
			},
			want: "2026-06-23 16:08:43.387 INFO    [chain:1|notify_every_minute] [task:1] [vxid:21474836598] [client:demo_worker] Starting task\n",
		},
		{
			name: "log row without context omits empty tokens",
			entry: client.ActivityEntry{
				TS: "2026-06-23 16:08:43.385", Source: "log", Level: "INFO",
				ClientName: "demo_worker", Message: "Retrieve scheduled chains to run",
			},
			want: "2026-06-23 16:08:43.385 INFO    [client:demo_worker] Retrieve scheduled chains to run\n",
		},
		{
			name: "exec OK row shows ms and chain, suppresses rc=0",
			entry: client.ActivityEntry{
				TS: "2026-06-23 16:08:43.398", Source: "exec", Level: "OK",
				ChainID: 1, TaskID: 1, Vxid: "21474836598", DurationMS: 7,
				ClientName: "demo_worker", Message: "SELECT 1",
			},
			want: "2026-06-23 16:08:43.398 OK      [chain:1] [task:1] [vxid:21474836598] [ms:7] [client:demo_worker] SELECT 1\n",
		},
		{
			name: "exec FAIL row shows rc",
			entry: client.ActivityEntry{
				TS: "2026-06-23 16:08:43.400", Source: "exec", Level: "FAIL",
				ChainID: 2, TaskID: 3, DurationMS: 12, Returncode: 1,
				ClientName: "demo_worker", Message: "boom",
			},
			want: "2026-06-23 16:08:43.400 FAIL    [chain:2] [task:3] [ms:12] [rc:1] [client:demo_worker] boom\n",
		},
		{
			name: "notice row uses severity as level",
			entry: client.ActivityEntry{
				TS: "2026-06-23 16:08:43.398", Source: "log", Level: "NOTICE",
				ClientName: "demo_worker", Severity: "NOTICE",
				Notice:  `Message by demo_worker from chain 2: "Hey"`,
				Message: "Notice received",
			},
			want: "2026-06-23 16:08:43.398 NOTICE  [client:demo_worker] Notice received\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderOneText(tt.entry))
		})
	}
}

// TestRenderActivityText_NoZeros guards the core promise of Phase 7: log rows
// never render the "0 0 0" columns the old table produced.
func TestRenderActivityText_NoZeros(t *testing.T) {
	out := renderOneText(client.ActivityEntry{
		TS: "2026-06-23 16:08:43.385", Source: "log", Level: "INFO",
		ClientName: "demo_worker", Message: "Chain executed successfully",
	})
	assert.NotContains(t, out, "[chain:0]")
	assert.NotContains(t, out, "[task:0]")
	assert.NotContains(t, out, "[ms:0]")
	assert.NotContains(t, out, "[rc:0]")
}

// TestConfigureLogColor_Gates verifies the color gate honors JSON, --no-color,
// NO_COLOR, and non-TTY writers (P7-6).
func TestConfigureLogColor_Gates(t *testing.T) {
	var buf bytes.Buffer

	configureLogColor(&buf, true, false) // json => off
	assert.False(t, logColorEnabled)

	configureLogColor(&buf, false, true) // --no-color => off
	assert.False(t, logColorEnabled)

	t.Setenv("NO_COLOR", "1")
	configureLogColor(&buf, false, false) // NO_COLOR => off
	assert.False(t, logColorEnabled)
	os.Unsetenv("NO_COLOR")

	configureLogColor(&buf, false, false) // buffer is not a TTY => off
	assert.False(t, logColorEnabled)
}

// TestColorize verifies colorize wraps text only when enabled.
func TestColorize(t *testing.T) {
	logColorEnabled = false
	assert.Equal(t, "hi", colorize("hi", colorGreen))

	logColorEnabled = true
	got := colorize("hi", colorGreen)
	assert.True(t, strings.HasPrefix(got, "\x1b[32m"))
	assert.True(t, strings.HasSuffix(got, ansiReset))
	assert.Contains(t, got, "hi")
	logColorEnabled = false // reset for other tests
}

// TestClampToWidth verifies message clamping with ellipsis.
func TestClampToWidth(t *testing.T) {
	assert.Equal(t, "hello", clampToWidth("hello", 0))      // disabled
	assert.Equal(t, "hello", clampToWidth("hello", 10))     // fits
	assert.Equal(t, "hel…", clampToWidth("hello world", 4)) // truncated
	assert.Equal(t, "…", clampToWidth("hello", 1))          // minimal
}

// TestVisibleLen ignores ANSI escapes when measuring width.
func TestVisibleLen(t *testing.T) {
	assert.Equal(t, 5, visibleLen("hello"))
	assert.Equal(t, 2, visibleLen("\x1b[32mhi\x1b[0m"))
}

// TestActivityRow verifies the legacy table row keeps exec-only ms/rc and adds
// the chain name to the CHAIN column.
func TestActivityRow(t *testing.T) {
	logRow := activityRow(client.ActivityEntry{
		Source: "log", TS: "t", Level: "INFO", ChainID: 1, ChainName: "n",
		ClientName: "w", Message: "m",
	})
	require.Len(t, logRow, len(activityHeaders()))
	assert.Equal(t, "1|n", logRow[3]) // CHAIN id|name
	assert.Equal(t, "", logRow[5])    // MS empty for log rows
	assert.Equal(t, "", logRow[6])    // RC empty for log rows

	execRow := activityRow(client.ActivityEntry{
		Source: "exec", TS: "t", Level: "OK", ChainID: 2, TaskID: 4,
		DurationMS: 9, Returncode: 0, ClientName: "w", Message: "SELECT 1",
	})
	assert.Equal(t, "9", execRow[5]) // MS shown for exec
	assert.Equal(t, "0", execRow[6]) // RC shown (even 0) for exec
}

// TestNewTailEmitter_JSON verifies the tail JSON emitter streams NDJSON.
func TestNewTailEmitter_JSON(t *testing.T) {
	var buf bytes.Buffer
	emit := newTailEmitter(&buf, outputJSON)
	emit(client.ActivityEntry{TS: "t1", Source: "log", Level: "INFO", Message: "a"})
	emit(client.ActivityEntry{TS: "t2", Source: "exec", Level: "OK", Message: "b"})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 2, "one JSON object per line")
	assert.Contains(t, lines[0], `"ts":"t1"`)
	assert.Contains(t, lines[1], `"ts":"t2"`)
}

// TestNewTailEmitter_TextBanner verifies the text tail prints the Ctrl-C banner.
func TestNewTailEmitter_TextBanner(t *testing.T) {
	var buf bytes.Buffer
	emit := newTailEmitter(&buf, outputText)
	emit(client.ActivityEntry{
		TS: "t1", Source: "log", Level: "INFO", ChainID: 1, ClientName: "w", Message: "go",
	})
	out := buf.String()
	assert.Contains(t, out, "press Ctrl-C to stop")
	assert.Contains(t, out, "[chain:1]")
	assert.Contains(t, out, "go")
}

func renderTree(entries []client.ActivityEntry) string {
	var buf bytes.Buffer
	configureLogColor(&buf, false, false) // color off; treeChars reset to ASCII
	renderActivityTree(&buf, entries, 0)  // no clamping
	return buf.String()
}

// isTreeChild reports whether line l carries a tree child connector (either the
// intermediate tee or the final corner), accepting both the ASCII and Unicode
// connector sets so tests are independent of the runtime Unicode detection.
func isTreeChild(l string) bool {
	return strings.HasPrefix(l, treeASCII.child) ||
		strings.HasPrefix(l, treeASCII.last) ||
		strings.HasPrefix(l, treeUnicode.child) ||
		strings.HasPrefix(l, treeUnicode.last)
}

// isTreeLast reports whether line l uses the final-child connector.
func isTreeLast(l string) bool {
	return strings.HasPrefix(l, treeASCII.last) || strings.HasPrefix(l, treeUnicode.last)
}

// The renderer consumes rows already grouped and ordered by SQL
// (client.ListActivityTree). The fixtures below mirror that contract: rows are
// in final display order, the run's first line carries IsHeader, and the run's
// vxid has been broadcast onto every line (including the vxid-less header).

// TestRenderActivityTree_HeaderAndChildren verifies the header carries the full
// chain+client+vxid identity, children are indented with "|- " and drop the
// chain/client/vxid tokens (constant within the run), and a blank line precedes
// each new run.
func TestRenderActivityTree_HeaderAndChildren(t *testing.T) {
	entries := []client.ActivityEntry{
		// Run 1 (chain 1), header first.
		{TS: "16:08:43.387", Source: "log", Level: "INFO", ChainID: 1, ChainName: "one", Vxid: "11", ClientName: "demo_worker", Message: "Starting chain", IsHeader: true},
		{TS: "16:08:43.397", Source: "exec", Level: "OK", ChainID: 1, ChainName: "one", TaskID: 1, Vxid: "11", DurationMS: 7, ClientName: "demo_worker", Message: "SELECT 1"},
		{TS: "16:08:43.405", Source: "log", Level: "INFO", ChainID: 1, ChainName: "one", Vxid: "11", ClientName: "demo_worker", Message: "Chain executed successfully"},
		// Run 2 (chain 2), header first.
		{TS: "16:08:43.385", Source: "log", Level: "INFO", ChainID: 2, ChainName: "two", Vxid: "22", ClientName: "demo_worker", Message: "Starting chain", IsHeader: true},
		{TS: "16:08:43.398", Source: "exec", Level: "OK", ChainID: 2, ChainName: "two", TaskID: 2, Vxid: "22", DurationMS: 5, ClientName: "demo_worker", Message: "SELECT 1"},
	}
	out := renderTree(entries)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 6, "5 rows + 1 blank separator line:\n%s", out)

	// Run 1 header: no child connector.
	assert.False(t, isTreeChild(lines[0]))
	assert.Contains(t, lines[0], "[chain:1|one]")
	assert.Contains(t, lines[0], "[client:demo_worker]")
	assert.Contains(t, lines[0], "[vxid:11]")
	assert.Contains(t, lines[0], "Starting chain")

	// Run 1 intermediate child: tee connector.
	assert.True(t, isTreeChild(lines[1]))
	assert.False(t, isTreeLast(lines[1]), "intermediate child must not use the corner")
	assert.NotContains(t, lines[1], "[chain:")
	assert.NotContains(t, lines[1], "[client:")
	assert.NotContains(t, lines[1], "[vxid:")
	assert.Contains(t, lines[1], "[task:1]")
	assert.Contains(t, lines[1], "SELECT 1")

	// Run 1 last child: corner connector.
	assert.True(t, isTreeLast(lines[2]), "last child of a run must use the corner connector")
	assert.Contains(t, lines[2], "Chain executed successfully")

	// Blank line separates the two runs.
	assert.Equal(t, "", lines[3])

	// Run 2 header: no child connector.
	assert.False(t, isTreeChild(lines[4]))
	assert.Contains(t, lines[4], "[chain:2|two]")
	assert.Contains(t, lines[4], "[vxid:22]")

	// Run 2 last child: corner connector (only one child).
	assert.True(t, isTreeLast(lines[5]), "sole child is also the last child")
}

// TestRenderActivityTree_SystemLinesInterleave verifies chain-less scheduler
// rows are rendered as standalone "|- " lines (no "(no chain)" heading),
// interleaved between chain branches by time, keeping their client token, and
// that consecutive system lines form one block (no blank line between them).
func TestRenderActivityTree_SystemLinesInterleave(t *testing.T) {
	// As SQL returns it (newest-first): a run branch, then a block of system
	// lines in DESCENDING ts, then an older run branch. The renderer must place
	// the system block between the runs AND re-sort it ascending for display.
	entries := []client.ActivityEntry{
		{TS: "17:11:00.000", Source: "log", Level: "INFO", ChainID: 1, ChainName: "one", Vxid: "11", ClientName: "w", Message: "Starting chain", IsHeader: true},
		{TS: "17:11:00.010", Source: "exec", Level: "OK", ChainID: 1, ChainName: "one", TaskID: 1, Vxid: "11", ClientName: "w", Message: "SELECT 1"},
		// system block, delivered newest-first (Notice .235 before Retrieve .228)
		{TS: "17:10:43.235", Source: "log", Level: "NOTICE", ClientName: "w", Severity: "NOTICE", Message: "Notice received"},
		{TS: "17:10:43.228", Source: "log", Level: "INFO", ClientName: "w", Message: "Retrieve scheduled chains to run"},
		// older run
		{TS: "17:10:00.000", Source: "log", Level: "INFO", ChainID: 2, ChainName: "two", Vxid: "22", ClientName: "w", Message: "Starting chain", IsHeader: true},
	}
	out := renderTree(entries)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// No "(no chain)" heading anymore.
	assert.NotContains(t, out, "(no chain)")

	// Each system line carries a child connector (tee or corner) and keeps the client token.
	for _, l := range lines {
		if strings.Contains(l, "Retrieve scheduled chains to run") || strings.Contains(l, "Notice received") {
			assert.True(t, isTreeChild(l), "system line must carry a child connector: %q", l)
			assert.Contains(t, l, "[client:w]")
			assert.NotContains(t, l, "[chain:")
		}
	}

	// The system block sits between the two run branches (chain 1 ... system ... chain 2).
	idxChain1 := indexOfLine(lines, "[chain:1|one]")
	idxRetrieve := indexOfLine(lines, "Retrieve scheduled chains to run")
	idxNotice := indexOfLine(lines, "Notice received")
	idxChain2 := indexOfLine(lines, "[chain:2|two]")
	require.True(t, idxChain1 >= 0 && idxRetrieve >= 0 && idxNotice >= 0 && idxChain2 >= 0)
	assert.Less(t, idxChain1, idxRetrieve, "system block after the newest run")
	assert.Less(t, idxNotice, idxChain2, "system block before the older run")

	// Within the block, order is ascending by ts: Retrieve (.228) before Notice (.235),
	// even though SQL delivered them newest-first.
	assert.Less(t, idxRetrieve, idxNotice, "system block reads chronologically (ascending)")
	assert.Equal(t, idxRetrieve+1, idxNotice, "consecutive system lines stay together")

	// The last system line (Notice, highest ts) uses the corner connector.
	assert.True(t, isTreeLast(lines[idxNotice]), "last system line must use the corner connector")
}

func indexOfLine(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

// TestParseOutputFormat_Tree verifies the tree value is accepted.
func TestParseOutputFormat_Tree(t *testing.T) {
	got, err := parseOutputFormat("tree")
	require.NoError(t, err)
	assert.Equal(t, outputTree, got)
}
