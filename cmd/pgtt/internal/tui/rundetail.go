package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// runDetailLoadedMsg carries the ShowRun result.
type runDetailLoadedMsg struct {
	tasks []client.RunTaskDetail
	err   error
}

// runDetailView shows the per-task breakdown of a single run (txid): a task
// table on top, and the selected task's params + output in a scrollable
// viewport below. Backed by client.ShowRun.
type runDetailView struct {
	client client.Client
	styles styles

	run   client.RunSummary
	tasks []client.RunTaskDetail

	cursor      int
	vp          viewport.Model
	vpReady     bool
	width, hght int
}

func newRunDetailView(c client.Client, s styles, run client.RunSummary) *runDetailView {
	return &runDetailView{client: c, styles: s, run: run}
}

func (v *runDetailView) Title() string {
	return "run " + strconv.FormatInt(v.run.Txid, 10)
}

func (v *runDetailView) Init() tea.Cmd { return v.fetch() }

func (v *runDetailView) SetSize(w, h int) {
	v.width, v.hght = w, h
	v.layoutViewport()
}

func (v *runDetailView) fetch() tea.Cmd {
	c, txid := v.client, v.run.Txid
	return func() tea.Msg {
		tasks, err := c.ShowRun(context.Background(), txid)
		return runDetailLoadedMsg{tasks: tasks, err: err}
	}
}

// outputHeight is the share of the body the output viewport occupies.
func (v *runDetailView) layoutViewport() {
	if v.width <= 0 || v.hght <= 0 {
		return
	}
	// header(2) + tasks title(1) + tasks table(taskRows+1) + output title(1).
	taskRows := len(v.tasks)
	if taskRows > 6 {
		taskRows = 6
	}
	top := 2 + 1 + (taskRows + 1) + 1
	h := v.hght - top
	if h < 3 {
		h = 3
	}
	if !v.vpReady {
		v.vp = viewport.New(v.width, h)
		v.vpReady = true
	} else {
		v.vp.Width = v.width
		v.vp.Height = h
	}
	v.refreshViewport()
}

func (v *runDetailView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		return v, v.fetch()

	case runDetailLoadedMsg:
		if msg.err != nil {
			return v, func() tea.Msg { return errMsg{msg.err} }
		}
		v.tasks = msg.tasks
		v.cursor = clamp(v.cursor, len(v.tasks))
		v.layoutViewport()
		return v, nil

	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

func (v *runDetailView) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	switch {
	case key.Matches(msg, defaultKeyMap().Up):
		v.cursor = clamp(v.cursor-1, len(v.tasks))
		v.refreshViewport()
		return v, nil
	case key.Matches(msg, defaultKeyMap().Down):
		v.cursor = clamp(v.cursor+1, len(v.tasks))
		v.refreshViewport()
		return v, nil
	}
	// Forward remaining keys (pgup/pgdn/space/etc.) to the viewport for scroll.
	if v.vpReady {
		var cmd tea.Cmd
		v.vp, cmd = v.vp.Update(msg)
		return v, cmd
	}
	return v, nil
}

// refreshViewport fills the viewport with the selected task's params + output.
func (v *runDetailView) refreshViewport() {
	if !v.vpReady {
		return
	}
	if v.cursor < 0 || v.cursor >= len(v.tasks) {
		v.vp.SetContent(v.styles.dim.Render("(no task selected)"))
		return
	}
	t := v.tasks[v.cursor]
	var b strings.Builder
	if strings.TrimSpace(t.Params) != "" {
		b.WriteString(v.styles.title.Render("params"))
		b.WriteByte('\n')
		b.WriteString(t.Params)
		b.WriteString("\n\n")
	}
	b.WriteString(v.styles.title.Render("output"))
	b.WriteByte('\n')
	if strings.TrimSpace(t.Output) == "" {
		b.WriteString(v.styles.dim.Render("(no output)"))
	} else {
		b.WriteString(t.Output)
	}
	v.vp.SetContent(b.String())
	v.vp.GotoTop()
}

func (v *runDetailView) Body(width, height int) string {
	if width != v.width || height != v.hght {
		v.width, v.hght = width, height
		v.layoutViewport()
	}
	var b strings.Builder
	b.WriteString(v.headerBlock())
	b.WriteByte('\n')

	b.WriteString(v.styles.title.Render("▌ Tasks"))
	b.WriteByte('\n')
	b.WriteString(v.tasksTable(width))
	b.WriteByte('\n')

	b.WriteString(v.styles.dim.Render("  Output (↑/↓ task · pgup/pgdn scroll)"))
	b.WriteByte('\n')
	if v.vpReady {
		b.WriteString(v.vp.View())
	}
	return b.String()
}

func (v *runDetailView) headerBlock() string {
	r := v.run
	lvl := v.styles.level(runStatusLevel(r.Status))
	line := fmt.Sprintf("txid %d · %s · %dms · ",
		r.Txid, orDash(r.StartedAt), r.DurationMS)
	return v.styles.title.Render(line) + lvl.Render(orDash(r.Status)) +
		v.styles.dim.Render(fmt.Sprintf(" · %d tasks, %d failed", r.TotalTasks, r.FailedTasks))
}

func (v *runDetailView) tasksTable(width int) string {
	cols := []column{
		{title: "TASK", min: 6},
		{title: "KIND", min: 6},
		{title: "COMMAND", min: 12, flex: 3},
		{title: "RC", min: 4},
		{title: "MS", min: 8},
		{title: "STARTED", min: 19},
	}
	rows := make([][]cell, len(v.tasks))
	for i, t := range v.tasks {
		rcLevel := "OK"
		if t.Returncode != 0 {
			rcLevel = "FAIL"
		}
		rc := v.styles.level(rcLevel)
		rows[i] = []cell{
			plainCell(strconv.FormatInt(t.TaskID, 10)),
			plainCell(orDash(t.Kind)),
			plainCell(oneLine(t.Command)),
			{text: strconv.Itoa(t.Returncode), style: &rc},
			plainCell(strconv.FormatInt(t.DurationMS, 10)),
			plainCell(orDash(t.StartedAt)),
		}
	}
	// Show at most 6 task rows in the table; the rest are reachable by scrolling
	// the cursor (the viewport shows the selected one's full output).
	h := len(v.tasks) + 1
	if h > 7 {
		h = 7
	}
	return v.styles.renderTable(cols, rows, v.cursor, width, h)
}
