package analyze

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/josemimbre/ch-compass/internal/ch"
)

const (
	// unknownTableCode is the ClickHouse exception code returned when a
	// system table (e.g. query_views_log) does not exist on the server.
	unknownTableCode = 60
	// accessDeniedCode is the ClickHouse exception code returned when the
	// connected user lacks the grant needed to read a system table or run
	// SYSTEM FLUSH LOGS.
	accessDeniedCode = 497
)

// degradable reports whether err represents an optional metric source being
// unavailable — a missing system table or an insufficient grant — rather
// than an unexpected failure. Query-log-derived detection (table access,
// unused views, cold tables) degrades gracefully for these instead of
// aborting the whole analyze run; any other error is still propagated.
func degradable(err error) bool {
	code, ok := ch.ExceptionCode(err)
	return ok && (code == unknownTableCode || code == accessDeniedCode)
}

// degradeOrPropagate reports whether err is degradable. If so, it writes a
// note describing the reduced accuracy to notes and returns true so the
// caller can continue with an empty/partial result instead of failing.
func degradeOrPropagate(err error, notes io.Writer, feature, impact string) bool {
	if !degradable(err) {
		return false
	}
	fmt.Fprintf(notes, "Note: %s (%v). %s\n", feature, err, impact)
	return true
}

type accessRow struct {
	Name           string    `ch:"table_name"`
	QueryCount     uint64    `ch:"query_count"`
	LastAccessed   time.Time `ch:"last_accessed"`
	TotalReadRows  uint64    `ch:"total_read_rows"`
	TotalReadBytes uint64    `ch:"total_read_bytes"`
}

type viewAccessRow struct {
	Name         string    `ch:"matched_view"`
	QueryCount   uint64    `ch:"query_count"`
	LastAccessed time.Time `ch:"last_accessed"`
}

type mvAccessRow struct {
	Name           string    `ch:"view_name"`
	QueryCount     uint64    `ch:"trigger_count"`
	LastAccessed   time.Time `ch:"last_triggered"`
	TotalReadRows  uint64    `ch:"total_read_rows"`
	TotalReadBytes uint64    `ch:"total_read_bytes"`
}

// queryPatterns collects query access data for all tables and views in
// database over the trailing days window. It flushes system logs first,
// then merges table access, regular view usage, and materialized view
// trigger activity into a single list. Any of these three sources can be
// unavailable (missing table or insufficient grants) without aborting the
// whole analyze run — a note explaining the reduced accuracy is written to
// notes instead, and that source contributes no data.
func queryPatterns(ctx context.Context, client *ch.Client, database string, days int, notes io.Writer) ([]TableAccess, error) {
	if err := client.Exec(ctx, "-- Force lazy system tables to be created and flush buffered log entries\nSYSTEM FLUSH LOGS"); err != nil {
		if !degradeOrPropagate(err, notes, "could not flush system logs", "Query-log-based detection may reflect stale data.") {
			return nil, err
		}
	}

	tableAccess, err := collectTableAccess(ctx, client, database, days, notes)
	if err != nil {
		return nil, err
	}

	viewAccess, err := collectRegularViewAccess(ctx, client, database, days, notes)
	if err != nil {
		return nil, err
	}

	mvAccess, err := collectMaterializedViewActivity(ctx, client, database, days, notes)
	if err != nil {
		return nil, err
	}

	return mergeAccess(tableAccess, viewAccess, mvAccess), nil
}

func collectTableAccess(ctx context.Context, client *ch.Client, database string, days int, notes io.Writer) ([]TableAccess, error) {
	var rows []accessRow
	err := client.Select(ctx, &rows, `
		-- Table access: count queries per table from the query log
		-- Excludes views (tracked separately via query text and query_views_log)
		-- Excludes ch-compass's own queries and ClickHouse internal operations
		SELECT
			arrayJoin(tables) AS table_name,
			count() AS query_count,
			max(event_time) AS last_accessed,
			sum(read_rows) AS total_read_rows,
			sum(read_bytes) AS total_read_bytes
		FROM system.query_log
		WHERE type = 'QueryFinish'
			AND is_initial_query = 1
			AND event_time >= now() - INTERVAL {days:UInt32} DAY
			AND has(databases, {database:String})
			AND http_user_agent != {user_agent:String}
		GROUP BY table_name
		HAVING table_name LIKE concat({database:String}, '.%')
			AND table_name NOT IN (
				SELECT concat({database:String}, '.', name)
				FROM system.tables
				WHERE database = {database:String}
					AND engine LIKE '%View%'
			)
	`,
		ch.Named("days", days),
		ch.Named("database", database),
		ch.Named("user_agent", ch.UserAgent),
	)
	if err != nil {
		if degradeOrPropagate(err, notes, "system.query_log is not accessible", "Table access and cold-table detection may be inaccurate.") {
			return nil, nil
		}
		return nil, err
	}

	return toTableAccess(rows), nil
}

