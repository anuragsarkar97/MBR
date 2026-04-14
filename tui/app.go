package tui

// app.go is the root BubbleTea model for mbr.
//
// Layout (full-screen):
//
//	┌────────────────────────────────────────────────────────┐
//	│  mbr  ·  AWS Resource Browser            profile@region│  ← header (3 rows)
//	├────────────────────────────────────────────────────────┤
//	│                                                        │
//	│                   [active view]                        │  ← fills remaining height
//	│                                                        │
//	├────────────────────────────────────────────────────────┤
//	│  ↑↓ nav  / filter  o orphans  tab type  q quit        │  ← status bar (1 row)
//	└────────────────────────────────────────────────────────┘

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/angsak/mbr/internal/aws/collector"
	"github.com/angsak/mbr/internal/aws/cost"
	"github.com/angsak/mbr/internal/aws/graph"
	"github.com/angsak/mbr/internal/aws/orphan"
	"github.com/angsak/mbr/tui/components"
	"github.com/angsak/mbr/tui/views"
)

// headerHeight is the number of terminal rows consumed by the top header bar.
const headerHeight = 3

// statusBarHeight is the number of rows consumed by the bottom status bar.
const statusBarHeight = 1

// Screen identifies which view is currently active.
type Screen int

const (
	ScreenRegionSelect Screen = iota // initial region picker
	ScreenLoading                    // scanning in progress
	ScreenResourceList               // main resource browser
	ScreenOrphans                    // orphaned-resource review
	ScreenGraph                      // dependency graph for a single resource
	ScreenDetail                     // resource detail + cost
	ScreenIAM                        // IAM roles and policies dashboard
)

// AppModel is the root BubbleTea model.
type AppModel struct {
	// screen is the currently active view.
	screen Screen

	// width and height are the current terminal dimensions.
	width, height int

	// awsCfg is the AWS configuration loaded at startup.
	awsCfg aws.Config

	// awsProfile and awsRegion are passed from CLI flags for display.
	awsProfile string
	awsRegions []string // selected regions; empty means all

	// graph holds the full resource graph after a successful scan.
	resourceGraph *graph.ResourceGraph

	// allResources is the flat slice backing the graph; kept for re-builds.
	allResources []collector.Resource

	// Sub-models for each screen.
	regionView   views.RegionModel
	resourceList views.ResourceListModel
	orphanList   views.OrphanListModel
	graphView    views.GraphViewModel
	detailView   views.DetailViewModel
	iamDashboard views.IAMDashboardModel
	spinner      components.SpinnerModel
	statusBar    components.StatusBarModel

	// loadingMsg is shown under the spinner during scan.
	loadingMsg string

	// scanErr holds any non-fatal error from the last scan.
	scanErr error
}

// NewApp creates the root AppModel. profile and regions come from CLI flags.
// If regions is empty the region selector screen is shown first.
func NewApp(cfg aws.Config, profile string, regions []string) AppModel {
	m := AppModel{
		awsCfg:     cfg,
		awsProfile: profile,
		awsRegions: regions,
		spinner:    components.NewSpinner(),
		statusBar:  components.NewStatusBar(),
	}
	m.statusBar.Profile = profile
	if len(regions) == 1 {
		m.statusBar.Region = regions[0]
	} else if len(regions) > 1 {
		m.statusBar.Region = fmt.Sprintf("%d regions", len(regions))
	}

	// If regions were pre-selected via --region flag, skip the picker.
	if len(regions) > 0 {
		m.screen = ScreenLoading
	} else {
		m.screen = ScreenRegionSelect
		m.regionView = views.NewRegionModel()
	}
	return m
}

// Init returns the initial command.
// Region selection needs no async fetch — AllAWSRegions is pre-populated.
// If --region was given we skip straight to scanning.
func (m AppModel) Init() tea.Cmd {
	switch m.screen {
	case ScreenRegionSelect:
		return m.spinner.Init() // spinner is not shown here but keeps it ticking for later
	case ScreenLoading:
		return tea.Batch(m.spinner.Init(), startScanCmd(m.awsCfg, m.awsRegions))
	}
	return nil
}

// contentHeight returns the number of rows available for the active view,
// excluding the header and status bar.
func (m AppModel) contentHeight() int {
	h := m.height - headerHeight - statusBarHeight
	if h < 1 {
		return 1
	}
	return h
}

