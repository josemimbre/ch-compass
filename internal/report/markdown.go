package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/josemimbre/ch-compass/internal/analyze"
)

// WriteMarkdown renders results as a standalone Markdown report to w.
func WriteMarkdown(w io.Writer, results []analyze.Result, version string, minSeverity *analyze.Severity) error {
	var b strings.Builder

	all := analyze.AllRecommendations(results, minSeverity)
	high, medium, low := countBySeverity(all)

	databases := make([]string, len(results))
	var totalTables, totalViews int
	for i, r := range results {
		databases[i] = r.Database
		totalTables += len(r.Tables)
		totalViews += len(r.Views)
	}

	fmt.Fprintf(&b, "# ch-compass Report\n\n")
	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| **ClickHouse** | %s |\n", version)
	fmt.Fprintf(&b, "| **Databases** | %s |\n", strings.Join(databases, ", "))
	fmt.Fprintf(&b, "| **Tables** | %d |\n", totalTables)
	fmt.Fprintf(&b, "| **Views** | %d |\n", totalViews)
	fmt.Fprintf(&b, "| **Recommendations** | %d (%d high, %d medium, %d low) |\n\n", len(all), high, medium, low)

	multiDB := len(results) > 1
	for _, r := range results {
		writeObjectsMarkdown(&b, r, multiDB)
	}

	writeRecommendationsMarkdown(&b, results, all, minSeverity, multiDB)

	_, err := io.WriteString(w, b.String())
	return err
}

func writeObjectsMarkdown(b *strings.Builder, r analyze.Result, multiDB bool) {
	if len(r.Tables) == 0 && len(r.Views) == 0 {
		return
	}

	if multiDB {
		fmt.Fprintf(b, "## %s\n\n", r.Database)
	} else {
		b.WriteString("## Objects\n\n")
	}

	b.WriteString("| Name | Engine | Rows | Size | Partitions |\n|------|--------|------|------|------------|\n")
	for _, t := range r.Tables {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %d |\n", t.Name, t.Engine, formatNumber(t.TotalRows), formatBytes(t.TotalBytes), t.PartitionCount)
	}
	for _, v := range r.Views {
		fmt.Fprintf(b, "| %s | %s | - | - | - |\n", v.Name, v.Engine)
	}
	b.WriteString("\n")
}

func writeRecommendationsMarkdown(b *strings.Builder, results []analyze.Result, all []analyze.Recommendation, minSeverity *analyze.Severity, multiDB bool) {
	b.WriteString("## Recommendations\n\n")

	if len(all) == 0 {
		b.WriteString("> No recommendations found.\n")
		return
	}

	for _, r := range results {
		recs := analyze.Filter(r.Recommendations, minSeverity)
		if len(recs) == 0 {
			continue
		}

		if multiDB {
			fmt.Fprintf(b, "### %s\n\n", r.Database)
		}
		for _, rec := range recs {
			fmt.Fprintf(b, "#### [%s] %s '%s'\n\n", strings.ToUpper(string(rec.Severity)), formatType(rec.Type), rec.Object)
			fmt.Fprintf(b, "%s\n\n", rec.Message)
			fmt.Fprintf(b, "```sql\n%s\n```\n\n", rec.Suggestion)
		}
	}
}
