package views

// resourcelist.go renders the main resource browser.
//
// Resources are grouped into five categories rendered as distinct cards:
//   Networking  — VPC, Subnet, Security Group, IGW
//   Compute     — EC2, ELB, ASG
//   Database    — RDS, DynamoDB, ElastiCache
//   Serverless  — Lambda
//   Storage     — EBS
//
// The whole page is a single scrollable viewport; ↑↓ navigate across all
// items in all cards, with the viewport following the cursor.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/angsak/mbr/internal/aws/collector"
	"github.com/angsak/mbr/internal/aws/graph"
)

// ── Messages ──────────────────────────────────────────────────────────────────

// ShowOrphansMsg is produced when the user presses "o" on the resource list.
// The root AppModel handles it by switching to the orphan-review screen.
type ShowOrphansMsg struct{}

// ShowGraphMsg is produced when the user presses "g" on the resource list.
// NodeID is the resource the cursor was on (may be empty if none selected).
type ShowGraphMsg struct{ NodeID string }

// ShowDetailMsg is produced when the user presses Enter on the resource list.
// NodeID identifies the resource whose detail+cost panel should be opened.
type ShowDetailMsg struct{ NodeID string }

// ── Group definitions ─────────────────────────────────────────────────────────

// category groups related resource types under one card heading.
type category struct {
	name  string
	icon  string
	color lipgloss.Color
	types []collector.ResourceType
}

// categories defines the display order and membership for each card.
var categories = []category{
	{
		name:  "Networking",
		icon:  "⬡",
		color: lipgloss.Color("#06B6D4"),
		types: []collector.ResourceType{
			collector.TypeVPC, collector.TypeSubnet,
			collector.TypeSecurityGroup, collector.TypeIGW,
		},
	},
	{
		name:  "Compute",
		icon:  "⬡",
		color: lipgloss.Color("#3B82F6"),
		types: []collector.ResourceType{
			collector.TypeEC2Instance, collector.TypeELBV2,
			collector.TypeELBClassic, collector.TypeASG,
		},
	},
	{
		name:  "Database",
		icon:  "⬡",
		color: lipgloss.Color("#F97316"),
		types: []collector.ResourceType{
			collector.TypeRDSInstance, collector.TypeRDSCluster,
			collector.TypeDynamoTable, collector.TypeElastiCache,
		},
	},
	{
		name:  "Serverless",
		icon:  "⬡",
		color: lipgloss.Color("#A78BFA"),
		types: []collector.ResourceType{collector.TypeLambdaFunction},
	},
	{
		name:  "Storage",
		icon:  "⬡",
		color: lipgloss.Color("#10B981"),
		types: []collector.ResourceType{collector.TypeEBSVolume},
	},
}

// typeLabel maps each ResourceType to its display name used in sub-group headings.
var typeLabel = map[collector.ResourceType]string{
	collector.TypeVPC:            "VPC",
	collector.TypeSubnet:         "Subnet",
	collector.TypeSecurityGroup:  "Security Group",
	collector.TypeIGW:            "Internet Gateway",
	collector.TypeEC2Instance:    "EC2 Instance",
	collector.TypeELBV2:          "Load Balancer (v2)",
	collector.TypeELBClassic:     "Load Balancer (Classic)",
	collector.TypeASG:            "Auto Scaling Group",
	collector.TypeRDSInstance:    "RDS Instance",
	collector.TypeRDSCluster:     "RDS Cluster",
	collector.TypeDynamoTable:    "DynamoDB Table",
	collector.TypeElastiCache:    "ElastiCache Cluster",
	collector.TypeLambdaFunction: "Lambda Function",
	collector.TypeEBSVolume:      "EBS Volume",
}

// ── Model ─────────────────────────────────────────────────────────────────────

// renderedLine is one display row in the scrollable buffer.
type renderedLine struct {
	text     string // pre-rendered, full-width string
	nodeID   string // non-empty only for selectable resource rows
}

