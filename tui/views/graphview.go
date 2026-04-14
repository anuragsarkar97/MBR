package views

// graphview.go renders the dependency graph for a single selected resource.
//
// Layout:
//
//	  ⬡  Dependency Graph — i-0abc123  (ec2:instance)
//	  ──────────────────────────────────────────────────
//
//	  i-0abc123  my-instance  ec2:instance  us-east-1
//	    name: My Web Server
//
//	  DEPENDS ON  (2)
//	    →  secured-by   sg-0xyz    my-sg
//	    →  contains     subnet-0d  10.0.0.0/24
//
//	  REFERENCED BY  (1)
//	    ←  attached-to  vol-0abc   my-vol
//	  ──────────────────────────────────────────────────
//	  ↑↓ scroll   esc back

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/angsak/mbr/internal/aws/graph"
)

// graphLine is one pre-rendered line in the scrollable buffer.
type graphLine struct{ text string }

// GraphViewModel is the BubbleTea model for the dependency-graph detail screen.
type GraphViewModel struct {
	g      *graph.ResourceGraph
	node   *graph.Node // the focal resource (nil if none selected)
	lines  []graphLine
	offset int
	width  int
	height int
}

// NewGraphViewModel creates a model focused on the resource identified by nodeID.
// If nodeID is empty or not found, the view shows a "no selection" prompt.
func NewGraphViewModel(g *graph.ResourceGraph, nodeID string, width, height int) GraphViewModel {
	m := GraphViewModel{g: g, width: width, height: height}
	if nodeID != "" {
		if n, ok := g.Node(nodeID); ok {
			m.node = n
		}
	}
	m.lines = m.buildLines()
	return m
}

// Init satisfies tea.Model.
func (m GraphViewModel) Init() tea.Cmd { return nil }

// Update handles resize and scrolling.
func (m GraphViewModel) Update(msg tea.Msg) (GraphViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.lines = m.buildLines()

	case tea.KeyMsg:
		h := m.listRows()
		switch msg.String() {
		case "up", "k":
			if m.offset > 0 {
				m.offset--
			}
		case "down", "j":
			if m.offset+h < len(m.lines) {
				m.offset++
			}
		case "pgup", "ctrl+u":
			m.offset -= h / 2
			if m.offset < 0 {
				m.offset = 0
			}
		case "pgdown", "ctrl+d":
			m.offset += h / 2
			max := len(m.lines) - h
			if m.offset > max {
				m.offset = max
			}
			if m.offset < 0 {
				m.offset = 0
			}
		}
	}
	return m, nil
}

