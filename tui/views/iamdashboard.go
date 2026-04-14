package views

// iamdashboard.go — IAM roles and policies dashboard.
//
// Layout:
//
//	  ⬡  IAM Dashboard                       42 roles · 18 policies
//	  ──────────────────────────────────────────────────────────────
//	  [ Roles ]  Policies                              tab to switch
//
//	  STATUS    ROLE NAME                  LAST USED       ASSUMED BY
//	  ● active  EC2InstanceRole            2 days ago      ec2
//	  ● active  LambdaExecutionRole        5 hours ago     lambda
//	  ~ stale   OldDeployRole              67 days ago     sts
//	  ✕ unused  TestRole                   184 days ago    sts
//	  ✕ never   ForgottenRole              —               ec2
//	  ──────────────────────────────────────────────────────────────
//	  ↑↓ nav   tab switch   enter detail   esc back

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anuragsarkar97/mbr/internal/aws/collector"
)

// ── Messages ─────────────────────────────────────────────────────────────────

// ShowIAMMsg is produced when the user presses "i" on the resource list.
type ShowIAMMsg struct{}

// IAMLoadedMsg carries the async IAM scan result back to the app.
type IAMLoadedMsg struct {
	Roles    []collector.Resource
	Policies []collector.Resource
	Err      error
}

// ── Model ─────────────────────────────────────────────────────────────────────

const (
	tabRoles    = 0
	tabPolicies = 1
)

// IAMDashboardModel is the BubbleTea model for the IAM dashboard screen.
type IAMDashboardModel struct {
	roles    []collector.Resource
	policies []collector.Resource
	loading  bool
	err      error

	tab    int // tabRoles or tabPolicies
	cursor int
	offset int
	width  int
	height int
}

// NewIAMDashboardModel creates a model in "loading" state.
func NewIAMDashboardModel(width, height int) IAMDashboardModel {
	return IAMDashboardModel{
		loading: true,
		width:   width,
		height:  height,
	}
}

// Init satisfies tea.Model.
func (m IAMDashboardModel) Init() tea.Cmd { return nil }

