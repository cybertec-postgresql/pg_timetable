package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/internal/client"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// sortMode controls chain row ordering.
type sortMode int

const (
	sortByID sortMode = iota
	sortByName
	sortByLastRun
)

func (s sortMode) String() string {
	switch s {
	case sortByName:
		return "name"
	case sortByLastRun:
		return "last run"
	default:
		return "id"
	}
}

// chainsListMsg carries the result of a ListChains fetch.
type chainsListMsg struct {
	items []client.ChainListItem
	err   error
}

// chainsView is the home screen: a refreshing, sortable, filterable table of
// chains backed entirely by client.ListChains (PAT-003).
type chainsView struct {
	client client.Client
	styles styles

	all      []client.ChainListItem // unfiltered, as fetched
	rows     []client.ChainListItem // filtered + sorted (what's displayed)
	selected int                    // index into rows
	sort     sortMode

	filtering bool   // is the filter input active?
	filter    string // current filter text

	width, height int

	keys chainsKeyMap
}

type chainsKeyMap struct {
	Sort   key.Binding
	Filter key.Binding
}

func defaultChainsKeyMap() chainsKeyMap {
	return chainsKeyMap{
		Sort:   key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "sort")),
		Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	}
}

func newChainsView(c client.Client, s styles) *chainsView {
	return &chainsView{client: c, styles: s, keys: defaultChainsKeyMap()}
}

func (v *chainsView) Title() string { return "Chains" }

func (v *chainsView) Init() tea.Cmd { return v.fetch() }

func (v *chainsView) SetSize(w, h int) { v.width, v.height = w, h }

// CapturingInput reports whether the filter box is active (see inputCapturer).
func (v *chainsView) CapturingInput() bool { return v.filtering }

// fetch returns a command that loads chains off the UI loop.
func (v *chainsView) fetch() tea.Cmd {
	c := v.client
	return func() tea.Msg {
		items, err := c.ListChains(context.Background())
		return chainsListMsg{items: items, err: err}
	}
}

func (v *chainsView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		return v, v.fetch()

	case chainsListMsg:
		if msg.err != nil {
			return v, func() tea.Msg { return errMsg{msg.err} }
		}
		v.all = msg.items
		v.reindex()
		return v, func() tea.Msg {
			return statusMsg(fmt.Sprintf("%d chains", len(v.all)))
		}

	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

func (v *chainsView) handleKey(msg tea.KeyMsg) (view, tea.Cmd) {
	// Filter-input mode captures most keys.
	if v.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			v.filtering = false
			v.filter = ""
			v.reindex()
		case tea.KeyEnter:
			v.filtering = false
		case tea.KeyBackspace, tea.KeyDelete:
			if v.filter != "" {
				v.filter = v.filter[:len(v.filter)-1]
				v.reindex()
			}
		case tea.KeyRunes, tea.KeySpace:
			v.filter += string(msg.Runes)
			if msg.Type == tea.KeySpace {
				v.filter += " "
			}
			v.reindex()
		}
		return v, nil
	}

	switch {
	case key.Matches(msg, defaultKeyMap().Up):
		v.move(-1)
	case key.Matches(msg, defaultKeyMap().Down):
		v.move(1)
	case key.Matches(msg, v.keys.Filter):
		v.filtering = true
	case key.Matches(msg, v.keys.Sort):
		v.sort = (v.sort + 1) % 3
		v.reindex()
		return v, func() tea.Msg { return statusMsg("sort: " + v.sort.String()) }
	case key.Matches(msg, defaultKeyMap().Enter):
		return v, v.openSelected()
	}
	return v, nil
}

func (v *chainsView) move(d int) {
	if len(v.rows) == 0 {
		return
	}
	v.selected += d
	if v.selected < 0 {
		v.selected = 0
	}
	if v.selected >= len(v.rows) {
		v.selected = len(v.rows) - 1
	}
}