// View renders the graph detail screen.
func (m GraphViewModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	// ── Title row ─────────────────────────────────────────────────────────────
	var titleText string
	if m.node != nil {
		rawID := m.node.Resource.RawID
		if rawID == "" {
			rawID = m.node.Resource.ID
		}
		titleText = fmt.Sprintf("  ⬡  Dependency Graph — %s", truncate(rawID, 30))
	} else {
		titleText = "  ⬡  Dependency Graph"
	}

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3B82F6")).Bold(true).
		Render(titleText)

	typeStr := ""
	if m.node != nil {
		typeStr = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Render(fmt.Sprintf("(%s)  ", string(m.node.Resource.Type)))
	}

	gap := m.width - lipgloss.Width(title) - lipgloss.Width(typeStr)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(title + strings.Repeat(" ", gap) + typeStr + "\n")

	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151")).
		Render(strings.Repeat("─", m.width))
	b.WriteString(divider + "\n")

	// ── Scrollable content ────────────────────────────────────────────────────
	h := m.listRows()
	offset := m.offset
	if offset > len(m.lines)-h {
		offset = len(m.lines) - h
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + h
	if end > len(m.lines) {
		end = len(m.lines)
	}

	for i := offset; i < end; i++ {
		b.WriteString(m.lines[i].text + "\n")
	}
	for i := end - offset; i < h; i++ {
		b.WriteString(strings.Repeat(" ", m.width) + "\n")
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	b.WriteString(divider + "\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

// buildLines constructs the full scrollable line buffer for the focused node.
func (m GraphViewModel) buildLines() []graphLine {
	var lines []graphLine

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6"))
	bright := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")).Bold(true)
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Bold(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true)

	if m.node == nil {
		lines = append(lines, graphLine{text: ""})
		lines = append(lines, graphLine{
			text: "  " + dim.Render("Navigate to a resource on the list and press") +
				" " + bright.Render("g") +
				" " + dim.Render("to view its dependencies."),
		})
		return lines
	}

	res := m.node.Resource
	rawID := res.RawID
	if rawID == "" {
		rawID = res.ID
	}

	// ── Focal resource info ───────────────────────────────────────────────────
	lines = append(lines, graphLine{text: ""})

	orphanTag := ""
	if m.node.IsOrphan {
		orphanTag = "  " + amber.Render("⚠ orphan")
	}
	lines = append(lines, graphLine{
		text: "  " + bright.Render(truncate(rawID, 26)) +
			"  " + dim.Render(string(res.Type)) +
			"  " + dim.Render(res.Region) +
			orphanTag,
	})

	displayName := res.DisplayName()
	if displayName != rawID && displayName != "" {
		lines = append(lines, graphLine{
			text: "  " + dim.Render("name: "+displayName),
		})
	}
	if len(m.node.OrphanReasons) > 0 {
		lines = append(lines, graphLine{
			text: "  " + amber.Render("reason: "+m.node.OrphanReasons[0]),
		})
	}

	// ── Outbound edges: this node depends on others ───────────────────────────
	outEdges := m.g.EdgesFrom(res.ID)
	if len(outEdges) > 0 {
		lines = append(lines, graphLine{text: ""})
		lines = append(lines, graphLine{
			text: "  " + purple.Render(fmt.Sprintf("DEPENDS ON  (%d)", len(outEdges))),
		})
		for _, e := range outEdges {
			if target, ok := m.g.Node(e.ToID); ok {
				lines = append(lines, graphLine{
					text: m.renderEdgeLine("→", string(e.Relationship), target, accent, dim),
				})
			}
		}
	}

	// ── Inbound edges: other nodes reference this one ─────────────────────────
	dependents := m.g.Dependents(res.ID)
	if len(dependents) > 0 {
		lines = append(lines, graphLine{text: ""})
		lines = append(lines, graphLine{
			text: "  " + green.Render(fmt.Sprintf("REFERENCED BY  (%d)", len(dependents))),
		})
		for _, dep := range dependents {
			// Find the relationship label from the dependent's outbound edges.
			rel := ""
			for _, e := range m.g.EdgesFrom(dep.Resource.ID) {
				if e.ToID == res.ID {
					rel = string(e.Relationship)
					break
				}
			}
			lines = append(lines, graphLine{
				text: m.renderEdgeLine("←", rel, dep, green, dim),
			})
		}
	}

	if len(outEdges) == 0 && len(dependents) == 0 {
		lines = append(lines, graphLine{text: ""})
		lines = append(lines, graphLine{
			text: "  " + dim.Render("No connections — this resource has no graph edges."),
		})
	}

	return lines
}

// renderEdgeLine formats one "→ relation  rawID  name" row.
func (m GraphViewModel) renderEdgeLine(
	dir, rel string,
	node *graph.Node,
	relStyle, dimStyle lipgloss.Style,
) string {
	rawID := node.Resource.RawID
	if rawID == "" {
		rawID = node.Resource.ID
	}
	rawID = truncate(rawID, 22)
	idCol := dimStyle.Render(fmt.Sprintf("%-22s", rawID))

	name := node.Resource.DisplayName()
	if name == node.Resource.RawID {
		name = ""
	}
	name = truncate(name, 26)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6"))
	nameCol := nameStyle.Render(fmt.Sprintf("%-26s", name))

	orphanTag := ""
	if node.IsOrphan {
		orphanTag = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render("⚠")
	}

	return "    " + dir + "  " +
		relStyle.Render(fmt.Sprintf("%-14s", rel)) +
		"  " + idCol +
		"  " + nameCol +
		orphanTag
}

// renderFooter renders the key hint bar.
func (m GraphViewModel) renderFooter() string {
	key := func(k, desc string) string {
		kStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F4F6")).
			Background(lipgloss.Color("#374151")).
			PaddingLeft(1).PaddingRight(1).Bold(true)
		dStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
		return kStyle.Render(k) + " " + dStyle.Render(desc)
	}

	hints := []string{
		key("↑↓", "scroll"),
		key("esc", "back"),
	}

	edgeCount := ""
	if m.node != nil {
		out := len(m.g.EdgesFrom(m.node.Resource.ID))
		in := len(m.g.Dependents(m.node.Resource.ID))
		edgeCount = lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).
			Render(fmt.Sprintf("%d out  %d in  ", out, in))
	}

	left := "  " + strings.Join(hints, "   ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(edgeCount)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + edgeCount
}

// listRows returns the number of rows available for scrollable content.
// Layout: title(1) + divider(1) + content(N) + divider(1) + footer(1) = N + 4
func (m GraphViewModel) listRows() int {
	h := m.height - 4
	if h < 3 {
		return 3
	}
	return h
}