// ResourceListModel is the BubbleTea model for the grouped resource browser.
type ResourceListModel struct {
	g      *graph.ResourceGraph
	width  int
	height int

	// lines is the full pre-rendered line buffer (rebuilt on each View call).
	// cursor is an index into lines; offset is the first visible line.
	lines  []renderedLine
	cursor int // line index of the selected resource row
	offset int // first visible line index
}

// NewResourceListModel creates a model sized to the given content area.
// Lines are built immediately so Update can navigate before the first View call.
func NewResourceListModel(g *graph.ResourceGraph, width, height int) ResourceListModel {
	m := ResourceListModel{
		g:      g,
		width:  width,
		height: height,
	}
	m.lines = m.buildLines()
	m.cursor = m.firstSelectable()
	return m
}

// Init satisfies tea.Model.
func (m ResourceListModel) Init() tea.Cmd { return nil }

// Update handles resize and keyboard navigation.
// Lines are always rebuilt on resize so moveCursor has a valid buffer to work with.
func (m ResourceListModel) Update(msg tea.Msg) (ResourceListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.lines = m.buildLines()          // rebuild after resize
		m.cursor = m.firstSelectable()    // reset to first item

	case tea.KeyMsg:
		// Ensure lines are populated (guard against first Update before View).
		if len(m.lines) == 0 {
			m.lines = m.buildLines()
			m.cursor = m.firstSelectable()
		}
		switch msg.String() {
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "pgup", "ctrl+u":
			m.moveCursor(-(m.listRows() / 2))
		case "pgdown", "ctrl+d":
			m.moveCursor(m.listRows() / 2)
		case "enter":
			id := ""
			if m.cursor >= 0 && m.cursor < len(m.lines) {
				id = m.lines[m.cursor].nodeID
			}
			return m, func() tea.Msg { return ShowDetailMsg{NodeID: id} }
		case "i":
			return m, func() tea.Msg { return ShowIAMMsg{} }
		case "o":
			return m, func() tea.Msg { return ShowOrphansMsg{} }
		case "g":
			id := ""
			if m.cursor >= 0 && m.cursor < len(m.lines) {
				id = m.lines[m.cursor].nodeID
			}
			return m, func() tea.Msg { return ShowGraphMsg{NodeID: id} }
		}
	}
	return m, nil
}

// moveCursor advances the cursor by delta positions among selectable lines only,
// then scrolls the viewport to keep the cursor visible.
func (m *ResourceListModel) moveCursor(delta int) {
	if len(m.lines) == 0 {
		return
	}
	// Collect indices of selectable lines.
	sel := m.selectableIndices()
	if len(sel) == 0 {
		return
	}

	// Find where cursor currently sits in sel.
	pos := 0
	for i, idx := range sel {
		if idx == m.cursor {
			pos = i
			break
		}
	}

	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos >= len(sel) {
		pos = len(sel) - 1
	}
	m.cursor = sel[pos]
	m.scrollToCursor()
}

// scrollToCursor adjusts offset so the cursor line is always visible.
func (m *ResourceListModel) scrollToCursor() {
	h := m.listRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
}

// selectableIndices returns all line indices that are resource rows.
func (m *ResourceListModel) selectableIndices() []int {
	var out []int
	for i, l := range m.lines {
		if l.nodeID != "" {
			out = append(out, i)
		}
	}
	return out
}

// firstSelectable returns the line index of the first resource row, or 0.
func (m ResourceListModel) firstSelectable() int {
	for i, l := range m.lines {
		if l.nodeID != "" {
			return i
		}
	}
	return 0
}

