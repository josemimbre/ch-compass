package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/josemimbre/ch-compass/internal/census"
	"github.com/josemimbre/ch-compass/internal/census/report"
	"github.com/josemimbre/ch-compass/internal/ch"
)

// errCensusFailed signals a non-zero exit code from runCensus. Its message
// is never shown (see the cmd.SilenceErrors usage below); main.go only
// cares that Execute() returned a non-nil error.
var errCensusFailed = errors.New("census failed")

func newCensusCmd() *cobra.Command {
	var (
		conn        connectionFlags
		clusterName string
		output      string
		format      string
	)

	cmd := &cobra.Command{
		Use:   "census",
		Short: "Collect cluster-wide inventory metrics from a ClickHouse server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "text", "json", "csv":
			default:
				return fmt.Errorf("invalid format %q, must be text, json, or csv", format)
			}

			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()

			code := runCensus(cmd.Context(), stdout, stderr, censusOptions{
				connectionFlags: conn,
				clusterName:     clusterName,
				format:          format,
				output:          output,
			})
			if code != 0 {
				cmd.SilenceErrors = true
				return errCensusFailed
			}
			return nil
		},
	}

	conn.register(cmd)
	flags := cmd.Flags()
	flags.StringVar(&clusterName, "cluster", "", "ClickHouse cluster name (system.clusters) to enable per-replica desync metrics; leave empty to skip them")
	flags.StringVarP(&format, "format", "f", "text", "Output format: text, json, or csv")
	flags.StringVarP(&output, "output", "o", "", "Write output to file instead of stdout")

	return cmd
}

type censusOptions struct {
	connectionFlags
	clusterName string
	output      string
	format      string
}

// runCensus connects to ClickHouse, collects every metric, renders the
// result, and returns the process exit code: 0 on success, 1 on error.
func runCensus(ctx context.Context, stdout, stderr io.Writer, opts censusOptions) int {
	fmt.Fprintf(stderr, "Connecting to %s:%d...\n", opts.Host, opts.Port)

	client, err := ch.Connect(ctx, ch.Options{
		Host:     opts.Host,
		Port:     opts.Port,
		Username: opts.Username,
		Password: opts.Password,
		Secure:   opts.Secure,
		Debug:    opts.Debug,
		Cluster:  opts.clusterName,
	}, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: could not connect to ClickHouse - %v\n", err)
		return 1
	}
	defer client.Close()

	metrics, err := census.Collect(ctx, client, census.Options{ClusterName: opts.clusterName})
	if err != nil {
		fmt.Fprintf(stderr, "Error: could not collect metrics - %v\n", err)
		return 1
	}

	w := stdout
	if opts.output != "" {
		f, err := os.Create(opts.output)
		if err != nil {
			fmt.Fprintf(stderr, "Error: could not create output file - %v\n", err)
			return 1
		}
		defer f.Close()
		w = f
	}

	switch opts.format {
	case "json":
		err = report.WriteJSON(w, metrics)
	case "csv":
		err = report.WriteCSV(w, metrics)
	default:
		report.WriteText(w, metrics)
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: could not write %s report - %v\n", opts.format, err)
		return 1
	}

	if opts.output != "" {
		fmt.Fprintf(stderr, "Report written to %s\n", opts.output)
	}
	return 0
}
