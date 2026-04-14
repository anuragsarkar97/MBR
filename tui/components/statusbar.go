package components

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel renders a one-line bar at the bottom of the terminal
// showing contextual key hints, the current AWS profile/region, and
// optional resource count.
type StatusBarModel struct {
	// Width is the terminal width; set via tea.WindowSizeMsg.
	Width int

	// Profile is the active AWS profile name shown on the right side.
	Profile string

	// Region is the last-scanned region (or "all" for multi-region scans).
	Region string

	// ResourceCount is the total number of resources loaded, 0 if not yet scanned.
	ResourceCount int

	// HelpText is the left-side key hint string produced by shortHelp().
	// Views update this to reflect their own context-specific bindings.
	HelpText string

	// style is the container background/text style.
	style lipgloss.Style

	// rightStyle is applied to the right-side info section.
	rightStyle lipgloss.Style
}

// NewStatusBar creates a StatusBarModel with sensible defaults.
func NewStatusBar() StatusBarModel {
	return StatusBarModel{
		style: lipgloss.NewStyle().
			Background(lipgloss.Color("#374151")).
			Foreground(lipgloss.Color("#F3F4F6")).
			PaddingLeft(1).PaddingRight(1),
		rightStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#06B6D4")).
			Background(lipgloss.Color("#374151")),
	}
}

// Init satisfies tea.Model; status bar needs no init command.
func (m StatusBarModel) Init() tea.Cmd { return nil }

// Update handles window resize to keep the bar full-width.
func (m StatusBarModel) Update(msg tea.Msg) (StatusBarModel, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width = ws.Width
	}
	return m, nil
}

// View renders the status bar. Left side: key hints. Right side: account info.
func (m StatusBarModel) View() string {
	if m.Width == 0 {
		return ""
	}

	right := ""
	if m.Profile != "" || m.Region != "" {
		right = fmt.Sprintf(" %s @ %s", m.Profile, m.Region)
	}
	if m.ResourceCount > 0 {
		right += fmt.Sprintf("  %d resources", m.ResourceCount)
	}

	// Pad between left and right to fill the terminal width.
	leftWidth := m.Width - lipgloss.Width(right) - 2 // 2 for padding
	if leftWidth < 0 {
		leftWidth = 0
	}

	left := fmt.Sprintf("%-*s", leftWidth, m.HelpText)

	return m.style.Render(left) + m.rightStyle.Render(right)
}