// View renders the visible viewport. It is a pure function — it reads m.lines,
// m.cursor, and m.offset (set by Update) and does not mutate any state.
func (m ResourceListModel) View() string {
	if m.width == 0 || m.g == nil {
		return ""
	}

	lines := m.lines
	if len(lines) == 0 {
		// Lines not yet built (should not happen after NewResourceListModel,
		// but guard against an uninitialised model).
		lines = m.buildLines()
	}

	h := m.listRows()

	// Clamp offset to valid range.
	offset := m.offset
	if offset > len(lines)-h {
		offset = len(lines) - h
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + h
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := offset; i < end; i++ {
		line := lines[i].text
		// Highlight the cursor row with a visible arrow + brighter background.
		// Each resource row starts with 2 literal spaces ("  ") which we
		// replace with the cursor arrow so the indicator is always aligned.
		if i == m.cursor && lines[i].nodeID != "" {
			rest := ""
			if len(line) > 2 {
				rest = line[2:]
			}
			arrow := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7C3AED")).Bold(true).
				Render("▶")
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#374151")).
				Render(arrow + " " + rest)
		}
		b.WriteString(line + "\n")
	}

	// Pad any remaining rows so the footer is always at a fixed position.
	for i := end - offset; i < h; i++ {
		b.WriteString(strings.Repeat(" ", m.width) + "\n")
	}

	b.WriteString(m.renderFooter())
	return b.String()
}

// typeGroup holds the nodes for one resource type within a category.
type typeGroup struct {
	rt    collector.ResourceType
	nodes []*graph.Node
}

// buildLines constructs the full scrollable line buffer for all groups.
func (m ResourceListModel) buildLines() []renderedLine {
	var lines []renderedLine

	for _, cat := range categories {
		// Collect nodes grouped by type, preserving category order.
		var groups []typeGroup
		for _, rt := range cat.types {
			nodes := m.g.FilterByType(rt)
			if len(nodes) > 0 {
				groups = append(groups, typeGroup{rt: rt, nodes: nodes})
			}
		}
		if len(groups) == 0 {
			continue
		}

		totalCount := 0
		for _, grp := range groups {
			totalCount += len(grp.nodes)
		}

		// Spacer before each card (except the first).
		if len(lines) > 0 {
			lines = append(lines, renderedLine{text: ""})
		}

		// Card header.
		lines = append(lines, m.renderCategoryHeader(cat, totalCount))

		// Thin rule under header.
		rule := lipgloss.NewStyle().Foreground(cat.color).
			Render(strings.Repeat("─", m.width))
		lines = append(lines, renderedLine{text: rule})

		// Sub-groups: show a type header when there are 2+ populated types.
		useSubHeaders := len(groups) > 1
		for i, grp := range groups {
			if useSubHeaders {
				if i > 0 {
					lines = append(lines, renderedLine{text: ""})
				}
				lines = append(lines, m.renderSubGroupHeader(grp.rt, len(grp.nodes), cat.color))
			}
			for _, node := range grp.nodes {
				lines = append(lines, renderedLine{
					text:   m.renderResourceRow(node),
					nodeID: node.Resource.ID,
				})
			}
		}
	}

	// Orphan summary line.
	orphans := m.g.Orphans()
	if len(orphans) > 0 {
		lines = append(lines, renderedLine{text: ""})
		lines = append(lines, renderedLine{
			text: lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F59E0B")).Bold(true).
				Render(fmt.Sprintf("  ⚠  %d orphaned resource(s) detected — press o to review and delete", len(orphans))),
		})
	}

	return lines
}

// renderCategoryHeader renders the coloured card heading line.
func (m ResourceListModel) renderCategoryHeader(cat category, count int) renderedLine {
	iconStyle := lipgloss.NewStyle().Foreground(cat.color)
	nameStyle := lipgloss.NewStyle().Foreground(cat.color).Bold(true)
	countStyle := lipgloss.NewStyle().Foreground(cat.color)

	left := "  " + iconStyle.Render(cat.icon) + "  " + nameStyle.Render(strings.ToUpper(cat.name))
	right := countStyle.Render(fmt.Sprintf("%d resource(s)  ", count))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return renderedLine{text: left + strings.Repeat(" ", gap) + right}
}

// renderSubGroupHeader renders a type-level heading inside a category card.
// It is only shown when the card contains more than one populated resource type.
func (m ResourceListModel) renderSubGroupHeader(rt collector.ResourceType, count int, catColor lipgloss.Color) renderedLine {
	label := typeLabel[rt]
	if label == "" {
		label = string(rt)
	}
	labelStyle := lipgloss.NewStyle().Foreground(catColor).Bold(true)
	countStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	text := "  " + labelStyle.Render(label) +
		"  " + countStyle.Render(fmt.Sprintf("(%d)", count))
	return renderedLine{text: text}
}

// renderResourceRow renders a single resource row (without cursor highlight,
// which is applied in View).
func (m ResourceListModel) renderResourceRow(node *graph.Node) string {
	res := node.Resource

	// Orphan / status indicator (2 chars).
	indicator := "  "
	if node.IsOrphan {
		indicator = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true).Render("⚠ ")
	}

	// Raw ID column (fixed 22 chars).
	rawID := res.RawID
	if rawID == "" {
		rawID = res.ID
	}
	rawID = truncate(rawID, 22)
	idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	idCol := idStyle.Render(fmt.Sprintf("%-22s", rawID))

	// Name column (fixed 26 chars).
	name := res.DisplayName()
	if name == res.RawID {
		name = "" // avoid repeating the ID
	}
	name = truncate(name, 26)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6"))
	nameCol := nameStyle.Render(fmt.Sprintf("%-26s", name))

	// Meta column: most interesting field for this type (fixed 24 chars).
	meta := resourceMeta(res)
	meta = truncate(meta, 24)
	metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	metaCol := metaStyle.Render(fmt.Sprintf("%-24s", meta))

	// Region (right-aligned remainder).
	regionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))
	regionCol := regionStyle.Render(res.Region)

	row := "  " + indicator + idCol + "  " + nameCol + "  " + metaCol + "  " + regionCol

	// Pad to full width.
	w := lipgloss.Width(row)
	if w < m.width {
		row += strings.Repeat(" ", m.width-w)
	}
	return row
}

