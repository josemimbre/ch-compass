package analyze

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
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

// FlushLogs flushes ClickHouse's buffered system logs so query-log-based
// detection (table access, unused views, unused materialized views, cold
// tables) reflects recent activity. SYSTEM FLUSH LOGS is a global
// operation, not scoped to a single database, so call this once per run
// before analyzing any database rather than once per database — repeating
// it per database would just redo the same (potentially cluster-wide, via
// client's Cluster) work for no benefit. A missing grant degrades
// gracefully: a note is written to notes and nil is returned, since
// query-log-based detection just becomes less accurate rather than
// impossible.
func FlushLogs(ctx context.Context, client *ch.Client, notes io.Writer) error {
	if err := client.FlushLogs(ctx); err != nil {
		if !degradeOrPropagate(err, notes, "could not flush system logs", "Query-log-based detection may reflect stale data.") {
			return err
		}
	}
	return nil
}

// LogRetention holds how many days of history system.query_log and
// system.query_views_log actually retain, as measured once per run by
// CheckLogRetention. Database uses it to cap the --days window it passes
// to whichever check depends on the corresponding table (table access and
// cold tables on QueryLog; unused views on QueryLog; unused materialized
// views on QueryViewsLog), and to skip that check outright once retention
// is too thin to draw any conclusion from — rather than confidently
// reporting "unused" from what's actually "no history to check".
type LogRetention struct {
	QueryLog      logDaysRetained
	QueryViewsLog logDaysRetained
}

// logDaysRetained is how many days of history one log table retains, or
// unknown when the retention check itself couldn't run (e.g. a missing
// grant) — treated as "don't cap", not as zero retention, since a failed
// check says nothing about actual retention.
type logDaysRetained struct {
	days  int
	known bool
}

// effectiveDays caps requested to how many days are actually retained,
// when that's known and shorter than requested.
func (r logDaysRetained) effectiveDays(requested int) int {
	if r.known && r.days < requested {
		return r.days
	}
	return requested
}

// CheckLogRetention measures how many days of history system.query_log and
// system.query_views_log actually retain, and warns when that's less than
// the requested --days window. Both typically apply their own
// TTL/rotation independent of --days, so a window longer than what's
// actually retained doesn't fail — it just silently looks like "no
// activity", indistinguishable from genuinely no activity. Every check
// that depends on these two tables (table/view access, cold tables,
// unused views/MVs — see queryPatterns) shares this blind spot, so it's
// checked once per run rather than once per check. Retention is a
// server-wide property, not scoped to any one database, so call this once
// per run alongside FlushLogs rather than once per database.
func CheckLogRetention(ctx context.Context, client *ch.Client, days int, notes io.Writer) LogRetention {
	tables := []string{"system.query_log", "system.query_views_log"}

	// Run both tables' queries concurrently, but only write to notes
	// afterward, from this goroutine: notes isn't guaranteed safe for
	// concurrent use (it's a bytes.Buffer in tests and in
	// report.WriteHTML/WriteMarkdown's buffer), and it's the one thing
	// tableRetention's two calls would otherwise race on.
	retained := make([]logDaysRetained, len(tables))
	texts := make([]string, len(tables))
	var wg sync.WaitGroup
	for i, table := range tables {
		wg.Add(1)
		go func(i int, table string) {
			defer wg.Done()
			retained[i], texts[i] = tableRetention(ctx, client, table, days)
		}(i, table)
	}
	wg.Wait()

	for _, text := range texts {
		fmt.Fprint(notes, text)
	}

	return LogRetention{QueryLog: retained[0], QueryViewsLog: retained[1]}
}

// tableRetention measures table's retention in days against days, along
// with the note to print for a shortfall (or "" when there's nothing to
// report — either the query failed or retention is sufficient).
// Deliberately doesn't route failures through degradeOrPropagate: unlike
// collectTableAccess/collectMaterializedViewActivity (which read these
// same tables for data they need and have nothing better to say than
// "unavailable"), a missing or inaccessible table here already gets its
// own, more specific note from whichever collector actually needs it
// (e.g. collectMaterializedViewActivity's dedicated note for a missing
// query_views_log) — this check only adds value on top of a table that's
// actually readable, so any error, known-degradable code or not, leaves
// retention unknown (see logDaysRetained) rather than reported.
func tableRetention(ctx context.Context, client *ch.Client, table string, days int) (logDaysRetained, string) {
	var rows []struct {
		Earliest time.Time `ch:"earliest"`
	}
	err := client.Select(ctx, &rows, `
		-- Retention check: earliest event still retained, to catch a log
		-- whose own TTL/rotation is shorter than the requested --days
		-- window. event_date (not event_time) is both tables' leading
		-- ORDER BY/PARTITION BY column, so ClickHouse can answer min()
		-- from part metadata alone rather than scanning a column.
		SELECT min(event_date) AS earliest
		FROM `+client.AllReplicasSource(table)+`
	`)
	if err != nil || len(rows) == 0 || rows[0].Earliest.IsZero() {
		// A failed query, or an empty table, has no earliest entry to
		// measure retention from — leave it unknown rather than zero.
		return logDaysRetained{}, ""
	}

	retainedDays := int(time.Since(rows[0].Earliest).Hours() / 24)
	return logDaysRetained{days: retainedDays, known: true}, retentionNote(table, retainedDays, days)
}

// retentionNote returns the note to print when retainedDays is less than
// days — meaning the requested window isn't fully backed by data — or ""
// when retention is sufficient.
func retentionNote(table string, retainedDays, days int) string {
	if retainedDays >= days {
		return ""
	}
	return fmt.Sprintf(
		"Note: %s only retains %d day(s) of history, less than the --days %d window requested. Detection that depends on it (table/view access, cold tables, unused views/MVs) is capped to that %d day(s) window, or skipped entirely once there's nothing left to check.\n",
		table, retainedDays, days, retainedDays,
	)
}

// queryPatterns collects query access data for all tables and views in
// database over the trailing days window, merging table access, regular
// view usage, and materialized view trigger activity into a single list.
// Any of these three sources can be unavailable (missing table or
// insufficient grants) without aborting the whole analyze run — a note
// explaining the reduced accuracy is written to notes instead, and that
// source contributes no data.
func queryPatterns(ctx context.Context, client *ch.Client, database string, days int, notes io.Writer) ([]TableAccess, error) {
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
		FROM `+client.AllReplicasSource("system.query_log")+`
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
			FROM `+client.AllReplicasSource("system.query_log")+`
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
		FROM `+client.AllReplicasSource("system.query_views_log")+`
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
