// Package views contains one BubbleTea sub-model per screen in mbr.
package views

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AllAWSRegions is the complete list of generally-available AWS commercial
// regions as of 2025. Hardcoded so the TUI works instantly with no API call.
var AllAWSRegions = func() []string {
	regions := []string{
		"af-south-1",     // Africa — Cape Town
		"ap-east-1",      // Asia Pacific — Hong Kong
		"ap-northeast-1", // Asia Pacific — Tokyo
		"ap-northeast-2", // Asia Pacific — Seoul
		"ap-northeast-3", // Asia Pacific — Osaka
		"ap-south-1",     // Asia Pacific — Mumbai
		"ap-south-2",     // Asia Pacific — Hyderabad
		"ap-southeast-1", // Asia Pacific — Singapore
		"ap-southeast-2", // Asia Pacific — Sydney
		"ap-southeast-3", // Asia Pacific — Jakarta
		"ap-southeast-4", // Asia Pacific — Melbourne
		"ap-southeast-5", // Asia Pacific — Malaysia
		"ca-central-1",   // Canada — Central
		"ca-west-1",      // Canada — Calgary
		"eu-central-1",   // Europe — Frankfurt
		"eu-central-2",   // Europe — Zurich
		"eu-north-1",     // Europe — Stockholm
		"eu-south-1",     // Europe — Milan
		"eu-south-2",     // Europe — Spain
		"eu-west-1",      // Europe — Ireland
		"eu-west-2",      // Europe — London
		"eu-west-3",      // Europe — Paris
		"il-central-1",   // Israel — Tel Aviv
		"me-central-1",   // Middle East — UAE
		"me-south-1",     // Middle East — Bahrain
		"mx-central-1",   // Mexico — Mexico City
		"sa-east-1",      // South America — São Paulo
		"us-east-1",      // US East — N. Virginia
		"us-east-2",      // US East — Ohio
		"us-west-1",      // US West — N. California
		"us-west-2",      // US West — Oregon
	}
	sort.Strings(regions)
	return regions
}()

// RegionSelectedMsg is sent to the root AppModel when the user confirms
// their region selection.
type RegionSelectedMsg struct {
	Regions []string
}

// RegionModel is a fully custom-rendered region picker.
// It does not use bubbles/list so we have complete control over key handling
// and visual layout — in particular guaranteeing that space toggles selection.
type RegionModel struct {
	// all is the master list (unfiltered).
	all []string

	// visible is the filtered subset shown in the list.
	visible []string

	// selected tracks which regions are checked.
	selected map[string]bool

	// cursor is the index into visible of the highlighted row.
	cursor int

	// offset is the first visible row (for scrolling).
	offset int

	// filterMode is true when the user is typing a search string.
	filterMode bool

	// filterText is the current search query.
	filterText string

	// allToggle tracks whether "select all" is active.
	allToggle bool

	width  int
	height int
}

// NewRegionModel returns a RegionModel pre-populated with all AWS regions.
func NewRegionModel() RegionModel {
	m := RegionModel{
		all:      AllAWSRegions,
		visible:  AllAWSRegions,
		selected: make(map[string]bool),
	}
	return m
}

// Regions returns the master region list (used by app.go for nil checks).
func (m RegionModel) Regions() []string { return m.all }

// WithRegions replaces the region list (used if dynamic fetch succeeds).
func (m RegionModel) WithRegions(regions []string) RegionModel {
	m.all = regions
	m.visible = regions
	m.selected = make(map[string]bool)
	m.cursor = 0
	m.offset = 0
	return m
}

// Init satisfies tea.Model.
func (m RegionModel) Init() tea.Cmd { return nil }

// Update handles all keyboard interaction for the region picker.
// Every key we care about is handled here; nothing is delegated to a sub-component.
func (m RegionModel) Update(msg tea.Msg) (RegionModel, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.filterMode {
			return m.updateFilterMode(msg)
		}
		return m.updateNormalMode(msg)
	}
	return m, nil
}

