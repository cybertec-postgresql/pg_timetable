package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// detailPane identifies which sub-table holds the cursor.
type detailPane int

const (
	paneTasks detailPane = iota
	paneRuns
)

// detailLoadedMsg carries the ShowChain result.
type detailLoadedMsg struct {
	chain *client.ChainListItem
	tasks []client.ChainTask
	err   error
}

// runsLoadedMsg carries the ListRuns result.
type runsLoadedMsg struct {
	runs []client.RunSummary
	err  error
}

const detailRunsLimit = 20

// detailView shows a single chain: its header, ordered tasks (top pane) and
// recent runs (bottom pane). Backed by client.ShowChain + client.ListRuns.
type detailView struct {
	client client.Client
	styles styles

	ref  string // chain id (string) used for ShowChain/ListRuns
	name string // best-known chain name (for the title/breadcrumb)

	chain *client.ChainListItem
	tasks []client.ChainTask
	runs  []client.RunSummary

	focus       detailPane
	taskCursor  int
	runCursor   int
	width, hght int

	keys detailKeyMap
}

type detailKeyMap struct {
	Switch key.Binding
}

func defaultDetailKeyMap() detailKeyMap {
	return detailKeyMap{
		Switch: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "tasks/runs")),
	}
}

func newDetailView(c client.Client, s styles, ref, name string) *detailView {
	return &detailView{client: c, styles: s, ref: ref, name: name, keys: defaultDetailKeyMap()}
}

func (v *detailView) Title() string {
	if v.name != "" {
		return v.name
	}
	return "chain " + v.ref
}

func (v *detailView) Init() tea.Cmd { return tea.Batch(v.fetchChain(), v.fetchRuns()) }

func (v *detailView) SetSize(w, h int) { v.width, v.hght = w, h }

func (v *detailView) fetchChain() tea.Cmd {
	c, ref := v.client, v.ref
	return func() tea.Msg {
		ch, tasks, err := c.ShowChain(context.Background(), ref)
		return detailLoadedMsg{chain: ch, tasks: tasks, err: err}
	}
}

func (v *detailView) fetchRuns() tea.Cmd {
	c, ref := v.client, v.ref
	return func() tea.Msg {
		runs, err := c.ListRuns(context.Background(), ref, detailRunsLimit)
		return runsLoadedMsg{runs: runs, err: err}
	}
}

func (v *detailView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		return v, tea.Batch(v.fetchChain(), v.fetchRuns())

	case detailLoadedMsg:
		if msg.err != nil {
			return v, func() tea.Msg { return errMsg{msg.err} }
		}
		v.chain = msg.chain
		v.tasks = msg.tasks
		if v.chain != nil && v.chain.ChainName != "" {
			v.name = v.chain.ChainName
		}
		v.clampCursors()
		return v, nil

	case runsLoadedMsg:
		if msg.err != nil {
			return v, func() tea.Msg { return errMsg{msg.err} }
		}
		v.runs = msg.runs
		v.clampCursors()
		return v, nil

	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

func (v *detailView) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	switch {
	case key.Matches(msg, v.keys.Switch):
		if v.focus == paneTasks {
			v.focus = paneRuns
		} else {
			v.focus = paneTasks
		}
	case key.Matches(msg, defaultKeyMap().Up):
		v.move(-1)
	case key.Matches(msg, defaultKeyMap().Down):
		v.move(1)
	case key.Matches(msg, defaultKeyMap().Enter):
		return v, v.openSelectedRun()
	}
	return v, nil
}

func (v *detailView) move(d int) {
	if v.focus == paneTasks {
		v.taskCursor = clamp(v.taskCursor+d, len(v.tasks))
	} else {
		v.runCursor = clamp(v.runCursor+d, len(v.runs))
	}
}

func (v *detailView) clampCursors() {
	v.taskCursor = clamp(v.taskCursor, len(v.tasks))
	v.runCursor = clamp(v.runCursor, len(v.runs))
}

// openSelectedRun pushes the per-task run-detail view for the highlighted run.
func (v *detailView) openSelectedRun() tea.Cmd {
	if v.focus != paneRuns || v.runCursor < 0 || v.runCursor >= len(v.runs) {
		return nil
	}
	run := v.runs[v.runCursor]
	return pushView(newRunDetailView(v.client, v.styles, run))
}

