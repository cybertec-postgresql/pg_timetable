package tui

import (
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

func chainItem(id int, name, client_, status, lastRun string, live, active bool) client.ChainListItem {
	var it client.ChainListItem
	it.ChainID = id
	it.ChainName = name
	it.ClientName = client_
	it.LastStatus = status
	it.LastRun = lastRun
	it.Live = live
	it.Active = active
	return it
}

func sampleChains() []client.ChainListItem {
	return []client.ChainListItem{
		chainItem(3, "backup", "w1", "OK", "2026-06-23 10:00:00", true, false),
		chainItem(1, "etl", "w2", "FAIL", "2026-06-23 12:00:00", true, true),
		chainItem(2, "cleanup", "w1", "", "", false, false),
	}
}

func loadedChainsView() *chainsView {
	v := newChainsView(nil, newStyles(false))
	v.SetSize(120, 20)
	updated, _ := v.Update(chainsListMsg{items: sampleChains()})
	return updated.(*chainsView)
}

func TestChainsSortByID(t *testing.T) {
	v := loadedChainsView()
	v.sort = sortByID
	v.reindex()
	gotIDs := []int{v.rows[0].ChainID, v.rows[1].ChainID, v.rows[2].ChainID}
	want := []int{1, 2, 3}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("sortByID order = %v, want %v", gotIDs, want)
		}
	}
}

func TestChainsSortByName(t *testing.T) {
	v := loadedChainsView()
	v.sort = sortByName
	v.reindex()
	got := []string{v.rows[0].ChainName, v.rows[1].ChainName, v.rows[2].ChainName}
	want := []string{"backup", "cleanup", "etl"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortByName order = %v, want %v", got, want)
		}
	}
}

func TestChainsSortByLastRunNewestFirstEmptyLast(t *testing.T) {
	v := loadedChainsView()
	v.sort = sortByLastRun
	v.reindex()
	// etl (12:00) > backup (10:00) > cleanup (empty).
	got := []string{v.rows[0].ChainName, v.rows[1].ChainName, v.rows[2].ChainName}
	want := []string{"etl", "backup", "cleanup"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortByLastRun order = %v, want %v", got, want)
		}
	}
}

func TestChainsFilterByNameAndClient(t *testing.T) {
	v := loadedChainsView()

	v.filter = "etl"
	v.reindex()
	if len(v.rows) != 1 || v.rows[0].ChainName != "etl" {
		t.Fatalf("name filter rows = %d (%v), want 1 etl", len(v.rows), names(v.rows))
	}

	v.filter = "w1" // client filter
	v.reindex()
	if len(v.rows) != 2 {
		t.Fatalf("client filter rows = %d (%v), want 2", len(v.rows), names(v.rows))
	}

	v.filter = "zzz"
	v.reindex()
	if len(v.rows) != 0 {
		t.Fatalf("no-match filter rows = %d, want 0", len(v.rows))
	}
}

func TestChainsSelectionPreservedAcrossRefresh(t *testing.T) {
	v := loadedChainsView()
	v.sort = sortByID
	v.reindex()
	// Select chain id 2 (index 1 under id sort).
	v.selected = 1
	selectedID := v.rows[v.selected].ChainID

	// Refresh with reordered data (different slice order, same ids).
	reordered := []client.ChainListItem{
		chainItem(2, "cleanup", "w1", "", "", false, false),
		chainItem(1, "etl", "w2", "FAIL", "2026-06-23 12:00:00", true, true),
		chainItem(3, "backup", "w1", "OK", "2026-06-23 10:00:00", true, false),
	}
	updated, _ := v.Update(chainsListMsg{items: reordered})
	v = updated.(*chainsView)

	if v.rows[v.selected].ChainID != selectedID {
		t.Fatalf("selection moved to id %d, want %d preserved", v.rows[v.selected].ChainID, selectedID)
	}
}

func TestChainsMoveClamps(t *testing.T) {
	v := loadedChainsView()
	v.selected = 0
	v.move(-1)
	if v.selected != 0 {
		t.Fatalf("move up at top = %d, want 0", v.selected)
	}
	v.selected = len(v.rows) - 1
	v.move(1)
	if v.selected != len(v.rows)-1 {
		t.Fatalf("move down at bottom = %d, want %d", v.selected, len(v.rows)-1)
	}
}

func TestChainsFilterCapturesInput(t *testing.T) {
	v := loadedChainsView()
	if v.CapturingInput() {
		t.Fatal("should not capture before /")
	}
	// Start filtering.
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !v.CapturingInput() {
		t.Fatal("/ did not enter filter mode")
	}
	// Type a letter that is also a global key ('q').
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if v.filter != "q" {
		t.Fatalf("filter = %q, want q (letter not swallowed as quit)", v.filter)
	}
	// Esc clears.
	v.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if v.CapturingInput() || v.filter != "" {
		t.Fatalf("esc did not clear filter: capturing=%v filter=%q", v.CapturingInput(), v.filter)
	}
}

func TestChainsBodyRendersRows(t *testing.T) {
	v := loadedChainsView()
	out := v.Body(120, 18)
	for _, want := range []string{"backup", "etl", "cleanup", "ID", "NAME", "LAST"} {
		if !strings.Contains(out, want) {
			t.Fatalf("body missing %q\n%s", want, out)
		}
	}
}

func TestChainsListMsgError(t *testing.T) {
	v := newChainsView(nil, newStyles(false))
	_, cmd := v.Update(chainsListMsg{err: errTest("connection lost")})
	if cmd == nil {
		t.Fatal("error result returned no command")
	}
	msg := cmd()
	em, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !strings.Contains(em.err.Error(), "connection lost") {
		t.Fatalf("errMsg = %v", em.err)
	}
}

func names(items []client.ChainListItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ChainName
	}
	return out
}
