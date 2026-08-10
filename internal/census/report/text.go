// Package report renders collected census metrics for the terminal and for
// machine-readable consumption.
package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/josemimbre/ch-compass/internal/census"
	"github.com/josemimbre/ch-compass/internal/termtable"
)

// friendlyNames gives each scalar metric a human-readable label for text
// output; metrics not listed here fall back to their raw name.
var friendlyNames = map[string]string{
	"total_databases":                 "Total databases",
	"num_empty_databases":             "Databases with no tables",
	"num_databases_all_empty":         "Databases with tables, but all empty",
	"max_tables_per_db":               "Max tables in one database",
	"min_tables_per_db":               "Min tables in one database",
	"total_tables":                    "Total tables",
	"num_empty_tables":                "Empty tables (total_rows = 0)",
	"num_tables_zero_parts":           "  - with zero parts (never inserted into)",
	"num_tables_zero_rows_with_parts": "  - with parts, but zero rows (fully deleted)",
	"num_empty_tables_by_parts":       "  - cross-checked via system.parts",
	"total_materialized_views":        "Total materialized views",
	"num_mv_targets":                  "MVs with an explicit TO target",
	"num_distinct_mv_targets":         "Distinct MV target tables",
	"num_plain_views":                 "Plain (non-materialized) views",
	"num_landing_tables":              "Landing tables (direct inserts)",
	"mvs_linked_to_landing":           "MVs reading directly from a landing table",
	"num_fan_in_targets":              "Target tables written to by >1 MV (fan-in)",
}

// section groups related scalar metrics under one heading, optionally
// followed by a breakdown sub-table (a metric with one row per Labels entry).
type section struct {
	title             string
	scalars           []string
	breakdownMetric   string
	breakdownLabelKey string // Labels key to read for each breakdown row, e.g. "engine"
	breakdownColumn   string // header for that column
	breakdownValueCol string // header for the Value column of the breakdown
}

var sections = []section{
	{
		title:             "Databases",
		scalars:           []string{"total_databases", "num_empty_databases", "num_databases_all_empty", "max_tables_per_db", "min_tables_per_db"},
		breakdownMetric:   "database_count_by_engine",
		breakdownLabelKey: "engine",
		breakdownColumn:   "Engine",
		breakdownValueCol: "Databases",
	},
	{
		title:             "Tables",
		scalars:           []string{"total_tables"},
		breakdownMetric:   "table_count_by_engine",
		breakdownLabelKey: "engine",
		breakdownColumn:   "Engine",
		breakdownValueCol: "Tables",
	},
	{
		title: "Materialized views",
		scalars: []string{
			"total_materialized_views",
			"num_mv_targets",
			"num_distinct_mv_targets",
			"num_plain_views",
			"num_landing_tables",
			"mvs_linked_to_landing",
			"num_fan_in_targets",
		},
	},
	{
		title:   "Empty tables",
		scalars: []string{"num_empty_tables", "num_tables_zero_parts", "num_tables_zero_rows_with_parts", "num_empty_tables_by_parts"},
	},
}

// WriteText renders metrics as a series of bordered tables, one per logical
// section (databases, tables, materialized views, empty tables), followed
// by a per-replica table if replica metrics were collected.
func WriteText(w io.Writer, metrics []census.Metric) {
	byName := indexByName(metrics)

	for _, s := range sections {
		writeSection(w, s, byName)
	}

	writeReplicas(w, metrics)
}

// indexByName groups metrics by name, preserving each name's row order
// (scalars have exactly one row with no Labels; breakdowns have one row per
// distinct Labels value).
func indexByName(metrics []census.Metric) map[string][]census.Metric {
	byName := make(map[string][]census.Metric)
	for _, m := range metrics {
		byName[m.Name] = append(byName[m.Name], m)
	}
	return byName
}

func writeSection(w io.Writer, s section, byName map[string][]census.Metric) {
	var scalarRows [][]string
	for _, name := range s.scalars {
		rows, ok := byName[name]
		if !ok || len(rows) == 0 {
			continue
		}
		scalarRows = append(scalarRows, []string{displayName(name), formatValue(rows[0].Value)})
	}

	breakdownRows, hasBreakdown := byName[s.breakdownMetric]
	if len(scalarRows) == 0 && !hasBreakdown {
		return
	}

	fmt.Fprintln(w, termtable.Bold.Render(s.title))

	if len(scalarRows) > 0 {
		fmt.Fprintln(w, termtable.Render([]string{"Metric", "Value"}, scalarRows))
	}

	if hasBreakdown {
		sort.Slice(breakdownRows, func(i, j int) bool { return breakdownRows[i].Value > breakdownRows[j].Value })
		rows := make([][]string, len(breakdownRows))
		for i, r := range breakdownRows {
			rows[i] = []string{r.Labels[s.breakdownLabelKey], formatValue(r.Value)}
		}
		fmt.Fprintln(w, termtable.Faint.Render(fmt.Sprintf("By %s:", s.breakdownColumn)))
		fmt.Fprintln(w, termtable.Render([]string{s.breakdownColumn, s.breakdownValueCol}, rows))
	}

	fmt.Fprintln(w)
}

// writeReplicas pivots replica_table_count/replica_num_mvs/replica_num_views
// (all keyed by the same replica hostname in Labels["replica"]) into one row
// per replica, which reads far better than three separate flat lists when
// spotting desync between replicas.
func writeReplicas(w io.Writer, metrics []census.Metric) {
	type replicaRow struct {
		tables, mvs, views float64
	}
	byReplica := make(map[string]*replicaRow)
	var order []string

	for _, m := range metrics {
		switch m.Name {
		case "replica_table_count", "replica_num_mvs", "replica_num_views":
		default:
			continue
		}

		replica := m.Labels["replica"]
		r, ok := byReplica[replica]
		if !ok {
			r = &replicaRow{}
			byReplica[replica] = r
			order = append(order, replica)
		}
		switch m.Name {
		case "replica_table_count":
			r.tables = m.Value
		case "replica_num_mvs":
			r.mvs = m.Value
		case "replica_num_views":
			r.views = m.Value
		}
	}
	if len(order) == 0 {
		return
	}
	sort.Strings(order)

	rows := make([][]string, len(order))
	for i, replica := range order {
		r := byReplica[replica]
		rows[i] = []string{replica, formatValue(r.tables), formatValue(r.mvs), formatValue(r.views)}
	}

	fmt.Fprintln(w, termtable.Bold.Render("Replicas"))
	fmt.Fprintln(w, termtable.Render([]string{"Replica", "Tables", "MVs", "Views"}, rows))
}

func displayName(metric string) string {
	if name, ok := friendlyNames[metric]; ok {
		return name
	}
	return metric
}

func formatValue(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}
