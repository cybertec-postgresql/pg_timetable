package tui

import (
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleRunTasks() []client.RunTaskDetail {
	return []client.RunTaskDetail{
		{TaskID: 1, Kind: "SQL", Command: "SELECT 1", Returncode: 0, DurationMS: 100, StartedAt: "2026-06-23 03:00:00", Output: "1 row"},
		{TaskID: 2, Kind: "PROGRAM", Command: "false", Returncode: 1, DurationMS: 50, StartedAt: "2026-06-23 03:00:01", Output: "boom", Params: `["x"]`},
	}
}

func loadedRunDetail() *runDetailView {
	run := client.RunSummary{Txid: 1002, StartedAt: "2026-06-23 03:00:00", DurationMS: 800, Status: "failed", TotalTasks: 2, FailedTasks: 1}
	v := newRunDetailView(nil, newStyles(false), run)
	v.SetSize(120, 30)
	uv, _ := v.Update(runDetailLoadedMsg{tasks: sampleRunTasks()})
	return uv.(*runDetailView)
}

func TestRunDetailLoadsTasks(t *testing.T) {
	v := loadedRunDetail()
	if len(v.tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(v.tasks))
	}
	out := v.Body(120, 28)
	for _, want := range []string{"txid 1002", "Tasks", "Output", "SELECT 1", "false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("run-detail body missing %q", want)
		}
	}
}

func TestRunDetailCursorUpdatesViewport(t *testing.T) {
	v := loadedRunDetail()
	// Initially task 0 -> output "1 row".
	if !strings.Contains(v.vp.View(), "1 row") {
		t.Fatalf("viewport missing first task output:\n%s", v.vp.View())
	}
	// Move to task 1 -> output "boom" + params.
	v.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := v.vp.View()
	if !strings.Contains(got, "boom") {
		t.Fatalf("viewport did not switch to second task output:\n%s", got)
	}
	if v.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", v.cursor)
	}
}

func TestRunDetailTitle(t *testing.T) {
	v := loadedRunDetail()
	if got := v.Title(); got != "run 1002" {
		t.Fatalf("title = %q, want 'run 1002'", got)
	}
}

func TestRunDetailErrorSurfaces(t *testing.T) {
	run := client.RunSummary{Txid: 5}
	v := newRunDetailView(nil, newStyles(false), run)
	_, cmd := v.Update(runDetailLoadedMsg{err: errTest("nope")})
	if cmd == nil {
		t.Fatal("no command on error")
	}
	if _, ok := cmd().(errMsg); !ok {
		t.Fatal("expected errMsg")
	}
}