func (v *detailView) Body(width, height int) string {
	var b strings.Builder
	b.WriteString(v.headerBlock(width))
	b.WriteByte('\n')

	// Split the remaining height between the two panes (tasks slightly larger).
	remaining := height - 4 // header block (3 lines) + spacer
	if remaining < 4 {
		remaining = 4
	}
	tasksH := remaining/2 + remaining%2
	runsH := remaining - tasksH

	b.WriteString(v.paneTitle("Tasks", v.focus == paneTasks))
	b.WriteByte('\n')
	b.WriteString(v.tasksTable(width, tasksH))
	b.WriteByte('\n')
	b.WriteString(v.paneTitle("Recent runs", v.focus == paneRuns))
	b.WriteByte('\n')
	b.WriteString(v.runsTable(width, runsH))
	return b.String()
}

func (v *detailView) paneTitle(label string, focused bool) string {
	if focused {
		return v.styles.title.Render("▌ " + label)
	}
	return v.styles.dim.Render("  " + label)
}

func (v *detailView) headerBlock(_ int) string {
	if v.chain == nil {
		return v.styles.dim.Render("loading chain…")
	}
	ch := v.chain
	live := "paused"
	if ch.Live {
		live = "live"
	}
	active := ""
	if ch.Active {
		active = v.styles.level("RUNNING").Render(" • running")
	}
	line1 := v.styles.title.Render(fmt.Sprintf("#%d %s", ch.ChainID, ch.ChainName)) + active
	line2 := v.styles.dim.Render(fmt.Sprintf(
		"schedule %s · %s · max %d · timeout %dms · on_error %s · client %s",
		orDash(ch.RunAt), live, ch.MaxInstances, ch.Timeout, orDash(ch.OnError), orDash(ch.ClientName),
	))
	return line1 + "\n" + line2
}

func (v *detailView) tasksTable(width, height int) string {
	cols := []column{
		{title: "ID", min: 5},
		{title: "NAME", min: 10, flex: 2},
		{title: "KIND", min: 6},
		{title: "COMMAND", min: 12, flex: 3},
		{title: "RUN AS", min: 8, flex: 1},
		{title: "FLAGS", min: 6},
		{title: "TIMEOUT", min: 7},
	}
	rows := make([][]cell, len(v.tasks))
	for i, t := range v.tasks {
		rows[i] = []cell{
			plainCell(strconv.Itoa(t.TaskID)),
			plainCell(orDash(t.TaskName)),
			plainCell(orDash(t.Kind)),
			plainCell(oneLine(t.Command)),
			plainCell(orDash(t.RunAs)),
			plainCell(taskFlags(t)),
			plainCell(strconv.Itoa(t.Timeout)),
		}
	}
	sel := -1
	if v.focus == paneTasks {
		sel = v.taskCursor
	}
	return v.styles.renderTable(cols, rows, sel, width, height)
}

func (v *detailView) runsTable(width, height int) string {
	cols := []column{
		{title: "TXID", min: 12},
		{title: "STARTED", min: 19},
		{title: "MS", min: 8},
		{title: "STATUS", min: 8},
		{title: "TASKS", min: 6},
		{title: "FAILED", min: 6},
		{title: "CLIENT", min: 10, flex: 1},
	}
	rows := make([][]cell, len(v.runs))
	for i, r := range v.runs {
		lvl := v.styles.level(runStatusLevel(r.Status))
		rows[i] = []cell{
			plainCell(strconv.FormatInt(r.Txid, 10)),
			plainCell(orDash(r.StartedAt)),
			plainCell(strconv.FormatInt(r.DurationMS, 10)),
			{text: orDash(r.Status), style: &lvl},
			plainCell(strconv.Itoa(r.TotalTasks)),
			plainCell(strconv.Itoa(r.FailedTasks)),
			plainCell(orDash(r.ClientName)),
		}
	}
	sel := -1
	if v.focus == paneRuns {
		sel = v.runCursor
	}
	return v.styles.renderTable(cols, rows, sel, width, height)
}

func taskFlags(t client.ChainTask) string {
	var f []string
	if t.IgnoreError {
		f = append(f, "ign")
	}
	if t.Autonomous {
		f = append(f, "auto")
	}
	if strings.TrimSpace(t.ConnectString) != "" {
		f = append(f, "remote")
	}
	if len(f) == 0 {
		return "—"
	}
	return strings.Join(f, ",")
}

// runStatusLevel maps a run status to a level keyword for coloring.
func runStatusLevel(status string) string {
	switch strings.ToLower(status) {
	case "failed", "fail", "error":
		return "FAIL"
	case "running":
		return "RUNNING"
	default:
		return "OK"
	}
}

// oneLine collapses whitespace/newlines so multi-line commands fit one cell.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clamp(i, length int) int {
	if length == 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= length {
		return length - 1
	}
	return i
}
