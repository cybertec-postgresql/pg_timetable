package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

func actEntry(level, name, msg string, id int64) client.ActivityEntry {
	return client.ActivityEntry{
		TS:        "2026-06-23 10:00:00",
		Source:    "log",
		Level:     level,
		ChainID:   id,
		ChainName: name,
		Message:   msg,
	}
}

// --- renderer tests -------------------------------------------------------

func TestRenderActivityLine_IdentityTokens(t *testing.T) {
	s := newStyles(false)
	e := actEntry("INFO", "etl", "started", 5)
	e.TaskID = 2
	e.Vxid = "12345"
	out := s.renderActivityLine(e, 0)
	for _, want := range []string{"[chain:5|etl]", "[task:2]", "[vxid:12345]", "started", "INFO"} {
		if !strings.Contains(out, want) {
			t.Fatalf("line missing %q: %q", want, out)
		}
	}
}

func TestRenderActivityLine_OmitsEmptyTokens(t *testing.T) {
	s := newStyles(false)
	e := actEntry("INFO", "", "plain", 0) // no chain/task/vxid
	out := s.renderActivityLine(e, 0)
	for _, bad := range []string{"[chain:", "[task:", "[vxid:"} {
		if strings.Contains(out, bad) {
			t.Fatalf("line should omit %q: %q", bad, out)
		}
	}
}

func TestRenderActivityLine_ExecMsRc(t *testing.T) {
	s := newStyles(false)
	e := client.ActivityEntry{TS: "t", Source: "exec", Level: "FAIL", ChainID: 1, DurationMS: 42, Returncode: 3, Message: "boom"}
	out := s.renderActivityLine(e, 0)
	if !strings.Contains(out, "[ms:42]") || !strings.Contains(out, "[rc:3]") {
		t.Fatalf("exec line missing ms/rc: %q", out)
	}
}

func TestIdentityTokensChainOnly(t *testing.T) {
	toks := identityTokens(client.ActivityEntry{ChainID: 9})
	if len(toks) != 1 || toks[0] != "[chain:9]" {
		t.Fatalf("tokens = %v, want [chain:9]", toks)
	}
}

// --- view logic tests -----------------------------------------------------

func loadedActivityView() *activityView {
	v := newActivityView(nil, newStyles(false), client.LogFilter{})
	v.SetSize(120, 12)
	back := []client.ActivityEntry{
		actEntry("INFO", "a", "one", 1),
		actEntry("INFO", "a", "two", 1),
	}
	uv, _ := v.Update(activityBackfillMsg{entries: back})
	return uv.(*activityView)
}

func TestActivityBackfillSeedsRing(t *testing.T) {
	v := loadedActivityView()
	if len(v.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(v.entries))
	}
	out := v.Body(120, 10)
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Fatalf("body missing backfilled entries:\n%s", out)
	}
}

func TestActivityStreamAppends(t *testing.T) {
	v := loadedActivityView()
	v.ch = make(chan client.ActivityEntry, 1) // so the re-issued wait cmd is valid
	uv, cmd := v.Update(activityStreamMsg{entry: actEntry("WARN", "a", "three", 1), ok: true})
	v = uv.(*activityView)
	if len(v.entries) != 3 || v.entries[2].Message != "three" {
		t.Fatalf("stream did not append: %d entries", len(v.entries))
	}
	if cmd == nil {
		t.Fatal("stream msg should re-issue the wait command")
	}
}

func TestActivityStreamEndStopsPumping(t *testing.T) {
	v := loadedActivityView()
	_, cmd := v.Update(activityStreamMsg{ok: false})
	if cmd != nil {
		t.Fatal("closed stream should not re-issue a wait command")
	}
}

func TestActivityFreezeAndScroll(t *testing.T) {
	v := loadedActivityView()
	// Up freezes and scrolls back.
	v.Update(tea.KeyMsg{Type: tea.KeyUp})
	if !v.frozen || v.offset != 1 {
		t.Fatalf("after up: frozen=%v offset=%d, want true/1", v.frozen, v.offset)
	}
	// Down to bottom unfreezes.
	v.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v.frozen || v.offset != 0 {
		t.Fatalf("after down: frozen=%v offset=%d, want false/0", v.frozen, v.offset)
	}
	// 'f' toggles freeze.
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if !v.frozen {
		t.Fatal("f did not freeze")
	}
}

