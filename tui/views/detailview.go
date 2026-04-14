package views

// detailview.go shows full resource metadata and 30-day cost for the
// selected resource. Press esc to return to the resource list.
//
// Layout:
//
//	  ◈  Resource Detail — i-0abc123  (ec2:instance)
//	  ─────────────────────────────────────────────
//	  ID          arn:aws:ec2:us-east-1:instance/i-0abc123
//	  Name        My Web Server
//	  Region      us-east-1
//	  Account     123456789012
//	  ...
//
//	  METADATA
//	  InstanceType   t3.medium
//	  State          running
//	  ...
//
//	  TAGS
//	  Name           My Web Server
//	  Environment    production
//	  ...
//
//	  COST  (30-day blended)
//	  ⟳ Loading…              ← replaced with USD amount on arrival
//	  ─────────────────────────────────────────────
//	  ↑↓ scroll   esc back

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/angsak/mbr/internal/aws/collector"
	"github.com/angsak/mbr/internal/aws/cost"
	"github.com/angsak/mbr/internal/aws/graph"
)

// ── Messages ─────────────────────────────────────────────────────────────────

// CostResultMsg carries the async cost fetch result back to the detail view.
type CostResultMsg struct {
	NodeID string
	Result cost.Result
}

// ── Model ─────────────────────────────────────────────────────────────────────

// DetailViewModel is the BubbleTea model for the resource detail screen.
type DetailViewModel struct {
	node        *graph.Node
	costLoading bool
	costResult  *cost.Result
	lines       []string
	offset      int
	width       int
	height      int
}

// NewDetailViewModel creates a model for the given node.
// costLoading should be true when the caller has already dispatched a cost
// fetch command; the view shows a spinner row until CostResultMsg arrives.
func NewDetailViewModel(node *graph.Node, width, height int) DetailViewModel {
	m := DetailViewModel{
		node:        node,
		costLoading: true,
		width:       width,
		height:      height,
	}
	m.lines = m.buildLines()
	return m
}

// Init satisfies tea.Model.
func (m DetailViewModel) Init() tea.Cmd { return nil }

