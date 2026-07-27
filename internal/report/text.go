package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/josemimbre/ch-compass/internal/analyze"
)

var (
	faintStyle  = lipgloss.NewStyle().Faint(true)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	cyanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	borderColor = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

var severityColors = map[analyze.Severity]lipgloss.Color{
	analyze.SeverityHigh:   lipgloss.Color("#e53e3e"),
	analyze.SeverityMedium: lipgloss.Color("#d69e2e"),
	analyze.SeverityLow:    lipgloss.Color("#a0aec0"),
}

var typeLabels = map[analyze.RecType]string{
	analyze.TypeOverPartitioned:        "Over-partitioned table",
	analyze.TypeColdTable:              "Cold table",
	analyze.TypeStuckMutation:          "Stuck mutation on",
	analyze.TypeUnusedView:             "Unused view",
	analyze.TypeUnusedMaterializedView: "Unused materialized view",
	analyze.TypeDuplicateIndex:         "Redundant skip index",
}

// WriteText renders results to w as colored terminal output: an objects
// table per database (unless quiet), followed by boxed recommendations
// grouped by database when there's more than one.
func WriteText(w io.Writer, results []analyze.Result, version string, minSeverity *analyze.Severity, quiet bool) {
	if !quiet {
		fmt.Fprintln(w, faintStyle.Render(fmt.Sprintf("ClickHouse %s", version)))
		fmt.Fprintln(w)

		for _, r := range results {
			if len(results) > 1 {
				fmt.Fprintln(w, boldStyle.Render(fmt.Sprintf("Database: %s", r.Database)))
			}
			writeObjectsTable(w, r.Tables, r.Views)
		}
	}

	all := analyze.AllRecommendations(results, minSeverity)
	writeRecommendations(w, results, all, minSeverity)
}

func writeObjectsTable(w io.Writer, tables, views []analyze.TableInfo) {
	if len(tables) == 0 && len(views) == 0 {
		return
	}

	rows := make([][]string, 0, len(tables)+len(views))
	for _, t := range tables {
		rows = append(rows, []string{
			t.Name, t.Engine,
			formatNumber(t.TotalRows), formatBytes(t.TotalBytes),
			fmt.Sprintf("%d", t.PartitionCount),
		})
	}
	for _, v := range views {
		rows = append(rows, []string{v.Name, v.Engine, "-", "-", "-"})
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderColor).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return style.Bold(true)
			}
			return style
		}).
		Headers("Name", "Engine", "Rows", "Size", "Partitions").
		Rows(rows...)

	fmt.Fprintln(w, t.Render())
	fmt.Fprintln(w)
}

func writeRecommendations(w io.Writer, results []analyze.Result, all []analyze.Recommendation, minSeverity *analyze.Severity) {
	if len(all) == 0 {
		fmt.Fprintln(w, greenStyle.Render("✓ No recommendations found."))
		return
	}

	multiDB := len(results) > 1
	for _, r := range results {
		recs := analyze.Filter(r.Recommendations, minSeverity)
		if len(recs) == 0 {
			continue
		}

		if multiDB {
			fmt.Fprintln(w, boldStyle.Render(fmt.Sprintf("Database: %s", r.Database)))
		}
		for _, rec := range recs {
			writeRecommendation(w, rec)
		}
		fmt.Fprintln(w)
	}

	writeSummary(w, all)
}

func writeRecommendation(w io.Writer, rec analyze.Recommendation) {
	header := fmt.Sprintf("%s %s", severityBadge(rec.Severity), boldStyle.Render(fmt.Sprintf("%s '%s'", formatType(rec.Type), rec.Object)))
	body := fmt.Sprintf("%s\n%s", rec.Message, cyanStyle.Render("→ "+rec.Suggestion))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(severityColors[rec.Severity]).
		Padding(0, 1)

	fmt.Fprintln(w, box.Render(header+"\n\n"+body))
}

func writeSummary(w io.Writer, recs []analyze.Recommendation) {
	high, medium, low := countBySeverity(recs)

	var parts []string
	if high > 0 {
		parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render(fmt.Sprintf("%d high", high)))
	}
	if medium > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(fmt.Sprintf("%d medium", medium)))
	}
	if low > 0 {
		parts = append(parts, faintStyle.Render(fmt.Sprintf("%d low", low)))
	}

	summary := boldStyle.Render(fmt.Sprintf("%d recommendations", len(recs)))
	if len(parts) > 0 {
		joined := parts[0]
		for _, p := range parts[1:] {
			joined += ", " + p
		}
		summary += fmt.Sprintf(" (%s)", joined)
	}

	fmt.Fprintln(w, summary)
}

func severityBadge(sev analyze.Severity) string {
	color := severityColors[sev]

	style := lipgloss.NewStyle().Bold(true)
	if sev == analyze.SeverityMedium {
		style = style.Foreground(lipgloss.Color("0")).Background(color)
	} else {
		style = style.Foreground(lipgloss.Color("15")).Background(color)
	}

	return style.Render(fmt.Sprintf(" %s ", strings.ToUpper(string(sev))))
}

func formatNumber(n *uint64) string {
	if n == nil {
		return "0"
	}
	return fmt.Sprintf("%d", *n)
}

func formatBytes(bytes *uint64) string {
	if bytes == nil {
		return "0 B"
	}

	b := float64(*bytes)
	switch {
	case *bytes < 1_024:
		return fmt.Sprintf("%d B", *bytes)
	case *bytes < 1_048_576:
		return fmt.Sprintf("%.1f KB", b/1_024)
	case *bytes < 1_073_741_824:
		return fmt.Sprintf("%.1f MB", b/1_048_576)
	default:
		return fmt.Sprintf("%.1f GB", b/1_073_741_824)
	}
}

func countBySeverity(recs []analyze.Recommendation) (high, medium, low int) {
	for _, r := range recs {
		switch r.Severity {
		case analyze.SeverityHigh:
			high++
		case analyze.SeverityMedium:
			medium++
		case analyze.SeverityLow:
			low++
		}
	}
	return
}

func formatType(t analyze.RecType) string {
	if label, ok := typeLabels[t]; ok {
		return label
	}

	s := strings.ReplaceAll(string(t), "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
