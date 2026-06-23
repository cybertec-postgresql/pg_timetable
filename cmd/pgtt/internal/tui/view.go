package tui

import tea "github.com/charmbracelet/bubbletea"

// view is one screen in the TUI (chains list, chain detail, activity, …).
//
// Views are plain values, not full tea.Models: the root model owns the program
// loop, dispatches messages, and renders the chrome (header/footer). A view
// only renders its body and reacts to messages routed to it.
//
// Navigation is a stack: pushing a view (e.g. drilling into a chain) suspends
// the current one; popping (Esc) resumes it. The root model manages the stack.
type view interface {
	// Title is shown in the header breadcrumb.
	Title() string

	// Init returns an optional command to run when the view becomes active
	// (typically its first data fetch). May return nil.
	Init() tea.Cmd

	// Update handles a message and returns the (possibly updated) view plus an
	// optional command. A view returns itself; it never swaps identity. View
	// transitions are requested via navigation messages (see below), which the
	// root model acts on — the view itself does not mutate the stack.
	Update(msg tea.Msg) (view, tea.Cmd)

	// Body renders the view's content area for the given inner width/height
	// (the space between header and footer). It must not exceed those bounds.
	Body(width, height int) string

	// SetSize informs the view of the available body dimensions, letting it
	// resize embedded components (tables, viewports) ahead of rendering.
	SetSize(width, height int)
}

// --- Navigation messages -------------------------------------------------
//
// Views request stack changes by returning these as commands; the root model
// interprets them. This keeps stack ownership in one place (T1-1).

// pushViewMsg asks the model to push a new view onto the stack.
type pushViewMsg struct{ v view }

// popViewMsg asks the model to pop the current view (return to the previous).
type popViewMsg struct{}

// replaceRootMsg asks the model to clear the stack and make v the sole root
// view. Used by top-level view switches (chains/sessions/activity).
type replaceRootMsg struct{ v view }

// pushView returns a command that asks the model to push v onto the stack
// (used by views to drill into a detail screen).
func pushView(v view) tea.Cmd { return func() tea.Msg { return pushViewMsg{v} } }

// inputCapturer is an optional interface a view may implement to signal that it
// is currently capturing free text (e.g. a filter box). While capturing, the
// root model forwards all key messages to the view instead of interpreting its
// global bindings, so typing letters like 'q' or 'r' edits the text rather than
// quitting or refreshing.
type inputCapturer interface {
	CapturingInput() bool
}