// Update routes incoming messages to the active sub-model and handles
// global key bindings (quit, resize).
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Always resize the status bar (needs full terminal width).
		var sbCmd tea.Cmd
		m.statusBar, sbCmd = m.statusBar.Update(msg)

		// Resize content-area sub-models.
		// regionView is always safe to resize — it is constructed in NewApp
		// via NewRegionModel() which initialises the bubbles list properly.
		// resourceList must only be resized after the scan completes; before
		// that it is a zero-value struct whose internal paginator is nil and
		// will panic if SetWidth/SetHeight is called.
		contentMsg := tea.WindowSizeMsg{
			Width:  msg.Width,
			Height: m.contentHeight(),
		}
		var rvCmd, rlCmd tea.Cmd
		m.regionView, rvCmd = m.regionView.Update(contentMsg)
		if m.resourceGraph != nil {
			m.resourceList, rlCmd = m.resourceList.Update(contentMsg)
			if m.screen == ScreenOrphans {
				m.orphanList, _ = m.orphanList.Update(contentMsg)
			}
			if m.screen == ScreenGraph {
				m.graphView, _ = m.graphView.Update(contentMsg)
			}
			if m.screen == ScreenDetail {
				m.detailView, _ = m.detailView.Update(contentMsg)
			}
		}
		if m.screen == ScreenIAM {
			m.iamDashboard, _ = m.iamDashboard.Update(contentMsg)
		}
		return m, tea.Batch(sbCmd, rvCmd, rlCmd)

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, GlobalKeys.Quit):
			return m, tea.Quit
		case key.Matches(msg, GlobalKeys.Back):
			switch m.screen {
			case ScreenOrphans, ScreenGraph, ScreenDetail, ScreenIAM:
				m.screen = ScreenResourceList
				return m, nil
			case ScreenResourceList:
				// Return to the region picker.
				m.screen = ScreenRegionSelect
				m.regionView = views.NewRegionModel()
				// Resize the fresh region view to current terminal dimensions.
				m.regionView, _ = m.regionView.Update(tea.WindowSizeMsg{
					Width: m.width, Height: m.contentHeight(),
				})
				return m, nil
			}
		}

	// ── Region selection ──────────────────────────────────────────────────
	case views.RegionSelectedMsg:
		m.awsRegions = msg.Regions
		m.statusBar.Region = fmt.Sprintf("%d region(s)", len(msg.Regions))
		m.screen = ScreenLoading
		m.loadingMsg = "Connecting to AWS…"
		return m, tea.Batch(m.spinner.Init(), startScanCmd(m.awsCfg, m.awsRegions))

	// ── Scan progress ─────────────────────────────────────────────────────
	case components.ProgressMsg:
		m.loadingMsg = fmt.Sprintf("Scanning %s / %s  (%d/%d)",
			msg.Region, msg.ResourceType, msg.Done, msg.Total)
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case scanResultMsg:
		if msg.err != nil {
			m.scanErr = msg.err
		}
		m.allResources = msg.resources
		g := graph.BuildGraph(m.allResources)
		orphan.RunAll(g)
		m.resourceGraph = g
		m.statusBar.ResourceCount = g.Len()
		m.screen = ScreenResourceList
		m.resourceList = views.NewResourceListModel(g, m.width, m.contentHeight())
		return m, nil

	// ── Orphan / Graph / Detail screen transitions ───────────────────────
	case views.ShowOrphansMsg:
		m.screen = ScreenOrphans
		m.orphanList = views.NewOrphanListModel(m.resourceGraph, m.width, m.contentHeight())
		return m, nil

	case views.ShowGraphMsg:
		m.screen = ScreenGraph
		m.graphView = views.NewGraphViewModel(m.resourceGraph, msg.NodeID, m.width, m.contentHeight())
		return m, nil

	case views.ShowDetailMsg:
		if node, ok := m.resourceGraph.Node(msg.NodeID); ok {
			m.screen = ScreenDetail
			m.detailView = views.NewDetailViewModel(node, m.width, m.contentHeight())
			return m, fetchCostCmd(m.awsCfg, node)
		}
		return m, nil

	case views.CostResultMsg:
		m.detailView, _ = m.detailView.Update(msg)
		return m, nil

	// ── IAM dashboard ─────────────────────────────────────────────────────
	case views.ShowIAMMsg:
		m.screen = ScreenIAM
		m.iamDashboard = views.NewIAMDashboardModel(m.width, m.contentHeight())
		return m, loadIAMCmd(m.awsCfg)

	case views.IAMLoadedMsg:
		m.iamDashboard, _ = m.iamDashboard.Update(msg)
		return m, nil

	// ── Spinner ticks ─────────────────────────────────────────────────────
	default:
		if m.screen == ScreenLoading || m.screen == ScreenRegionSelect {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	// Delegate to active sub-model.
	switch m.screen {
	case ScreenRegionSelect:
		updated, cmd := m.regionView.Update(msg)
		m.regionView = updated
		return m, cmd

	case ScreenResourceList:
		updated, cmd := m.resourceList.Update(msg)
		m.resourceList = updated
		return m, cmd

	case ScreenOrphans:
		updated, cmd := m.orphanList.Update(msg)
		m.orphanList = updated
		return m, cmd

	case ScreenGraph:
		updated, cmd := m.graphView.Update(msg)
		m.graphView = updated
		return m, cmd

	case ScreenDetail:
		updated, cmd := m.detailView.Update(msg)
		m.detailView = updated
		return m, cmd

	case ScreenIAM:
		updated, cmd := m.iamDashboard.Update(msg)
		m.iamDashboard = updated
		return m, cmd
	}

	return m, nil
}

