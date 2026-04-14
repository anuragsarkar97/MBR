// Package components contains reusable BubbleTea sub-models used by
// multiple views. Import this package from any view that needs a spinner,
// confirmation dialog, status bar, or table.
package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SpinnerModel wraps the charmbracelet spinner bubble with a status message.
// Embed it in any view that needs to show loading progress.
type SpinnerModel struct {
	spinner spinner.Model
	// Message is displayed beside the spinner, e.g. "Scanning us-east-1..."
	Message string
	// style is applied to the spinner itself.
	style lipgloss.Style
}

// NewSpinner creates a SpinnerModel with the Dot spinner style.
func NewSpinner() SpinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))
	return SpinnerModel{
		spinner: s,
		style:   s.Style,
	}
}

// Init returns the tick command that drives the spinner animation.
func (m SpinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update handles spinner tick messages. Returns the updated model and the
// next tick command.
func (m SpinnerModel) Update(msg tea.Msg) (SpinnerModel, tea.Cmd) {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// View renders the spinner with its current message.
func (m SpinnerModel) View() string {
	return fmt.Sprintf("%s %s", m.spinner.View(), m.Message)
}

// ProgressMsg is sent by the scan runner to update the spinner message.
type ProgressMsg struct {
	Region       string
	ResourceType string
	Done         int
	Total        int
}

// ScanCompleteMsg is sent when RunAll finishes. It carries the results
// and any non-fatal errors from individual collectors.
type ScanCompleteMsg struct {
	Err error
}