// resourceMeta returns the most useful single metadata string for a resource type.
func resourceMeta(res collector.Resource) string {
	switch res.Type {
	case collector.TypeEC2Instance:
		state := res.Metadata["State"]
		itype := res.Metadata["InstanceType"]
		if state != "" && itype != "" {
			return itype + "  " + state
		}
		return state + itype
	case collector.TypeEBSVolume:
		size := res.Metadata["SizeGiB"]
		state := res.Metadata["State"]
		if size != "" {
			return size + " GiB  " + state
		}
		return state
	case collector.TypeVPC:
		cidr := res.Metadata["CidrBlock"]
		if def := res.Metadata["IsDefault"]; def == "true" {
			return cidr + "  default"
		}
		return cidr
	case collector.TypeSubnet:
		return res.Metadata["CidrBlock"] + "  " + res.Metadata["AvailabilityZone"]
	case collector.TypeSecurityGroup:
		return res.Metadata["Description"]
	case collector.TypeIGW:
		if vpc := res.Metadata["AttachedVpcId"]; vpc != "" {
			return "attached  " + vpc
		}
		return "detached"
	case collector.TypeRDSInstance, collector.TypeRDSCluster:
		return res.Metadata["Engine"] + "  " + res.Metadata["Status"]
	case collector.TypeDynamoTable:
		return res.Metadata["Status"]
	case collector.TypeElastiCache:
		return res.Metadata["Engine"] + "  " + res.Metadata["Status"]
	case collector.TypeLambdaFunction:
		return res.Metadata["Runtime"]
	case collector.TypeELBV2, collector.TypeELBClassic:
		return res.Metadata["Scheme"] + "  " + res.Metadata["State"]
	case collector.TypeASG:
		return "min " + res.Metadata["MinSize"] + "  max " + res.Metadata["MaxSize"]
	}
	return ""
}

// renderFooter renders the key hint bar at the bottom.
func (m ResourceListModel) renderFooter() string {
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
		key("enter", "detail"),
		key("i", "iam"),
		key("o", "orphans"),
		key("g", "graph"),
		key("q", "quit"),
	}

	total := m.g.Len()
	right := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).
		Render(fmt.Sprintf("%d resources  ", total))

	left := "  " + strings.Join(hints, "   ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// listRows returns the number of rows available for the scrollable content,
// reserving 1 row for the footer.
func (m ResourceListModel) listRows() int {
	h := m.height - 1
	if h < 5 {
		return 5
	}
	return h
}

// truncate shortens s to maxLen runes, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return string(runes[:maxLen-1]) + "…"
}