// Update handles resize, scrolling, and the async cost result.
func (m DetailViewModel) Update(msg tea.Msg) (DetailViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.lines = m.buildLines()

	case CostResultMsg:
		if m.node != nil && msg.NodeID == m.node.Resource.ID {
			r := msg.Result
			m.costResult = &r
			m.costLoading = false
			m.node.CostUSD = r.USD
			m.lines = m.buildLines()
		}

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

// View renders the detail screen.
func (m DetailViewModel) View() string {
	if m.width == 0 || m.node == nil {
		return ""
	}

	var b strings.Builder

	// ── Title ────────────────────────────────────────────────────────────────
	res := m.node.Resource
	rawID := res.RawID
	if rawID == "" {
		rawID = res.ID
	}
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3B82F6")).Bold(true).
		Render(fmt.Sprintf("  ◈  %s", truncate(rawID, 36)))
	typeStr := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Render(fmt.Sprintf("(%s)  ", string(res.Type)))

	gap := m.width - lipgloss.Width(title) - lipgloss.Width(typeStr)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(title + strings.Repeat(" ", gap) + typeStr + "\n")

	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151")).
		Render(strings.Repeat("─", m.width))
	b.WriteString(divider + "\n")

	// ── Scrollable body ───────────────────────────────────────────────────────
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
		b.WriteString(m.lines[i] + "\n")
	}
	for i := end - offset; i < h; i++ {
		b.WriteString(strings.Repeat(" ", m.width) + "\n")
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	b.WriteString(divider + "\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

// buildLines constructs the full scrollable line buffer.
func (m DetailViewModel) buildLines() []string {
	if m.node == nil {
		return nil
	}
	res := m.node.Resource

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	bright := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6"))
	section := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Bold(true)
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true)
	blue := lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6"))

	kw := 16 // key column width

	kv := func(key, val string) string {
		return "  " + dim.Render(fmt.Sprintf("%-*s", kw, key)) + "  " + bright.Render(val)
	}

	var lines []string
	add := func(s string) { lines = append(lines, s) }

	// ── Identity ──────────────────────────────────────────────────────────────
	add("")
	rawID := res.RawID
	if rawID == "" {
		rawID = res.ID
	}
	if res.ID != rawID {
		add(kv("ARN", truncate(res.ID, m.width-kw-6)))
	}
	add(kv("ID", rawID))
	if res.Name != "" {
		add(kv("Name", res.Name))
	}
	add(kv("Type", string(res.Type)))
	add(kv("Region", res.Region))
	if res.AccountID != "" {
		add(kv("Account", res.AccountID))
	}
	if m.node.IsOrphan {
		orphanStr := amber.Render("⚠  orphan")
		if len(m.node.OrphanReasons) > 0 {
			orphanStr += dim.Render("  — "+m.node.OrphanReasons[0])
		}
		add("  " + orphanStr)
	}

	// ── Type-specific or generic metadata ─────────────────────────────────────
	switch res.Type {

	case collector.TypeSecurityGroup:
		add("")
		add("  " + section.Render("CONFIGURATION"))
		add(kv("Group Name", res.Metadata["GroupName"]))
		add(kv("VPC", res.Metadata["VpcId"]))
		if d := res.Metadata["Description"]; d != "" {
			add(kv("Description", d))
		}

		add("")
		add("  " + section.Render("INBOUND RULES"))
		for _, line := range renderSGRules(res.Metadata["InboundRules"]) {
			add(line)
		}

		add("")
		add("  " + section.Render("OUTBOUND RULES"))
		for _, line := range renderSGRules(res.Metadata["OutboundRules"]) {
			add(line)
		}

	case collector.TypeASG:
		add("")
		add("  " + section.Render("CAPACITY"))
		add(kv("Min / Desired / Max", fmt.Sprintf("%s / %s / %s",
			res.Metadata["MinSize"], res.Metadata["DesiredCapacity"], res.Metadata["MaxSize"])))
		add(kv("In Service", res.Metadata["InServiceCount"]+" instances"))
		if n := res.Metadata["PendingCount"]; n != "0" {
			add(kv("Pending", n+" instances"))
		}
		if n := res.Metadata["TerminatingCount"]; n != "0" {
			add(kv("Terminating", n+" instances"))
		}
		add(kv("Health Check", res.Metadata["HealthCheckType"]))
		if lt := res.Metadata["LaunchTemplate"]; lt != "" {
			add(kv("Launch Template", lt))
		}
		add(kv("Status", res.Metadata["Status"]))

		if azs := res.Metadata["AvailabilityZones"]; azs != "" {
			add("")
			add("  " + section.Render("AVAILABILITY ZONES"))
			for _, az := range strings.Split(azs, ",") {
				add("  " + dim.Render("•") + " " + bright.Render(strings.TrimSpace(az)))
			}
		}

		if tgs := res.Metadata["TargetGroupARNs"]; tgs != "" {
			add("")
			add("  " + section.Render("TARGET GROUPS"))
			for _, tg := range strings.Split(tgs, ",") {
				tg = strings.TrimSpace(tg)
				// Show targetgroup/name/hash from the ARN for readability.
				parts := strings.Split(tg, ":")
				display := tg
				if len(parts) > 5 {
					display = parts[len(parts)-1]
				}
				add("  " + dim.Render("•") + " " + bright.Render(display))
			}
		}

	case collector.TypeRDSInstance:
		add("")
		add("  " + section.Render("CONNECTIVITY"))
		add(kv("Endpoint", res.Metadata["Endpoint"]))
		add(kv("Engine", res.Metadata["Engine"]+" "+res.Metadata["EngineVersion"]))
		add(kv("Instance Class", res.Metadata["DBInstanceClass"]))
		add(kv("Multi-AZ", res.Metadata["MultiAZ"]))
		add(kv("VPC", res.Metadata["VpcId"]))
		add(kv("Publicly Accessible", res.Metadata["PubliclyAccessible"]))

		add("")
		add("  " + section.Render("STORAGE"))
		add(kv("Allocated", res.Metadata["StorageGB"]+" GB"))
		add(kv("Storage Type", res.Metadata["StorageType"]))

		add("")
		add("  " + section.Render("MANAGEMENT"))
		add(kv("Status", res.Metadata["Status"]))
		if br := res.Metadata["BackupRetention"]; br != "" && br != "0" {
			add(kv("Backup Retention", br+" days"))
		}
		add(kv("Deletion Protection", res.Metadata["DeletionProtection"]))
		if pg := res.Metadata["ParameterGroup"]; pg != "" {
			add(kv("Parameter Group", pg))
		}

	default:
		// Generic metadata dump for all other resource types.
		if len(res.Metadata) > 0 {
			add("")
			add("  " + section.Render("METADATA"))

			keys := make([]string, 0, len(res.Metadata))
			for k := range res.Metadata {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				add(kv(k, res.Metadata[k]))
			}
		}
	}

	// ── Tags ──────────────────────────────────────────────────────────────────
	if len(res.Tags) > 0 {
		add("")
		add("  " + section.Render("TAGS"))

		keys := make([]string, 0, len(res.Tags))
		for k := range res.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			add(kv(k, res.Tags[k]))
		}
	}

	// ── Cost ──────────────────────────────────────────────────────────────────
	add("")
	add("  " + section.Render("COST") + "  " + dim.Render("(30-day blended, USD)"))

	switch {
	case m.costLoading:
		add("  " + dim.Render("⟳  Loading…"))
	case m.costResult == nil || m.costResult.Granularity == "none":
		add("  " + dim.Render("—  not available"))
	case m.costResult.Err != nil:
		add("  " + amber.Render("⚠  "+m.costResult.Err.Error()))
	case m.costResult.Granularity == "resource":
		add("  " + green.Render(fmt.Sprintf("$%.4f", m.costResult.USD)) +
			"  " + dim.Render("(per-resource)"))
	case m.costResult.Granularity == "service":
		svc := ""
		if sn, ok := cost.ServiceNameFor(res.Type); ok {
			svc = sn
		}
		add("  " + blue.Render(fmt.Sprintf("$%.2f", m.costResult.USD)) +
			"  " + dim.Render(fmt.Sprintf("(service total: %s)", svc)))
	}

	add("")
	return lines
}

// renderFooter renders the key-hint bar.
func (m DetailViewModel) renderFooter() string {
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
	left := "  " + strings.Join(hints, "   ")
	return left
}

// listRows returns the number of rows available for scrollable content.
// Layout: title(1) + divider(1) + content(N) + divider(1) + footer(1) = N+4
func (m DetailViewModel) listRows() int {
	h := m.height - 4
	if h < 3 {
		return 3
	}
	return h
}

// renderSGRules parses the pipe-delimited SG rule string produced by
// collector.formatIpPermissions and returns formatted display lines.
// Each rule has the form: protocol|portRange|source|description
func renderSGRules(rules string) []string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	proto_s := lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Bold(true)
	port_s := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6"))
	src_s := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))

	if rules == "" {
		return []string{"  " + dim.Render("(none)")}
	}

	var lines []string
	for _, rule := range strings.Split(rules, "\n") {
		if rule == "" {
			continue
		}
		parts := strings.SplitN(rule, "|", 4)
		if len(parts) < 4 {
			continue
		}
		proto, port, src, desc := parts[0], parts[1], parts[2], parts[3]

		line := "  " + proto_s.Render(fmt.Sprintf("%-8s", strings.ToUpper(proto)))
		line += port_s.Render(fmt.Sprintf("%-12s", port))
		if len(src) > 28 {
			src = src[:25] + "..."
		}
		line += "  " + src_s.Render(fmt.Sprintf("%-28s", src))
		if desc != "" {
			line += "  " + dim.Render(desc)
		}
		lines = append(lines, line)
	}
	return lines
}
