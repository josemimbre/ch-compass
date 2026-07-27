package report

import (
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/josemimbre/ch-compass/internal/analyze"
)

const htmlStyle = `
*, *::before, *::after { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 0; padding: 2rem; background: #f5f5f5; color: #1a1a1a; line-height: 1.6; }
.container { max-width: 900px; margin: 0 auto; }
h1 { margin-bottom: 0.5rem; }
h2 { margin-top: 2rem; }
.meta { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 1rem; margin-bottom: 2rem; }
.meta-card { background: #fff; border-radius: 8px; padding: 1rem; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.meta-card .label { font-size: 0.8rem; color: #666; text-transform: uppercase; }
.meta-card .value { font-size: 1.2rem; font-weight: 600; }
table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1); margin-bottom: 2rem; }
th { background: #f0f0f0; text-align: left; padding: 0.75rem 1rem; font-size: 0.85rem; text-transform: uppercase; color: #555; }
td { padding: 0.75rem 1rem; border-top: 1px solid #eee; }
.rec { background: #fff; border-radius: 8px; padding: 1.25rem; margin-bottom: 1rem; box-shadow: 0 1px 3px rgba(0,0,0,0.1); border-left: 4px solid #ccc; }
.rec.high { border-left-color: #e53e3e; }
.rec.medium { border-left-color: #d69e2e; }
.rec.low { border-left-color: #a0aec0; }
.rec-header { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; }
.badge { font-size: 0.75rem; font-weight: 700; padding: 0.15rem 0.5rem; border-radius: 4px; color: #fff; }
.badge.high { background: #e53e3e; }
.badge.medium { background: #d69e2e; }
.badge.low { background: #a0aec0; }
.rec-title { font-weight: 600; }
.rec-message { color: #444; margin-bottom: 0.5rem; }
.rec-suggestion { background: #f7fafc; border: 1px solid #e2e8f0; border-radius: 4px; padding: 0.5rem 0.75rem; font-family: monospace; font-size: 0.9rem; white-space: pre-wrap; }
.empty { text-align: center; color: #38a169; padding: 2rem; font-size: 1.1rem; }
`

// WriteHTML renders results as a standalone HTML report to w.
func WriteHTML(w io.Writer, results []analyze.Result, version string, minSeverity *analyze.Severity) error {
	all := analyze.AllRecommendations(results, minSeverity)
	high, medium, low := countBySeverity(all)

	databases := make([]string, len(results))
	var totalTables, totalViews int
	for i, r := range results {
		databases[i] = r.Database
		totalTables += len(r.Tables)
		totalViews += len(r.Views)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>ch-compass Report</title>
<style>%s</style>
</head>
<body>
<div class="container">
<h1>ch-compass Report</h1>
<div class="meta">
<div class="meta-card"><div class="label">ClickHouse</div><div class="value">%s</div></div>
<div class="meta-card"><div class="label">Databases</div><div class="value">%s</div></div>
<div class="meta-card"><div class="label">Tables</div><div class="value">%d</div></div>
<div class="meta-card"><div class="label">Views</div><div class="value">%d</div></div>
<div class="meta-card"><div class="label">Recommendations</div><div class="value">%d</div></div>
<div class="meta-card"><div class="label">Summary</div><div class="value">%d high, %d medium, %d low</div></div>
</div>
`,
		htmlStyle,
		html.EscapeString(version),
		html.EscapeString(strings.Join(databases, ", ")),
		totalTables, totalViews, len(all), high, medium, low,
	)

	multiDB := len(results) > 1
	for _, r := range results {
		writeObjectsHTML(&b, r, multiDB)
	}

	writeRecommendationsHTML(&b, results, all, minSeverity, multiDB)

	b.WriteString("</div>\n</body>\n</html>\n")

	_, err := io.WriteString(w, b.String())
	return err
}

func writeObjectsHTML(b *strings.Builder, r analyze.Result, multiDB bool) {
	if len(r.Tables) == 0 && len(r.Views) == 0 {
		return
	}

	if multiDB {
		fmt.Fprintf(b, "<h2>%s</h2>\n", html.EscapeString(r.Database))
	} else {
		b.WriteString("<h2>Objects</h2>\n")
	}

	b.WriteString("<table>\n<thead><tr><th>Name</th><th>Engine</th><th>Rows</th><th>Size</th><th>Partitions</th></tr></thead>\n<tbody>\n")
	for _, t := range r.Tables {
		fmt.Fprintf(b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td></tr>\n",
			html.EscapeString(t.Name), html.EscapeString(t.Engine), formatNumber(t.TotalRows), formatBytes(t.TotalBytes), t.PartitionCount)
	}
	for _, v := range r.Views {
		fmt.Fprintf(b, "<tr><td>%s</td><td>%s</td><td>-</td><td>-</td><td>-</td></tr>\n",
			html.EscapeString(v.Name), html.EscapeString(v.Engine))
	}
	b.WriteString("</tbody>\n</table>\n")
}

func writeRecommendationsHTML(b *strings.Builder, results []analyze.Result, all []analyze.Recommendation, minSeverity *analyze.Severity, multiDB bool) {
	b.WriteString("<h2>Recommendations</h2>\n")

	if len(all) == 0 {
		b.WriteString(`<div class="empty">&#10003; No recommendations found.</div>` + "\n")
		return
	}

	for _, r := range results {
		recs := analyze.Filter(r.Recommendations, minSeverity)
		if len(recs) == 0 {
			continue
		}

		if multiDB {
			fmt.Fprintf(b, "<h3>%s</h3>\n", html.EscapeString(r.Database))
		}
		for _, rec := range recs {
			sev := string(rec.Severity)
			fmt.Fprintf(b, `<div class="rec %s">
<div class="rec-header"><span class="badge %s">%s</span><span class="rec-title">%s '%s'</span></div>
<div class="rec-message">%s</div>
<div class="rec-suggestion">%s</div>
</div>
`,
				sev, sev, strings.ToUpper(sev),
				html.EscapeString(formatType(rec.Type)), html.EscapeString(rec.Object),
				html.EscapeString(rec.Message),
				html.EscapeString(rec.Suggestion),
			)
		}
	}
}
