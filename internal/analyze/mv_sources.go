package analyze

import (
	"context"
	"fmt"
	"io"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// hasMaterializedView reports whether tables includes at least one
// materialized view, so callers can skip systemSourcedMaterializedViews'
// query entirely when there's nothing for it to find.
func hasMaterializedView(tables []TableInfo) bool {
	for _, t := range tables {
		if t.ViewType == ViewTypeMaterialized {
			return true
		}
	}
	return false
}

// systemSourcedMaterializedViews returns the short names of materialized
// views in database whose source table lives in the system database.
//
// ClickHouse's own system-table writes (system.query_log, system.part_log,
// ...) are done by an internal log flush, not by anything that runs
// through the query pipeline — there's no query_id, no logged query at
// all — so a materialized view built on top of a system table (a common
// pattern for persisting logs ClickHouse would otherwise rotate away)
// never gets an entry in system.query_views_log no matter how often it's
// actually triggered. unusedMaterializedViews excludes these from its
// query_views_log-based check entirely, rather than reporting a
// guaranteed false positive.
//
// dependencies_table on a system.tables row lists the materialized views
// based on that table (the reverse of "which table feeds this view"), so
// this scans system's own rows for dependents in database.
//
// Any failure here degrades gracefully — a note is written to notes and
// an empty result returned — since this is a supplementary signal, not a
// required one: unusedMaterializedViews just loses the extra exclusion
// and falls back to flagging these views like any other.
func systemSourcedMaterializedViews(ctx context.Context, client *ch.Client, database string, notes io.Writer) []string {
	var rows []struct {
		Name string `ch:"mv_name"`
	}
	err := client.Select(ctx, &rows, `
		-- Materialized views sourced from a system.* table: dependencies_table
		-- on a system table's row lists the MVs based on it, so pairing that
		-- up with dependencies_database finds every MV in database whose
		-- source lives in system.
		SELECT tup.2 AS mv_name
		FROM system.tables
		ARRAY JOIN arrayZip(dependencies_database, dependencies_table) AS tup
		WHERE database = 'system'
			AND tup.1 = {database:String}
	`, ch.Named("database", database))
	if err != nil {
		fmt.Fprintf(notes, "Note: could not determine which materialized views are sourced from a system.* table (%v). Unused-materialized-view detection may false-positive on views that persist a system table (e.g. a copy of system.query_log), since their trigger activity never appears in system.query_views_log.\n", err)
		return nil
	}

	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	return names
}