func collectRegularViewAccess(ctx context.Context, client *ch.Client, database string, days int, notes io.Writer) ([]TableAccess, error) {
	var viewRows []struct {
		Name string `ch:"name"`
	}
	err := client.Select(ctx, &viewRows, `
		-- List regular views in the database
		SELECT name
		FROM system.tables
		WHERE database = {database:String}
			AND engine = 'View'
	`, ch.Named("database", database))
	if err != nil {
		return nil, err
	}

	if len(viewRows) == 0 {
		return nil, nil
	}

	names := make([]string, len(viewRows))
	patterns := make([]string, len(viewRows))
	for i, v := range viewRows {
		names[i] = v.Name
		patterns[i] = viewSearchPattern(database, v.Name)
	}

	var rows []viewAccessRow
	err = client.Select(ctx, &rows, `
		-- Regular view usage: single scan of query_log for all view references.
		-- Matches both qualified ("database.view", from any session) and bare
		-- ("view", e.g. after "USE database") references via a word-boundary
		-- regex. Bare references are only counted for queries that actually
		-- touched this database (has(databases, ...)) to avoid conflating
		-- same-named views across unrelated databases.
		-- Excludes ch-compass's own queries and ClickHouse internal operations
		SELECT
			matched_view,
			count() AS query_count,
			max(event_time) AS last_accessed
		FROM (
			SELECT
				event_time,
				arrayJoin(
					arrayFilter(
						(name, pattern) -> match(query, pattern),
						{names:Array(String)},
						{patterns:Array(String)}
					)
				) AS matched_view
			FROM system.query_log
			WHERE type = 'QueryFinish'
				AND is_initial_query = 1
				AND event_time >= now() - INTERVAL {days:UInt32} DAY
				AND http_user_agent != {user_agent:String}
				AND query_kind = 'Select'
				AND has(databases, {database:String})
		)
		GROUP BY matched_view
	`,
		ch.Named("days", days),
		ch.Named("user_agent", ch.UserAgent),
		ch.Named("database", database),
		ch.Named("names", names),
		ch.Named("patterns", patterns),
	)
	if err != nil {
		if degradeOrPropagate(err, notes, "system.query_log is not accessible", "Unused-view detection may be inaccurate.") {
			return nil, nil
		}
		return nil, err
	}

	access := make([]TableAccess, 0, len(rows))
	for _, r := range rows {
		lastAccessed := r.LastAccessed
		access = append(access, TableAccess{
			Name:         r.Name,
			QueryCount:   r.QueryCount,
			LastAccessed: &lastAccessed,
		})
	}

	return access, nil
}

// viewSearchPattern builds a case-insensitive, word-boundary regex matching
// either the bare view name or "database.view", so both fully-qualified
// references and unqualified ones (e.g. after "USE database") count as
// activity.
func viewSearchPattern(database, view string) string {
	qualifiedPrefix := regexp.QuoteMeta(database) + `\.`
	return `(?i)\b(?:` + qualifiedPrefix + `)?` + regexp.QuoteMeta(view) + `\b`
}

func collectMaterializedViewActivity(ctx context.Context, client *ch.Client, database string, days int, notes io.Writer) ([]TableAccess, error) {
	var rows []mvAccessRow
	err := client.Select(ctx, &rows, `
		-- Materialized view activity: count insert-triggered executions
		SELECT
			view_name,
			count() AS trigger_count,
			max(event_time) AS last_triggered,
			sum(read_rows) AS total_read_rows,
			sum(read_bytes) AS total_read_bytes
		FROM system.query_views_log
		WHERE status = 'QueryFinish'
			AND event_time >= now() - INTERVAL {days:UInt32} DAY
			AND view_name LIKE concat({database:String}, '.%')
		GROUP BY view_name
	`,
		ch.Named("days", days),
		ch.Named("database", database),
	)
	if err != nil {
		if code, ok := ch.ExceptionCode(err); ok && code == unknownTableCode {
			fmt.Fprintln(notes, `Note: system.query_views_log is not available. Unused materialized view detection may be inaccurate.
  To enable it, add to your ClickHouse server config:
    <query_views_log>
        <database>system</database>
        <table>query_views_log</table>
    </query_views_log>
  And set log_query_views=1 for the user profile.`)
			return nil, nil
		}
		if degradeOrPropagate(err, notes, "system.query_views_log is not accessible", "Unused-materialized-view detection may be inaccurate.") {
			return nil, nil
		}

		return nil, err
	}

	access := make([]TableAccess, 0, len(rows))
	for _, r := range rows {
		access = append(access, TableAccess{
			Name:           shortName(r.Name),
			QueryCount:     r.QueryCount,
			LastAccessed:   timePtr(r.LastAccessed),
			TotalReadRows:  r.TotalReadRows,
			TotalReadBytes: r.TotalReadBytes,
		})
	}

	return access, nil
}

func toTableAccess(rows []accessRow) []TableAccess {
	access := make([]TableAccess, 0, len(rows))
	for _, r := range rows {
		access = append(access, TableAccess{
			Name:           shortName(r.Name),
			QueryCount:     r.QueryCount,
			LastAccessed:   timePtr(r.LastAccessed),
			TotalReadRows:  r.TotalReadRows,
			TotalReadBytes: r.TotalReadBytes,
		})
	}
	return access
}

func mergeAccess(groups ...[]TableAccess) []TableAccess {
	merged := make(map[string]TableAccess)

	for _, group := range groups {
		for _, a := range group {
			existing, ok := merged[a.Name]
			if !ok {
				merged[a.Name] = a
				continue
			}

			existing.QueryCount += a.QueryCount
			existing.TotalReadRows += a.TotalReadRows
			existing.TotalReadBytes += a.TotalReadBytes
			if a.LastAccessed != nil && (existing.LastAccessed == nil || a.LastAccessed.After(*existing.LastAccessed)) {
				existing.LastAccessed = a.LastAccessed
			}
			merged[a.Name] = existing
		}
	}

	result := make([]TableAccess, 0, len(merged))
	for _, a := range merged {
		result = append(result, a)
	}
	return result
}

func shortName(fullName string) string {
	parts := strings.Split(fullName, ".")
	return parts[len(parts)-1]
}

func timePtr(t time.Time) *time.Time {
	return &t
}
