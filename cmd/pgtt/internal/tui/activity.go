package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// activityRingCap bounds the number of retained activity lines so a long-lived
// stream never grows without limit.
const activityRingCap = 2000

// activityBackfillMsg carries the initial ListActivity snapshot.
type activityBackfillMsg struct {
	entries []client.ActivityEntry
	err     error
}

// activityStreamMsg carries one live entry delivered by the tail goroutine, or
// signals the stream ended (ok == false).
type activityStreamMsg struct {
	entry client.ActivityEntry
	ok    bool
}

// activityView is a live, colored, filterable activity stream backed by
// client.ListActivity (initial backfill) + client.TailActivity (live). The
// synchronous emit callback is bridged onto a channel and pumped into the
// Bubble Tea loop one entry at a time.
type activityView struct {
	client client.Client
	styles styles
	filter client.LogFilter

	entries []client.ActivityEntry // ring buffer (oldest first)
	offset  int                    // scroll offset from the bottom (0 = newest)
	frozen  bool                   // autoscroll paused?

	width, hght int

	// streaming plumbing
	ch     chan client.ActivityEntry
	cancel context.CancelFunc

	keys activityKeyMap
}

type activityKeyMap struct {
	Freeze key.Binding
	Top    key.Binding
	Bottom key.Binding
}

func defaultActivityKeyMap() activityKeyMap {
	return activityKeyMap{
		Freeze: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "freeze")),
		Top:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
		Bottom: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
	}
}

func newActivityView(c client.Client, s styles, f client.LogFilter) *activityView {
	return &activityView{client: c, styles: s, filter: f, keys: defaultActivityKeyMap()}
}

func (v *activityView) Title() string {
	switch {
	case v.filter.ChainID > 0 && v.filter.ClientName != "":
		return fmt.Sprintf("Activity (chain %d, %s)", v.filter.ChainID, v.filter.ClientName)
	case v.filter.ChainID > 0:
		return fmt.Sprintf("Activity (chain %d)", v.filter.ChainID)
	case v.filter.ClientName != "":
		return "Activity (" + v.filter.ClientName + ")"
	default:
		return "Activity"
	}
}

func (v *activityView) SetSize(w, h int) { v.width, v.hght = w, h }

// Init backfills the recent history then starts the live tail.
func (v *activityView) Init() tea.Cmd {
	return tea.Batch(v.backfill(), v.startTail())
}

func (v *activityView) backfill() tea.Cmd {
	c, f := v.client, v.filter
	return func() tea.Msg {
		entries, err := c.ListActivity(context.Background(), f)
		return activityBackfillMsg{entries: entries, err: err}
	}
}

// startTail launches the tail goroutine (bridging emit→channel) and returns the
// command that waits for the first streamed entry.
func (v *activityView) startTail() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	v.ch = make(chan client.ActivityEntry, 256)

	c, f, ch := v.client, v.filter, v.ch
	go func() {
		defer close(ch)
		_ = c.TailActivity(ctx, f, func(e client.ActivityEntry) {
			select {
			case ch <- e:
			case <-ctx.Done():
			}
		})
	}()
	return waitForActivity(v.ch)
}

// waitForActivity reads one entry from ch and returns it as a message. When the
// channel closes (tail stopped), it reports ok == false.
func waitForActivity(ch chan client.ActivityEntry) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		return activityStreamMsg{entry: e, ok: ok}
	}
}

// Close cancels the tail goroutine (see closer).
func (v *activityView) Close() {
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}

func (v *activityView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case activityBackfillMsg:
		if msg.err != nil {
			return v, func() tea.Msg { return errMsg{msg.err} }
		}
		// Backfill seeds the ring; live entries append after.
		v.entries = trimRing(msg.entries)
		return v, func() tea.Msg { return statusMsg(fmt.Sprintf("%d entries", len(v.entries))) }

	case activityStreamMsg:
		if !msg.ok {
			return v, nil // stream ended (view closing)
		}
		v.append(msg.entry)
		// Keep pumping the channel.
		return v, waitForActivity(v.ch)

	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

func (v *activityView) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	switch {
	case key.Matches(msg, v.keys.Freeze):
		v.frozen = !v.frozen
		state := "live"
		if v.frozen {
			state = "frozen"
		}
		return v, func() tea.Msg { return statusMsg("scroll: " + state) }
	case key.Matches(msg, v.keys.Top):
		v.frozen = true
		v.offset = maxInt(0, len(v.entries)-1)
	case key.Matches(msg, v.keys.Bottom):
		v.offset = 0
		v.frozen = false
	case key.Matches(msg, defaultKeyMap().Up):
		v.frozen = true
		v.offset = minInt(v.offset+1, maxInt(0, len(v.entries)-1))
	case key.Matches(msg, defaultKeyMap().Down):
		v.offset = maxInt(0, v.offset-1)
		if v.offset == 0 {
			v.frozen = false
		}
	}
	return v, nil
}

// append adds a live entry to the ring. When not frozen the view stays pinned
// to the newest line; when frozen the scroll offset is preserved.
func (v *activityView) append(e client.ActivityEntry) {
	v.entries = append(v.entries, e)
	if len(v.entries) > activityRingCap {
		v.entries = v.entries[len(v.entries)-activityRingCap:]
	}
	if v.frozen && v.offset > 0 {
		// Keep the same line in view as new ones arrive below.
		v.offset = minInt(v.offset+1, maxInt(0, len(v.entries)-1))
	}
}

func trimRing(entries []client.ActivityEntry) []client.ActivityEntry {
	if len(entries) > activityRingCap {
		return entries[len(entries)-activityRingCap:]
	}
	// Copy so later appends don't alias the client's slice.
	out := make([]client.ActivityEntry, len(entries))
	copy(out, entries)
	return out
}

func (v *activityView) Body(width, height int) string {
	panelH := height - 1 // reserve one line for the status/help hint below
	if panelH < 3 {
		panelH = 3
	}
	innerW, innerH := v.styles.innerSize(width, panelH)

	var content string
	if len(v.entries) == 0 {
		content = v.styles.dim.Render("(waiting for activity…)")
	} else {
		// The window ends at len-offset and shows up to innerH lines.
		end := len(v.entries) - v.offset
		if end < 1 {
			end = 1
		}
		start := maxInt(0, end-innerH)
		var lines []string
		for i := start; i < end; i++ {
			lines = append(lines, v.styles.renderActivityLine(v.entries[i], innerW))
		}
		content = strings.Join(lines, "\n")
	}

	state := "live"
	if v.frozen {
		state = fmt.Sprintf("frozen ↑%d", v.offset)
	}
	title := fmt.Sprintf("Activity · %s [%d]", state, len(v.entries))
	return v.styles.panel(title, true, width, panelH, content) + "\n" + v.hint()
}

func (v *activityView) hint() string {
	return v.styles.dim.Render(
		"  f freeze · g/G top/bottom · ↑/↓ scroll · esc back")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
