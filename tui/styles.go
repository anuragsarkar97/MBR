// Package tui contains all BubbleTea models and Lipgloss styles for mbr.
// All style definitions live here so that visual tweaks require changes
// in exactly one file.
package tui

import "github.com/charmbracelet/lipgloss"

// Palette defines the colour scheme. These are the only colours used
// across all TUI components.
var Palette = struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Danger    lipgloss.Color
	Warning   lipgloss.Color
	Success   lipgloss.Color
	Muted     lipgloss.Color
	Text      lipgloss.Color
	Border    lipgloss.Color
}{
	Primary:   lipgloss.Color("#7C3AED"), // violet
	Secondary: lipgloss.Color("#3B82F6"), // blue
	Accent:    lipgloss.Color("#06B6D4"), // cyan
	Danger:    lipgloss.Color("#EF4444"), // red
	Warning:   lipgloss.Color("#F59E0B"), // amber
	Success:   lipgloss.Color("#10B981"), // green
	Muted:     lipgloss.Color("#6B7280"), // grey
	Text:      lipgloss.Color("#F3F4F6"), // near-white
	Border:    lipgloss.Color("#374151"), // dark grey
}

// Styles is the package-level style registry. All TUI components
// reference these instead of defining their own.
var Styles = struct {
	// Title is used for screen headings.
	Title lipgloss.Style

	// Subtitle is used for secondary headings and section labels.
	Subtitle lipgloss.Style

	// Selected is the style for the focused list item.
	Selected lipgloss.Style

	// Dim is used for de-emphasised text (e.g. secondary metadata).
	Dim lipgloss.Style

	// Orphan highlights orphaned resource labels.
	Orphan lipgloss.Style

	// Danger highlights high-risk resources.
	Danger lipgloss.Style

	// Success highlights healthy or deletable-with-confidence resources.
	Success lipgloss.Style

	// Badge is a small rounded label (e.g. resource type tag).
	Badge lipgloss.Style

	// StatusBar is the bottom status bar container.
	StatusBar lipgloss.Style

	// KeyHint is a key binding hint in the status bar.
	KeyHint lipgloss.Style

	// Box is the standard bordered panel used by most views.
	Box lipgloss.Style

	// Cost is the style for cost amounts.
	Cost lipgloss.Style
}{
	Title: lipgloss.NewStyle().
		Foreground(Palette.Primary).
		Bold(true).
		PaddingBottom(1),

	Subtitle: lipgloss.NewStyle().
		Foreground(Palette.Accent).
		Bold(false),

	Selected: lipgloss.NewStyle().
		Foreground(Palette.Text).
		Background(Palette.Primary).
		Bold(true),

	Dim: lipgloss.NewStyle().
		Foreground(Palette.Muted),

	Orphan: lipgloss.NewStyle().
		Foreground(Palette.Warning).
		Bold(true),

	Danger: lipgloss.NewStyle().
		Foreground(Palette.Danger).
		Bold(true),

	Success: lipgloss.NewStyle().
		Foreground(Palette.Success),

	Badge: lipgloss.NewStyle().
		Foreground(Palette.Text).
		Background(Palette.Secondary).
		PaddingLeft(1).PaddingRight(1),

	StatusBar: lipgloss.NewStyle().
		Foreground(Palette.Text).
		Background(Palette.Border).
		PaddingLeft(1).PaddingRight(1),

	KeyHint: lipgloss.NewStyle().
		Foreground(Palette.Accent).
		Bold(true),

	Box: lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Palette.Border).
		Padding(0, 1),

	Cost: lipgloss.NewStyle().
		Foreground(Palette.Warning).
		Bold(true),
}

// ResourceTypeColor returns a Lipgloss color for each resource type,
// used for colour-coding nodes in the graph view and list badges.
func ResourceTypeColor(rt string) lipgloss.Color {
	switch rt {
	case "ec2:instance":
		return lipgloss.Color("#3B82F6") // blue
	case "ec2:ebs-volume":
		return lipgloss.Color("#8B5CF6") // purple
	case "ec2:vpc":
		return lipgloss.Color("#10B981") // green
	case "ec2:subnet":
		return lipgloss.Color("#06B6D4") // cyan
	case "ec2:sg":
		return lipgloss.Color("#F59E0B") // amber
	case "ec2:igw":
		return lipgloss.Color("#EC4899") // pink
	case "elb:v2", "elb:classic":
		return lipgloss.Color("#14B8A6") // teal
	case "asg:group":
		return lipgloss.Color("#6366F1") // indigo
	case "rds:instance", "rds:cluster":
		return lipgloss.Color("#F97316") // orange
	case "dynamodb:table":
		return lipgloss.Color("#EAB308") // yellow
	case "elasticache:cluster":
		return lipgloss.Color("#84CC16") // lime
	case "lambda:function":
		return lipgloss.Color("#A78BFA") // violet
	default:
		return Palette.Muted
	}
}
