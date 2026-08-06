package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/josemimbre/ch-compass/internal/analyze"
	"github.com/josemimbre/ch-compass/internal/ch"
	"github.com/josemimbre/ch-compass/internal/report"
)

// errAnalyzeFailed signals a non-zero exit code from runAnalyze. Its message
// is never shown (see the cmd.SilenceErrors usage below); main.go only cares
// that Execute() returned a non-nil error.
var errAnalyzeFailed = errors.New("analyze failed")

func newAnalyzeCmd() *cobra.Command {
	var (
		host, database, username, password string
		output, cluster                    string
		port                               int
		format, severity                   string
		days                               int
		debug, quiet, secure, allDatabases bool
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze a ClickHouse database and generate recommendations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "text", "json", "html", "md":
			default:
				return fmt.Errorf("invalid format %q, must be text, json, html, or md", format)
			}
			if allDatabases && database != "" {
				return fmt.Errorf("--database and --all-databases are mutually exclusive")
			}
			if !allDatabases && database == "" {
				return fmt.Errorf(`required flag "database" not set (or pass --all-databases)`)
			}

			var minSeverity *analyze.Severity
			if severity != "" {
				sev, err := analyze.ParseSeverity(severity)
				if err != nil {
					return err
				}
				minSeverity = &sev
			}

			var databases []string
			if !allDatabases {
				databases = splitTrimmed(database)
			}
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()

			if code := runAnalyze(cmd.Context(), stdout, stderr, analyzeOptions{
				host:         host,
				port:         port,
				databases:    databases,
				allDatabases: allDatabases,
				username:     username,
				password:     password,
				secure:       secure,
				debug:        debug,
				quiet:        quiet,
				days:         days,
				cluster:      cluster,
				format:       format,
				output:       output,
				minSeverity:  minSeverity,
			}); code != 0 {
				// runAnalyze already reported the failure (or the found
				// recommendations) to stderr/stdout itself; suppress
				// cobra's own "Error: ..." line and just signal failure.
				cmd.SilenceErrors = true
				return errAnalyzeFailed
			}
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&host, "host", "localhost", "ClickHouse host")
	flags.IntVarP(&port, "port", "p", 8123, "ClickHouse HTTP port")
	flags.StringVarP(&database, "database", "d", "", "Database name(s) to analyze (comma-separated for multiple)")
	flags.BoolVar(&allDatabases, "all-databases", false, "Analyze every database on the server (excluding system schemas)")
	flags.StringVarP(&username, "user", "u", "", "ClickHouse username")
	flags.StringVar(&password, "password", "", "ClickHouse password")
	flags.StringVarP(&format, "format", "f", "text", "Output format: text, json, html, or md")
	flags.StringVarP(&output, "output", "o", "", "Write output to file instead of stdout (useful with html/md formats)")
	flags.StringVarP(&severity, "severity", "s", "", "Minimum severity to show: low, medium, or high")
	flags.IntVar(&days, "days", 30, "Analysis window in days")
	flags.BoolVar(&debug, "debug", false, "Print SQL queries as they are executed")
	flags.BoolVarP(&quiet, "quiet", "q", false, "Only output recommendations, no connection info or table listing")
	flags.BoolVar(&secure, "secure", false, "Use HTTPS for ClickHouse connection (TLS)")
	flags.StringVar(&cluster, "cluster", "", "ClickHouse cluster name: widens query-log-based detection (table access, unused views/MVs) across every host in the cluster instead of just the connected node")

	return cmd
}

type analyzeOptions struct {
	host, username, password string
	output, cluster          string
	port                     int
	databases                []string
	allDatabases             bool
	secure, debug, quiet     bool
	days                     int
	format                   string
	minSeverity              *analyze.Severity
}

// runAnalyze connects to ClickHouse, analyzes every configured database,
// renders the result, and returns the process exit code: 0 when no
// recommendations survive the severity filter, 1 otherwise or on error.
func runAnalyze(ctx context.Context, stdout, stderr io.Writer, opts analyzeOptions) int {
	if opts.format == "text" && !opts.quiet {
		fmt.Fprintf(stdout, "Connecting to %s:%d...\n", opts.host, opts.port)
	}

	connectDatabase := "default"
	if !opts.allDatabases {
		connectDatabase = opts.databases[0]
	}

	client, err := ch.Connect(ctx, ch.Options{
		Host:     opts.host,
		Port:     opts.port,
		Database: connectDatabase,
		Username: opts.username,
		Password: opts.password,
		Secure:   opts.secure,
		Debug:    opts.debug,
		Cluster:  opts.cluster,
	}, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: could not connect to ClickHouse - %v\n", err)
		return 1
	}
	defer client.Close()

	version, err := client.Version(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Error: could not query ClickHouse - %v\n", err)
		return 1
	}

	// Flush once for the whole run rather than per database: it's a
	// global operation (potentially cluster-wide, via opts.cluster), so
	// repeating it per database would just redo the same work.
	if err := analyze.FlushLogs(ctx, client, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: could not flush system logs - %v\n", err)
		return 1
	}

	// Same reasoning as the flush above: retention is a server-wide
	// property of query_log/query_views_log, not scoped to any one
	// database, so check it once rather than once per database.
	analyze.CheckLogRetention(ctx, client, opts.days, stdout)

	databases := opts.databases
	if opts.allDatabases {
		databases, err = analyze.ListDatabases(ctx, client)
		if err != nil {
			fmt.Fprintf(stderr, "Error: could not list databases - %v\n", err)
			return 1
		}
	}

	results := make([]analyze.Result, len(databases))
	for i, database := range databases {
		if opts.format == "text" && !opts.quiet {
			fmt.Fprintf(stdout, "Analyzing database '%s'...\n", database)
		}

		result, err := analyze.Database(ctx, client, database, opts.days, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "Error: could not analyze database '%s' - %v\n", database, err)
			return 1
		}
		results[i] = result
	}

	switch opts.format {
	case "json":
		if err := report.WriteJSON(stdout, results, version, opts.minSeverity); err != nil {
			fmt.Fprintf(stderr, "Error: could not write JSON report - %v\n", err)
			return 1
		}
	case "html", "md":
		var buf strings.Builder
		if opts.format == "html" {
			err = report.WriteHTML(&buf, results, version, opts.minSeverity)
		} else {
			err = report.WriteMarkdown(&buf, results, version, opts.minSeverity)
		}
		if err != nil {
			fmt.Fprintf(stderr, "Error: could not render %s report - %v\n", opts.format, err)
			return 1
		}

		if opts.output == "" {
			fmt.Fprint(stdout, buf.String())
		} else if err := os.WriteFile(opts.output, []byte(buf.String()), 0o644); err != nil {
			fmt.Fprintf(stderr, "Error writing report: %v\n", err)
			return 1
		} else {
			fmt.Fprintf(stdout, "Report written to %s\n", opts.output)
		}
	default:
		report.WriteText(stdout, results, version, opts.minSeverity, opts.quiet)
	}

	if len(analyze.AllRecommendations(results, opts.minSeverity)) > 0 {
		return 1
	}
	return 0
}

func splitTrimmed(s string) []string {
	parts := strings.Split(s, ",")
	trimmed := make([]string, len(parts))
	for i, p := range parts {
		trimmed[i] = strings.TrimSpace(p)
	}
	return trimmed
}