// updateNormalMode handles keys when not in filter/search mode.
func (m RegionModel) updateNormalMode(msg tea.KeyMsg) (RegionModel, tea.Cmd) {
	listH := m.listRows()

	switch msg.String() {

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		}

	case "down", "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
			if m.cursor >= m.offset+listH {
				m.offset = m.cursor - listH + 1
			}
		}

	case " ": // space — toggle current region
		if m.cursor < len(m.visible) {
			name := m.visible[m.cursor]
			m.selected[name] = !m.selected[name]
			// Move cursor down after toggling for faster multi-select.
			if m.cursor < len(m.visible)-1 {
				m.cursor++
				if m.cursor >= m.offset+listH {
					m.offset = m.cursor - listH + 1
				}
			}
		}

	case "a": // select all / deselect all
		m.allToggle = !m.allToggle
		if m.allToggle {
			for _, r := range m.all {
				m.selected[r] = true
			}
		} else {
			m.selected = make(map[string]bool)
		}

	case "/": // enter filter mode
		m.filterMode = true
		m.filterText = ""

	case "enter": // confirm and start scan
		chosen := m.chosenRegions()
		if len(chosen) > 0 {
			return m, func() tea.Msg { return RegionSelectedMsg{Regions: chosen} }
		}

	case "g", "home": // jump to top
		m.cursor = 0
		m.offset = 0

	case "G", "end": // jump to bottom
		m.cursor = len(m.visible) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.cursor >= listH {
			m.offset = m.cursor - listH + 1
		}
	}

	return m, nil
}

// updateFilterMode handles keys while the user is typing a search query.
func (m RegionModel) updateFilterMode(msg tea.KeyMsg) (RegionModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Exit filter mode, keep current results.
		m.filterMode = false

	case "esc":
		// Exit filter mode AND clear the filter so the full list is restored.
		m.filterMode = false
		m.filterText = ""
		m.applyFilter()

	case "backspace", "ctrl+h":
		if len(m.filterText) > 0 {
			runes := []rune(m.filterText)
			m.filterText = string(runes[:len(runes)-1])
		}
		m.applyFilter()

	default:
		// Append printable characters to the filter.
		if len(msg.String()) == 1 {
			m.filterText += msg.String()
			m.applyFilter()
		}
	}
	return m, nil
}

// applyFilter rebuilds visible from all, filtered by filterText.
func (m *RegionModel) applyFilter() {
	q := strings.ToLower(m.filterText)
	if q == "" {
		m.visible = m.all
	} else {
		// Allocate a fresh slice — never reuse m.visible[:0] as it shares the
		// backing array with m.all and would silently corrupt it on append.
		filtered := make([]string, 0, len(m.all))
		for _, r := range m.all {
			if strings.Contains(r, q) {
				filtered = append(filtered, r)
			}
		}
		m.visible = filtered
	}
	m.cursor = 0
	m.offset = 0
}

// View renders the full region picker filling the content area.
func (m RegionModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder
	listH := m.listRows()

	// ── Title row ────────────────────────────────────────────────────────
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A78BFA")).
		Bold(true).
		Render("  Select regions to scan")

	selCount := m.chosenRegions()
	countBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981")).
		Bold(true).
		Render(fmt.Sprintf("%d selected", len(selCount)))

	totalBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Render(fmt.Sprintf("/ %d total", len(m.visible)))

	titleGap := m.width - lipgloss.Width(title) - lipgloss.Width(countBadge) - lipgloss.Width(totalBadge) - 4
	if titleGap < 1 {
		titleGap = 1
	}
	b.WriteString(title + strings.Repeat(" ", titleGap) + countBadge + "  " + totalBadge + "\n")

	// ── Filter bar ───────────────────────────────────────────────────────
	filterBar := m.renderFilterBar()
	b.WriteString(filterBar + "\n")

	// ── Divider ──────────────────────────────────────────────────────────
	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151")).
		Render(strings.Repeat("─", m.width))
	b.WriteString(divider + "\n")

	// ── Region rows ──────────────────────────────────────────────────────
	end := m.offset + listH
	if end > len(m.visible) {
		end = len(m.visible)
	}

	for i := m.offset; i < end; i++ {
		b.WriteString(m.renderRow(i) + "\n")
	}

	// Pad empty rows so the footer stays in place.
	for i := end - m.offset; i < listH; i++ {
		b.WriteString(strings.Repeat(" ", m.width) + "\n")
	}

	// ── Divider ──────────────────────────────────────────────────────────
	b.WriteString(divider + "\n")

	// ── Footer / key hints ───────────────────────────────────────────────
	b.WriteString(m.renderFooter())

	return b.String()
}

