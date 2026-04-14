package tui

import "github.com/charmbracelet/bubbles/key"

// GlobalKeys defines key bindings that are active in every screen.
// Individual views may override these with context-specific bindings.
var GlobalKeys = struct {
	Quit   key.Binding
	Help   key.Binding
	Back   key.Binding
	Scan   key.Binding
}{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c", "q"),
		key.WithHelp("q", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Scan: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "re-scan"),
	),
}

// ListKeys defines key bindings active in list-style views (resource list,
// orphan list, region selector).
var ListKeys = struct {
	Up       key.Binding
	Down     key.Binding
	Select   key.Binding
	Filter   key.Binding
	ShowCost key.Binding
	Orphans  key.Binding
	Graph    key.Binding
	Danger   key.Binding
	Delete   key.Binding
}{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select / view cost"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	ShowCost: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "cost"),
	),
	Orphans: key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "orphans"),
	),
	Graph: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "graph"),
	),
	Danger: key.NewBinding(
		key.WithKeys("!"),
		key.WithHelp("!", "danger"),
	),
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete orphan"),
	),
}

// shortHelp returns a short string of the most important bindings for the
// status bar. Call this from each view's View() method.
func shortHelp(bindings ...key.Binding) string {
	var parts []string
	for _, b := range bindings {
		h := b.Help()
		if h.Key != "" && h.Desc != "" {
			parts = append(parts, Styles.KeyHint.Render(h.Key)+" "+Styles.Dim.Render(h.Desc))
		}
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += Styles.Dim.Render("  ")
		}
		result += p
	}
	return result
}