// View renders the full-screen layout: header + content + status bar.
// The content area is always exactly contentHeight() rows tall so no
// blank lines appear at the bottom of the terminal.
func (m AppModel) View() string {
	if m.width == 0 {
		return "" // terminal size not yet known
	}

	header := m.renderHeader()
	status := m.statusBar.View()
	body := m.renderBody()

	return header + body + status
}

// renderHeader draws the top bar: logo on the left, account info on the right.
func (m AppModel) renderHeader() string {
	logo := lipgloss.NewStyle().
		Foreground(Palette.Primary).
		Bold(true).
		Render("  mbr")

	tagline := Styles.Dim.Render("  AWS Resource Browser")

	right := ""
	if m.awsProfile != "" {
		right = Styles.Dim.Render(m.awsProfile + "  ")
	}

	// Inner content row: logo + tagline left-aligned, profile right-aligned.
	leftPart := logo + tagline
	rightPart := right
	gap := m.width - lipgloss.Width(leftPart) - lipgloss.Width(rightPart)
	if gap < 0 {
		gap = 0
	}
	inner := leftPart + strings.Repeat(" ", gap) + rightPart

	// Draw a full-width coloured header row.
	headerStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#1F2937")). // dark background
		Width(m.width)

	divider := lipgloss.NewStyle().
		Foreground(Palette.Border).
		Width(m.width).
		Render(strings.Repeat("─", m.width))

	return headerStyle.Render(inner) + "\n" + divider + "\n"
}

// renderBody renders the active screen's content area, padded to exactly
// contentHeight() rows so the layout never shifts.
func (m AppModel) renderBody() string {
	w := m.width
	h := m.contentHeight()

	var content string

	switch m.screen {
	case ScreenRegionSelect:
		content = m.regionView.View()

	case ScreenLoading:
		// Centre the spinner + message in the content area.
		inner := Styles.Title.Render("mbr — AWS Resource Browser") + "\n\n" +
			m.spinner.View() + "  " + Styles.Dim.Render(m.loadingMsg)
		content = lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, inner)

	case ScreenResourceList:
		content = m.resourceList.View()

	case ScreenOrphans:
		content = m.orphanList.View()

	case ScreenGraph:
		content = m.graphView.View()

	case ScreenDetail:
		content = m.detailView.View()

	case ScreenIAM:
		content = m.iamDashboard.View()
	}

	if m.scanErr != nil {
		content += "\n" + Styles.Orphan.Render("⚠  "+m.scanErr.Error())
	}

	// Pad content to exactly h rows using Lipgloss so the status bar is
	// always anchored to the bottom regardless of how many lines the view
	// returned. Height() appends newlines until the minimum row count is met.
	return lipgloss.NewStyle().Height(h).Render(content)
}

// ── Commands ─────────────────────────────────────────────────────────────────

// scanResultMsg carries the collected resources (and any error) back to Update.
type scanResultMsg struct {
	resources []collector.Resource
	err       error
}

// loadIAMCmd fetches all IAM roles and policies asynchronously.
func loadIAMCmd(cfg aws.Config) tea.Cmd {
	return func() tea.Msg {
		result := collector.CollectIAM(context.Background(), cfg)
		return views.IAMLoadedMsg{
			Roles:    result.Roles,
			Policies: result.Policies,
			Err:      result.Err,
		}
	}
}

// fetchCostCmd runs the Cost Explorer lookup for a single node asynchronously.
func fetchCostCmd(cfg aws.Config, node *graph.Node) tea.Cmd {
	return func() tea.Msg {
		result := cost.FetchResource(context.Background(), cfg, node.Resource)
		return views.CostResultMsg{NodeID: node.Resource.ID, Result: result}
	}
}

// startScanCmd runs the full collection pipeline asynchronously.
func startScanCmd(cfg aws.Config, regions []string) tea.Cmd {
	return func() tea.Msg {
		resources, err := collector.RunAll(
			context.Background(),
			cfg,
			regions,
			collector.DefaultRegistry,
			10,
			nil,
		)
		return scanResultMsg{resources: resources, err: err}
	}
}