// openSelected pushes the chain detail view for the highlighted row. Until T3
// lands, this pushes a placeholder titled with the chain name.
func (v *chainsView) openSelected() tea.Cmd {
	if v.selected < 0 || v.selected >= len(v.rows) {
		return nil
	}
	ch := v.rows[v.selected]
	detail := newPlaceholderView(ch.ChainName, v.client, v.styles)
	return pushView(detail)
}

// reindex re-applies the current filter + sort and clamps the selection,
// preserving the selected chain by id across refreshes when possible.
func (v *chainsView) reindex() {
	prevID := -1
	if v.selected >= 0 && v.selected < len(v.rows) {
		prevID = v.rows[v.selected].ChainID
	}

	filtered := v.all[:0:0]
	needle := strings.ToLower(strings.TrimSpace(v.filter))
	for _, it := range v.all {
		if needle == "" ||
			strings.Contains(strings.ToLower(it.ChainName), needle) ||
			strings.Contains(strings.ToLower(it.ClientName), needle) {
			filtered = append(filtered, it)
		}
	}
	sortChains(filtered, v.sort)
	v.rows = filtered

	// Restore selection to the same chain id if it still exists.
	v.selected = 0
	if prevID >= 0 {
		for i, it := range v.rows {
			if it.ChainID == prevID {
				v.selected = i
				break
			}
		}
	}
	if v.selected >= len(v.rows) {
		v.selected = max(0, len(v.rows)-1)
	}
}

func sortChains(items []client.ChainListItem, mode sortMode) {
	sort.SliceStable(items, func(i, j int) bool {
		switch mode {
		case sortByName:
			return strings.ToLower(items[i].ChainName) < strings.ToLower(items[j].ChainName)
		case sortByLastRun:
			// Lexicographic on the formatted timestamp; newest first. Empty
			// (never-run) sorts last.
			a, b := items[i].LastRun, items[j].LastRun
			if a == b {
				return items[i].ChainID < items[j].ChainID
			}
			if a == "" {
				return false
			}
			if b == "" {
				return true
			}
			return a > b
		default:
			return items[i].ChainID < items[j].ChainID
		}
	})
}

func (v *chainsView) Body(width, height int) string {
	cols := []column{
		{title: "ID", min: 5},
		{title: "NAME", min: 12, flex: 3},
		{title: "LIVE", min: 4},
		{title: "ACTIVE", min: 6},
		{title: "SCHEDULE", min: 10, flex: 2},
		{title: "LAST", min: 6},
		{title: "LAST RUN", min: 19},
		{title: "WORKER", min: 10, flex: 1},
	}

	rows := make([][]cell, len(v.rows))
	for i, it := range v.rows {
		lvl := v.styles.level(it.LastStatus)
		rows[i] = []cell{
			plainCell(strconv.Itoa(it.ChainID)),
			plainCell(it.ChainName),
			plainCell(boolMark(it.Live)),
			plainCell(boolMark(it.Active)),
			plainCell(it.RunAt),
			{text: orDash(it.LastStatus), style: &lvl},
			plainCell(orDash(it.LastRun)),
			plainCell(orDash(it.LastWorker)),
		}
	}

	tbl := v.styles.renderTable(cols, rows, v.selected, width, height-1)
	return v.filterLine(width) + "\n" + tbl
}

// filterLine renders the incremental filter prompt (active or hint).
func (v *chainsView) filterLine(_ int) string {
	switch {
	case v.filtering:
		return v.styles.title.Render("/") + v.filter + v.styles.dim.Render("▌")
	case v.filter != "":
		return v.styles.dim.Render(fmt.Sprintf("filter: %q (esc to clear)  sort: %s", v.filter, v.sort))
	default:
		return v.styles.dim.Render(fmt.Sprintf("/ filter   o sort (%s)   enter open", v.sort))
	}
}

func boolMark(b bool) string {
	if b {
		return "✓"
	}
	return "·"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
