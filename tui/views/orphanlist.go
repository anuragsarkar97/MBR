package views

// orphanlist.go renders the orphaned-resource review screen.
// It shows every node marked IsOrphan=true, the reason(s), and lets the user
// navigate with ↑/↓. Press esc to return to the main resource list.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anuragsarkar97/mbr/internal/aws/graph"
)

// OrphanListModel is the BubbleTea model for the orphan review screen.
type OrphanListModel struct {
	nodes  []*graph.Node
	cursor int
	offset int
	width  int
	height int
}

// NewOrphanListModel creates a model pre-populated with all orphaned nodes.
func NewOrphanListModel(g *graph.ResourceGraph, width, height int) OrphanListModel {
	return OrphanListModel{
		nodes:  g.Orphans(),
		width:  width,
		height: height,
	}
}

// Init satisfies tea.Model.
func (m OrphanListModel) Init() tea.Cmd { return nil }

// Update handles resize and keyboard navigation.
func (m OrphanListModel) Update(msg tea.Msg) (OrphanListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		h := m.listRows()
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}
		case "down", "j":
			if m.cursor < len(m.nodes)-1 {
				m.cursor++
				if m.cursor >= m.offset+h {
					m.offset = m.cursor - h + 1
				}
			}
		case "pgup", "ctrl+u":
			m.cursor -= h / 2
			if m.cursor < 0 {
				m.cursor = 0
			}
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		case "pgdown", "ctrl+d":
			m.cursor += h / 2
			if m.cursor >= len(m.nodes) {
				m.cursor = len(m.nodes) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			if m.cursor >= m.offset+h {
				m.offset = m.cursor - h + 1
			}
		}
	}
	return m, nil
}

// View renders the orphan list filling the content area.
func (m OrphanListModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	// ── Title row ────────────────────────────────────────────────────────────
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).Bold(true).
		Render("  ⚠  Orphaned Resources")
	countStr := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Render(fmt.Sprintf("%d found  ", len(m.nodes)))
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(countStr)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(title + strings.Repeat(" ", gap) + countStr + "\n")

	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151")).
		Render(strings.Repeat("─", m.width))
	b.WriteString(divider + "\n")

	// ── Resource rows ─────────────────────────────────────────────────────────
	h := m.listRows()

	if len(m.nodes) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Render("  No orphaned resources detected.")
		b.WriteString(empty + "\n")
		for i := 1; i < h; i++ {
			b.WriteString(strings.Repeat(" ", m.width) + "\n")
		}
	} else {
		offset := m.offset
		if offset > len(m.nodes)-h {
			offset = len(m.nodes) - h
		}
		if offset < 0 {
			offset = 0
		}
		end := offset + h
		if end > len(m.nodes) {
			end = len(m.nodes)
		}
		for i := offset; i < end; i++ {
			b.WriteString(m.renderRow(i) + "\n")
		}
		// Pad remaining rows so the footer stays anchored.
		for i := end - offset; i < h; i++ {
			b.WriteString(strings.Repeat(" ", m.width) + "\n")
		}
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	b.WriteString(divider + "\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

// renderRow renders one orphan row.
func (m OrphanListModel) renderRow(i int) string {
	node := m.nodes[i]
	isCursor := i == m.cursor

	// Cursor indicator.
	cursor := "  "
	if isCursor {
		cursor = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).Bold(true).
			Render("▶ ")
	}

	// Short resource ID (22 chars).
	rawID := node.Resource.RawID
	if rawID == "" {
		rawID = node.Resource.ID
	}
	rawID = truncate(rawID, 22)
	idCol := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Render(fmt.Sprintf("%-22s", rawID))

	// Display name (26 chars).
	name := node.Resource.DisplayName()
	if name == node.Resource.RawID {
		name = ""
	}
	name = truncate(name, 26)
	nameCol := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F3F4F6")).
		Render(fmt.Sprintf("%-26s", name))

	// Orphan reason (remainder of line).
	reason := ""
	if len(node.OrphanReasons) > 0 {
		reason = node.OrphanReasons[0]
	}
	reason = truncate(reason, m.width)
	reasonCol := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B")).
		Render(reason)

	row := cursor + idCol + "  " + nameCol + "  " + reasonCol

	// Pad to full width.
	w := lipgloss.Width(row)
	if w < m.width {
		row += strings.Repeat(" ", m.width-w)
	}

	if isCursor {
		return lipgloss.NewStyle().Background(lipgloss.Color("#374151")).Render(row)
	}
	return row
}

// renderFooter renders the key hint bar.
func (m OrphanListModel) renderFooter() string {
	key := func(k, desc string) string {
		kStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F4F6")).
			Background(lipgloss.Color("#374151")).
			PaddingLeft(1).PaddingRight(1).Bold(true)
		dStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
		return kStyle.Render(k) + " " + dStyle.Render(desc)
	}

	hints := []string{
		key("↑↓", "navigate"),
		key("esc", "back"),
	}

	total := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).
		Render(fmt.Sprintf("%d orphan(s)  ", len(m.nodes)))

	left := "  " + strings.Join(hints, "   ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(total)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + total
}

// listRows returns the number of rows available for resource items.
// Layout: title(1) + divider(1) + items(N) + divider(1) + footer(1) = N + 4
func (m OrphanListModel) listRows() int {
	h := m.height - 4
	if h < 3 {
		return 3
	}
	return h
}
