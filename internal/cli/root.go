// Package cli wires up the ch-compass command tree.
package cli

import (
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X ...cli.version=x.y.z".
var version = "dev"

// NewRootCmd builds the root command and attaches all subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ch-compass",
		Short:         "ClickHouse optimization recommendation engine and cluster census tool",
		Long:          "ch-compass analyzes a ClickHouse database and recommends optimizations (unused capacity, over-partitioned tables, stuck mutations, and more) via `analyze`, and collects cluster-wide inventory metrics — database/table counts, materialized view topology, replica desync — via `census`.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newAnalyzeCmd())
	root.AddCommand(newCensusCmd())

	return root
}
