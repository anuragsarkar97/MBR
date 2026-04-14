// mbr is a CLI tool for browsing AWS resources across all regions,
// visualising resource relationships, detecting orphans, and analysing costs.
//
// Usage:
//
//	mbr                         # Launch interactive TUI (default)
//	mbr scan                    # Scan and print JSON to stdout
//	mbr scan --region us-east-1 # Scan a single region
//	mbr orphans                 # List orphaned resources (text/JSON)
//	mbr version                 # Print version info
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/aws"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	awspkg "github.com/angsak/mbr/internal/aws"
	"github.com/angsak/mbr/internal/aws/collector"
	"github.com/angsak/mbr/internal/aws/graph"
	"github.com/angsak/mbr/internal/aws/orphan"
	"github.com/angsak/mbr/internal/version"
	"github.com/angsak/mbr/tui"

	// Blank imports register all collectors and orphan rules via init().
	_ "github.com/angsak/mbr/internal/aws/collector"
	_ "github.com/angsak/mbr/internal/aws/orphan"
)

// Global flags shared across all commands.
var (
	flagProfile string
	flagRegion  string
	flagOutput  string // "text" or "json"
)

func main() {
	root := buildRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// buildRootCmd constructs the full Cobra command tree.
func buildRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mbr",
		Short: "AWS Resource Browser — visualise and manage AWS resources",
		Long: `mbr scans your AWS account across all regions, maps resource
relationships, detects orphaned resources, and shows cost breakdowns
— all in an interactive terminal UI.

Run without a subcommand to launch the TUI.`,
		// Running "mbr" with no subcommand launches the TUI.
		RunE: runTUI,
	}

	// Persistent flags are available to every subcommand.
	root.PersistentFlags().StringVar(&flagProfile, "profile", "", "AWS profile name (default: AWS_PROFILE env var)")
	root.PersistentFlags().StringVar(&flagRegion, "region", "", "Limit scan to one region (default: all enabled regions)")
	root.PersistentFlags().StringVar(&flagOutput, "output", "text", "Output format: text or json")

	root.AddCommand(buildScanCmd())
	root.AddCommand(buildOrphansCmd())
	root.AddCommand(buildVersionCmd())

	return root
}

// ── TUI (default command) ─────────────────────────────────────────────────────

// runTUI is the default action: launch the BubbleTea interactive UI.
func runTUI(cmd *cobra.Command, args []string) error {
	cfg, err := awspkg.LoadConfig(context.Background(), flagProfile, flagRegion)
	if err != nil {
		return fmt.Errorf("AWS config: %w", err)
	}

	regions := []string{}
	if flagRegion != "" {
		regions = []string{flagRegion}
	}

	app := tui.NewApp(cfg, flagProfile, regions)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI: %w", err)
	}
	return nil
}

// ── scan subcommand ───────────────────────────────────────────────────────────

// buildScanCmd builds the "mbr scan" subcommand, which collects resources
// and prints them as text or JSON without launching the TUI.
func buildScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Scan AWS resources and print results",
		RunE:  runScan,
	}
}

// runScan performs a headless scan and prints results to stdout.
func runScan(cmd *cobra.Command, args []string) error {
	awsCfg, regions, err := loadConfigAndRegions()
	if err != nil {
		return err
	}
	cfg := awsCfg

	fmt.Fprintf(os.Stderr, "Scanning %d region(s)…\n", len(regions))

	resources, err := collector.RunAll(
		context.Background(),
		cfg, regions,
		collector.DefaultRegistry,
		10,
		func(region, rt string) {
			fmt.Fprintf(os.Stderr, "  ✓ %s / %s\n", region, rt)
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: partial errors during scan: %v\n", err)
	}

	return printResources(resources)
}

// ── orphans subcommand ────────────────────────────────────────────────────────

// buildOrphansCmd builds the "mbr orphans" subcommand.
func buildOrphansCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "orphans",
		Short: "List orphaned (unused) AWS resources",
		RunE:  runOrphans,
	}
}

// runOrphans scans, builds the graph, runs orphan detection, and prints results.
func runOrphans(cmd *cobra.Command, args []string) error {
	awsCfg, regions, err := loadConfigAndRegions()
	if err != nil {
		return err
	}
	cfg := awsCfg

	fmt.Fprintf(os.Stderr, "Scanning %d region(s)…\n", len(regions))

	resources, err := collector.RunAll(
		context.Background(),
		cfg, regions,
		collector.DefaultRegistry,
		10, nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	g := graph.BuildGraph(resources)
	orphan.RunAll(g)

	orphans := g.Orphans()
	if len(orphans) == 0 {
		fmt.Println("No orphaned resources found.")
		return nil
	}

	fmt.Printf("Found %d orphaned resource(s):\n\n", len(orphans))
	return printNodes(orphans)
}

// ── version subcommand ────────────────────────────────────────────────────────

// buildVersionCmd builds the "mbr version" subcommand.
func buildVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("mbr %s (commit %s, built %s)\n",
				version.Version, version.Commit, version.Date)
		},
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// loadConfigAndRegions loads the AWS config and resolves the region list,
// calling DescribeRegions if --region was not specified.
func loadConfigAndRegions() (awsCfg awsconfig.Config, regions []string, err error) {
	awsCfg, err = awspkg.LoadConfig(context.Background(), flagProfile, flagRegion)
	if err != nil {
		return awsCfg, nil, fmt.Errorf("AWS config: %w", err)
	}

	if flagRegion != "" {
		return awsCfg, []string{flagRegion}, nil
	}

	regions, err = awspkg.ListRegions(context.Background(), awsCfg)
	if err != nil {
		return awsCfg, nil, fmt.Errorf("list regions: %w", err)
	}
	return awsCfg, regions, nil
}

// printResources outputs resources in the format specified by --output.
func printResources(resources []collector.Resource) error {
	if flagOutput == "json" {
		return printJSON(resources)
	}
	for _, r := range resources {
		fmt.Printf("%-20s  %-15s  %-15s  %s\n",
			r.Type, r.Region, r.RawID, r.DisplayName())
	}
	return nil
}

// printNodes outputs graph nodes in the format specified by --output.
func printNodes(nodes []*graph.Node) error {
	if flagOutput == "json" {
		resources := make([]collector.Resource, len(nodes))
		for i, n := range nodes {
			resources[i] = n.Resource
		}
		return printJSON(resources)
	}
	for _, n := range nodes {
		r := n.Resource
		reasons := ""
		if len(n.OrphanReasons) > 0 {
			reasons = " — " + n.OrphanReasons[0]
		}
		fmt.Printf("%-20s  %-15s  %-15s  %s%s\n",
			r.Type, r.Region, r.RawID, r.DisplayName(), reasons)
	}
	return nil
}

// printJSON marshals v to indented JSON and writes it to stdout.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