// renderRow renders one region row at index i.
func (m RegionModel) renderRow(i int) string {
	name := m.visible[i]
	isCursor := i == m.cursor
	isSelected := m.selected[name]

	// Cursor arrow
	cursor := "  "
	if isCursor {
		cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true).Render("▶ ")
	}

	// Checkbox
	checkbox := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).Render("○ ")
	if isSelected {
		checkbox = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true).Render("● ")
	}

	// Region name
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	if isCursor && isSelected {
		nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true)
	} else if isCursor {
		nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")).Bold(true)
	} else if isSelected {
		nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	}

	row := cursor + checkbox + nameStyle.Render(name)

	// Pad to full width so background fills the line.
	rowWidth := lipgloss.Width(row)
	if rowWidth < m.width {
		row += strings.Repeat(" ", m.width-rowWidth)
	}

	if isCursor {
		return lipgloss.NewStyle().Background(lipgloss.Color("#1F2937")).Render(row)
	}
	return row
}

// renderFilterBar renders the search input line.
func (m RegionModel) renderFilterBar() string {
	prefix := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("  / filter: ")

	if m.filterMode {
		text := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")).Render(m.filterText)
		cursor := lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true).Render("█")
		bar := prefix + text + cursor
		padW := m.width - lipgloss.Width(bar)
		if padW > 0 {
			bar += strings.Repeat(" ", padW)
		}
		return lipgloss.NewStyle().Background(lipgloss.Color("#111827")).Render(bar)
	}

	if m.filterText != "" {
		text := lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Render(m.filterText)
		hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).Render("  (/ to edit, esc to clear)")
		return prefix + text + hint
	}

	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).Render("type / to filter regions")
	return prefix + hint
}

// renderFooter renders the key hint bar at the bottom of the picker.
func (m RegionModel) renderFooter() string {
	key := func(k, desc string) string {
		kStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F4F6")).
			Background(lipgloss.Color("#374151")).
			PaddingLeft(1).PaddingRight(1).
			Bold(true)
		dStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
		return kStyle.Render(k) + " " + dStyle.Render(desc)
	}

	hints := []string{
		key("↑↓", "move"),
		key("space", "toggle"),
		key("a", "all/none"),
		key("/", "filter"),
	}

	n := len(m.chosenRegions())
	var enterHint string
	if n > 0 {
		enterHint = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F4F6")).
			Background(lipgloss.Color("#7C3AED")).
			Bold(true).
			PaddingLeft(2).PaddingRight(2).
			Render(fmt.Sprintf("enter  scan %d region(s) →", n))
	} else {
		enterHint = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Render("select regions then press enter")
	}

	left := "  " + strings.Join(hints, "   ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(enterHint) - 2
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + enterHint
}

// listRows returns the number of rows available for region items.
// Layout: title(1) + filterBar(1) + divider(1) + items(N) + divider(1) + footer(1) = N + 5
func (m RegionModel) listRows() int {
	h := m.height - 5
	if h < 3 {
		return 3
	}
	return h
}

// chosenRegions returns selected region names in sorted order.
func (m RegionModel) chosenRegions() []string {
	var out []string
	for r, sel := range m.selected {
		if sel {
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}