// Update handles resize, tab switching, navigation, and the async load result.
func (m IAMDashboardModel) Update(msg tea.Msg) (IAMDashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case IAMLoadedMsg:
		m.loading = false
		m.err = msg.Err
		m.roles = sortRoles(msg.Roles)
		m.policies = sortPolicies(msg.Policies)
		m.cursor = 0
		m.offset = 0

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.tab = 1 - m.tab // toggle 0↔1
			m.cursor = 0
			m.offset = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}
		case "down", "j":
			n := m.activeCount()
			if m.cursor < n-1 {
				m.cursor++
				h := m.listRows()
				if m.cursor >= m.offset+h {
					m.offset = m.cursor - h + 1
				}
			}
		case "pgup", "ctrl+u":
			h := m.listRows()
			m.cursor -= h / 2
			if m.cursor < 0 {
				m.cursor = 0
			}
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		case "pgdown", "ctrl+d":
			h := m.listRows()
			n := m.activeCount()
			m.cursor += h / 2
			if m.cursor >= n {
				m.cursor = n - 1
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

// View renders the IAM dashboard.
func (m IAMDashboardModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	// ── Title bar ────────────────────────────────────────────────────────────
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A78BFA")).Bold(true).
		Render("  ⬡  IAM Dashboard")

	summary := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).
		Render(fmt.Sprintf("%d roles · %d policies  ", len(m.roles), len(m.policies)))

	gap := m.width - lipgloss.Width(title) - lipgloss.Width(summary)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(title + strings.Repeat(" ", gap) + summary + "\n")

	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")).
		Render(strings.Repeat("─", m.width))
	b.WriteString(divider + "\n")

	// ── Tab bar ───────────────────────────────────────────────────────────────
	b.WriteString(m.renderTabBar() + "\n")
	b.WriteString("\n")

	// ── Body ─────────────────────────────────────────────────────────────────
	switch {
	case m.loading:
		placeholder := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).
			Render("  ⟳  Loading IAM data…")
		b.WriteString(placeholder + "\n")

	case m.err != nil:
		errLine := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).
			Render("  ⚠  " + m.err.Error())
		b.WriteString(errLine + "\n")

	case m.tab == tabRoles:
		b.WriteString(m.renderColumnHeaders("STATUS", "ROLE NAME", "LAST USED", "ASSUMED BY") + "\n")
		m.renderRows(&b, m.roles, m.renderRoleRow)

	case m.tab == tabPolicies:
		b.WriteString(m.renderColumnHeaders("STATUS", "POLICY NAME", "ATTACHED TO", "UPDATED") + "\n")
		m.renderRows(&b, m.policies, m.renderPolicyRow)
	}

	// Pad remaining rows.
	written := 4 // title + divider + tabbar + blank
	if !m.loading && m.err == nil {
		written++ // column header line
	}
	h := m.listRows()
	n := m.activeCount()
	visible := n - m.offset
	if visible > h {
		visible = h
	}
	if visible < 0 {
		visible = 0
	}
	written += visible
	for i := written; i < m.height-2; i++ {
		b.WriteString(strings.Repeat(" ", m.width) + "\n")
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	b.WriteString(divider + "\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

// ── Rendering helpers ─────────────────────────────────────────────────────────

func (m IAMDashboardModel) renderTabBar() string {
	active := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A78BFA")).Bold(true).
		Underline(true)
	inactive := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4B5563"))

	roleTab := "  Roles"
	polTab := "  Policies"
	if m.tab == tabRoles {
		roleTab = active.Render(roleTab)
		polTab = inactive.Render(polTab)
	} else {
		roleTab = inactive.Render(roleTab)
		polTab = active.Render(polTab)
	}

	tabStr := roleTab + "   " + polTab
	tabHint := hint.Render("tab to switch  ")
	gap := m.width - lipgloss.Width(tabStr) - lipgloss.Width(tabHint)
	if gap < 1 {
		gap = 1
	}
	return tabStr + strings.Repeat(" ", gap) + tabHint
}

func (m IAMDashboardModel) renderColumnHeaders(cols ...string) string {
	widths := []int{10, 36, 16, 0}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).Bold(true)
	var parts []string
	for i, col := range cols {
		if i < len(widths) && widths[i] > 0 {
			parts = append(parts, dim.Render(fmt.Sprintf("  %-*s", widths[i], col)))
		} else {
			parts = append(parts, dim.Render("  "+col))
		}
	}
	return strings.Join(parts, "")
}

func (m IAMDashboardModel) renderRows(b *strings.Builder, items []collector.Resource, render func(int, collector.Resource, bool) string) {
	h := m.listRows()
	n := len(items)
	offset := m.offset
	if offset > n-h {
		offset = n - h
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + h
	if end > n {
		end = n
	}
	for i := offset; i < end; i++ {
		isCursor := i == m.cursor
		b.WriteString(render(i, items[i], isCursor) + "\n")
	}
}

func (m IAMDashboardModel) renderRoleRow(_ int, res collector.Resource, isCursor bool) string {
	status := res.Metadata["UsageStatus"]
	daysSinceStr := res.Metadata["DaysSinceUsed"]
	days, _ := strconv.Atoi(daysSinceStr)
	trust := res.Metadata["TrustPrincipal"]
	isServiceLinked := res.Metadata["IsServiceLinked"] == "true"

	// Status badge (10 chars)
	statusBadge := formatRoleStatus(status)

	// Role name (36 chars)
	nameStr := truncate(res.RawID, 34)
	if isServiceLinked {
		nameStr = truncate(res.RawID, 30) + " ⬡"
	}
	nameCol := fmt.Sprintf("%-36s", nameStr)

	// Last used (16 chars)
	lastUsed := formatLastUsed(status, days)
	lastUsedCol := fmt.Sprintf("%-16s", truncate(lastUsed, 15))

	// Trust principal
	trustCol := truncate(trust, m.width-68)

	row := "  " + statusBadge + "  " + nameCol + "  " + lastUsedCol + "  " + trustCol

	// Pad to full width.
	w := lipgloss.Width(row)
	if w < m.width {
		row += strings.Repeat(" ", m.width-w)
	}

	if isCursor {
		cursor := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A78BFA")).Bold(true).Render("▶")
		rest := ""
		if len(row) > 2 {
			rest = row[2:]
		}
		row = lipgloss.NewStyle().
			Background(lipgloss.Color("#374151")).
			Render(cursor + " " + rest)
	}
	return row
}

func (m IAMDashboardModel) renderPolicyRow(_ int, res collector.Resource, isCursor bool) string {
	attachStr := res.Metadata["AttachmentCount"]
	attachCount, _ := strconv.Atoi(attachStr)
	updated := res.Metadata["UpdateDate"]

	// Status badge (10 chars)
	var statusBadge string
	if attachCount == 0 {
		statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).
			Render(fmt.Sprintf("%-10s", "✕ orphan"))
	} else {
		statusBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true).
			Render(fmt.Sprintf("%-10s", "● in use"))
	}

	// Policy name (36 chars)
	nameCol := fmt.Sprintf("%-36s", truncate(res.RawID, 35))

	// Attachment count (16 chars)
	attachLabel := fmt.Sprintf("%d entities", attachCount)
	if attachCount == 0 {
		attachLabel = "not attached"
	} else if attachCount == 1 {
		attachLabel = "1 entity"
	}
	attachCol := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).
		Render(fmt.Sprintf("%-16s", truncate(attachLabel, 15)))

	// Updated date
	updatedCol := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).
		Render(updated)

	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6"))
	row := "  " + statusBadge + "  " + nameStyle.Render(nameCol) + "  " + attachCol + "  " + updatedCol

	w := lipgloss.Width(row)
	if w < m.width {
		row += strings.Repeat(" ", m.width-w)
	}

	if isCursor {
		cursor := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A78BFA")).Bold(true).Render("▶")
		rest := ""
		if len(row) > 2 {
			rest = row[2:]
		}
		row = lipgloss.NewStyle().
			Background(lipgloss.Color("#374151")).
			Render(cursor + " " + rest)
	}
	return row
}

