package analyze

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// unknownTableCode is the ClickHouse exception code returned when a system
// table (e.g. query_views_log) does not exist on the server.
const unknownTableCode = 60

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
// trigger activity into a single list. Notes about unavailable system
// tables are written to notes.
func queryPatterns(ctx context.Context, client *ch.Client, database string, days int, notes io.Writer) ([]TableAccess, error) {
	if err := client.Exec(ctx, "-- Force lazy system tables to be created and flush buffered log entries\nSYSTEM FLUSH LOGS"); err != nil {
		return nil, err
	}

	tableAccess, err := collectTableAccess(ctx, client, database, days)
	if err != nil {
		return nil, err
	}

	viewAccess, err := collectRegularViewAccess(ctx, client, database, days)
	if err != nil {
		return nil, err
	}

	mvAccess, err := collectMaterializedViewActivity(ctx, client, database, days, notes)
	if err != nil {
		return nil, err
	}

	return mergeAccess(tableAccess, viewAccess, mvAccess), nil
}

func collectTableAccess(ctx context.Context, client *ch.Client, database string, days int) ([]TableAccess, error) {
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
		return nil, err
	}

	return toTableAccess(rows), nil
}

func collectRegularViewAccess(ctx context.Context, client *ch.Client, database string, days int) ([]TableAccess, error) {
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

	fullNames := make([]string, len(viewRows))
	for i, v := range viewRows {
		fullNames[i] = fmt.Sprintf("%s.%s", database, v.Name)
	}

	var rows []viewAccessRow
	err = client.Select(ctx, &rows, `
		-- Regular view usage: single scan of query_log for all view references
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
						x -> positionCaseInsensitive(query, x) > 0,
						{views:Array(String)}
					)
				) AS matched_view
			FROM system.query_log
			WHERE type = 'QueryFinish'
				AND is_initial_query = 1
				AND event_time >= now() - INTERVAL {days:UInt32} DAY
				AND http_user_agent != {user_agent:String}
				AND query_kind = 'Select'
		)
		GROUP BY matched_view
	`,
		ch.Named("days", days),
		ch.Named("user_agent", ch.UserAgent),
		ch.Named("views", fullNames),
	)
	if err != nil {
		return nil, err
	}

	access := make([]TableAccess, 0, len(rows))
	for _, r := range rows {
		lastAccessed := r.LastAccessed
		access = append(access, TableAccess{
			Name:         shortName(r.Name),
			QueryCount:   r.QueryCount,
			LastAccessed: &lastAccessed,
		})
	}

	return access, nil
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

		fmt.Fprintf(notes, "Note: could not query system.query_views_log: %v\n", err)
		return nil, nil
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