// TestActivityScrollKeepsFullWindow guards the reported bug: repeatedly
// pressing Up (or jumping to the top with 'g') must never scroll the oldest
// entry off the top and shrink the window down to a single line. The offset is
// clamped so a full window (viewH lines) always stays visible.
func TestActivityScrollKeepsFullWindow(t *testing.T) {
	v := newActivityView(nil, newStyles(false), client.LogFilter{})
	v.SetSize(120, 12)
	entries := make([]client.ActivityEntry, 0, 50)
	for i := 0; i < 50; i++ {
		entries = append(entries, actEntry("INFO", "a", "m", 1))
	}
	uv, _ := v.Update(activityBackfillMsg{entries: entries})
	v = uv.(*activityView)

	// Render once so viewH is known, then spam Up far past the buffer size.
	v.Body(120, 12)
	for i := 0; i < 200; i++ {
		v.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	if want := v.maxOffset(); v.offset != want {
		t.Fatalf("after 200 Up: offset=%d, want clamped to %d", v.offset, want)
	}
	// The visible window must still be full (viewH lines), not a single line.
	visible := len(v.entries) - v.offset
	if visible < v.viewH {
		t.Fatalf("window shrank to %d lines, want a full window of %d", visible, v.viewH)
	}

	// 'g' (top) must land on the same full-window offset, not len-1.
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if v.offset != v.maxOffset() {
		t.Fatalf("g offset=%d, want %d", v.offset, v.maxOffset())
	}
	if got := len(v.entries) - v.offset; got < v.viewH {
		t.Fatalf("g window = %d lines, want full window of %d", got, v.viewH)
	}
}

func TestActivityRingCap(t *testing.T) {
	v := newActivityView(nil, newStyles(false), client.LogFilter{})
	for i := 0; i < activityRingCap+50; i++ {
		v.append(actEntry("INFO", "a", "m", 1))
	}
	if len(v.entries) != activityRingCap {
		t.Fatalf("ring size = %d, want cap %d", len(v.entries), activityRingCap)
	}
}

func TestActivityTitleReflectsFilter(t *testing.T) {
	cases := []struct {
		f    client.LogFilter
		want string
	}{
		{client.LogFilter{}, "Activity"},
		{client.LogFilter{ChainID: 7}, "Activity (chain 7)"},
		{client.LogFilter{ClientName: "w1"}, "Activity (w1)"},
		{client.LogFilter{ChainID: 7, ClientName: "w1"}, "Activity (chain 7, w1)"},
	}
	for _, c := range cases {
		v := newActivityView(nil, newStyles(false), c.f)
		if got := v.Title(); got != c.want {
			t.Fatalf("Title(%+v) = %q, want %q", c.f, got, c.want)
		}
	}
}

func TestActivityBackfillError(t *testing.T) {
	v := newActivityView(nil, newStyles(false), client.LogFilter{})
	_, cmd := v.Update(activityBackfillMsg{err: errTest("down")})
	if cmd == nil {
		t.Fatal("no command on error")
	}
	if _, ok := cmd().(errMsg); !ok {
		t.Fatal("expected errMsg")
	}
}

// --- streaming bridge integration (fake client) ---------------------------

// fakeTailClient implements only ListActivity + TailActivity; the rest of the
// Client interface is unused by activityView and panics if called.
type fakeTailClient struct {
	client.Client
	backfill []client.ActivityEntry
	stream   []client.ActivityEntry
}

func (f *fakeTailClient) ListActivity(_ context.Context, _ client.LogFilter) ([]client.ActivityEntry, error) {
	return f.backfill, nil
}

func (f *fakeTailClient) TailActivity(ctx context.Context, _ client.LogFilter, emit func(client.ActivityEntry)) error {
	for _, e := range f.stream {
		select {
		case <-ctx.Done():
			return nil
		default:
			emit(e)
		}
	}
	<-ctx.Done() // block like the real poller until cancelled
	return nil
}

func TestActivityTailBridgeDeliversEntries(t *testing.T) {
	fc := &fakeTailClient{
		backfill: []client.ActivityEntry{actEntry("INFO", "a", "hist", 1)},
		stream:   []client.ActivityEntry{actEntry("INFO", "a", "live1", 1), actEntry("WARN", "a", "live2", 1)},
	}
	v := newActivityView(fc, newStyles(false), client.LogFilter{})
	v.SetSize(100, 10)
	defer v.Close()

	cmd := v.startTail()
	if cmd == nil {
		t.Fatal("startTail returned no command")
	}

	// Drain the first two streamed entries via the wait command.
	got := 0
	deadline := time.After(2 * time.Second)
	for got < 2 {
		select {
		case <-deadline:
			t.Fatalf("only received %d/2 streamed entries", got)
		default:
		}
		msg := cmd()
		sm, ok := msg.(activityStreamMsg)
		if !ok {
			t.Fatalf("unexpected msg %T", msg)
		}
		if !sm.ok {
			t.Fatal("stream closed early")
		}
		uv, next := v.Update(sm)
		v = uv.(*activityView)
		cmd = next
		got++
	}
	if v.entries[len(v.entries)-1].Message != "live2" {
		t.Fatalf("last entry = %q, want live2", v.entries[len(v.entries)-1].Message)
	}
}