func (m IAMDashboardModel) renderFooter() string {
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
		key("tab", "switch panel"),
		key("esc", "back"),
	}

	// Summary counts for current tab.
	var summary string
	if m.tab == tabRoles {
		counts := roleCounts(m.roles)
		summary = lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).
			Render(fmt.Sprintf("%d active  %d stale  %d unused  %d never  ",
				counts["active"], counts["stale"], counts["unused"], counts["never"]))
	} else {
		orphans := 0
		for _, p := range m.policies {
			if p.Metadata["AttachmentCount"] == "0" {
				orphans++
			}
		}
		summary = lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).
			Render(fmt.Sprintf("%d orphaned policies  ", orphans))
	}

	left := "  " + strings.Join(hints, "   ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(summary)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + summary
}

// ── Formatting helpers ────────────────────────────────────────────────────────

func formatRoleStatus(status string) string {
	switch collector.IAMUsageStatus(status) {
	case collector.IAMStatusActive:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true).
			Render(fmt.Sprintf("%-10s", "● active"))
	case collector.IAMStatusStale:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true).
			Render(fmt.Sprintf("%-10s", "~ stale"))
	case collector.IAMStatusUnused:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).
			Render(fmt.Sprintf("%-10s", "✕ unused"))
	default: // never
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).
			Render(fmt.Sprintf("%-10s", "✕ never"))
	}
}

func formatLastUsed(status string, days int) string {
	switch collector.IAMUsageStatus(status) {
	case collector.IAMStatusNever:
		return "—"
	case collector.IAMStatusActive:
		if days == 0 {
			return "today"
		}
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

// ── Sorting ───────────────────────────────────────────────────────────────────

// statusOrder maps status to a sort priority (lower = shown first).
var statusOrder = map[string]int{
	string(collector.IAMStatusActive): 0,
	string(collector.IAMStatusStale):  1,
	string(collector.IAMStatusUnused): 2,
	string(collector.IAMStatusNever):  3,
}

// sortRoles orders roles: active → stale → unused → never, then by name.
func sortRoles(roles []collector.Resource) []collector.Resource {
	sorted := make([]collector.Resource, len(roles))
	copy(sorted, roles)
	sort.SliceStable(sorted, func(i, j int) bool {
		si := statusOrder[sorted[i].Metadata["UsageStatus"]]
		sj := statusOrder[sorted[j].Metadata["UsageStatus"]]
		if si != sj {
			return si < sj
		}
		return sorted[i].RawID < sorted[j].RawID
	})
	return sorted
}

// sortPolicies orders policies: in-use first (most attached first), orphaned last.
func sortPolicies(policies []collector.Resource) []collector.Resource {
	sorted := make([]collector.Resource, len(policies))
	copy(sorted, policies)
	sort.SliceStable(sorted, func(i, j int) bool {
		ai, _ := strconv.Atoi(sorted[i].Metadata["AttachmentCount"])
		aj, _ := strconv.Atoi(sorted[j].Metadata["AttachmentCount"])
		if ai != aj {
			return ai > aj // more attached = first
		}
		return sorted[i].RawID < sorted[j].RawID
	})
	return sorted
}

// ── Counters ──────────────────────────────────────────────────────────────────

func roleCounts(roles []collector.Resource) map[string]int {
	counts := map[string]int{"active": 0, "stale": 0, "unused": 0, "never": 0}
	for _, r := range roles {
		s := r.Metadata["UsageStatus"]
		counts[s]++
	}
	return counts
}

func (m IAMDashboardModel) activeCount() int {
	if m.tab == tabRoles {
		return len(m.roles)
	}
	return len(m.policies)
}

// listRows returns the number of rows available for the scrollable list.
// Layout: title(1) + divider(1) + tabbar(1) + blank(1) + colheader(1) + list(N) + padding + divider(1) + footer(1)
func (m IAMDashboardModel) listRows() int {
	h := m.height - 7
	if h < 3 {
		return 3
	}
	return h
}
