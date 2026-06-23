package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds every global key binding. Views may interpret additional keys
// in their own Update, but these are always available and drive the help panel.
type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Back    key.Binding
	Refresh key.Binding
	Help    key.Binding
	Quit    key.Binding

	// Top-level view switches.
	Chains   key.Binding
	Sessions key.Binding
	Activity key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "backspace"),
			key.WithHelp("esc", "back"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Chains: key.NewBinding(
			key.WithKeys("1", "c"),
			key.WithHelp("1/c", "chains"),
		),
		Sessions: key.NewBinding(
			key.WithKeys("2", "s"),
			key.WithHelp("2/s", "sessions"),
		),
		Activity: key.NewBinding(
			key.WithKeys("3", "a"),
			key.WithHelp("3/a", "activity"),
		),
	}
}

// ShortHelp implements help.KeyMap: the compact one-line hint shown in the footer.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Back, k.Refresh, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap: the expanded grid shown in the help overlay.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Back},
		{k.Chains, k.Sessions, k.Activity},
		{k.Refresh, k.Help, k.Quit},
	}
}
