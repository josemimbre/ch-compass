package census

import (
	"context"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// bq is a single backtick, kept as a plain (non-raw) string so it can be
// spliced into the raw-string regex below — Go raw strings can't contain a
// literal backtick themselves, and the pattern needs one to match
// ClickHouse's optional identifier quoting.
const bq = "`"

// mvTargetRegexPattern matches `TO db.table` immediately after a MV's own
// name (allowing only an optional `IF NOT EXISTS` and db-qualifier in
// between). It's anchored there deliberately: an earlier unanchored version
// of this pattern (`MATERIALIZED VIEW.*?TO...`) could skip past MVs with no
// explicit TO (implicit target) and false-match an unrelated `TO word.word`
// substring later in the query body (a comment, a literal), silently
// producing bogus targets and inflated fan-in numbers.
var mvTargetRegexPattern = `MATERIALIZED VIEW\\s+(?:IF NOT EXISTS\\s+)?(?:` + bq + `?[a-zA-Z0-9_]+` + bq + `?\\.)?` + bq + `?[a-zA-Z0-9_]+` + bq + `?\\s+TO\\s+` + bq + `?([a-zA-Z0-9_]+)` + bq + `?\\.` + bq + `?([a-zA-Z0-9_]+)` + bq + `?`

// mvTargetsCTE defines mv_targets(target_database, target_table), one row
// per MV with a resolvable TO target. It ends right after the closing
// parenthesis (no trailing SELECT or comma), so callers can either append a
// SELECT directly (standalone use) or ",\n<another CTE>...\nSELECT ..." to
// chain another CTE after it.
var mvTargetsCTE = `WITH mv_targets AS
(
    SELECT
        extractGroups(create_table_query, '` + mvTargetRegexPattern + `')[1] AS target_database,
        extractGroups(create_table_query, '` + mvTargetRegexPattern + `')[2] AS target_table
    FROM system.tables
    WHERE engine = 'MaterializedView'
      AND database NOT IN ` + systemDatabases + `
)
`

// TotalMaterializedViews returns the total number of materialized views.
func TotalMaterializedViews(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"total_materialized_views"`
	}
	query := `
		SELECT count() AS total_materialized_views
		FROM system.tables
		WHERE engine = 'MaterializedView'
		  AND database NOT IN ` + systemDatabases + `
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "total_materialized_views", Value: float64(rows[0].Count)}, nil
}

// MVTargetCount returns how many MVs have a resolvable TO target.
func MVTargetCount(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"num_mv_targets"`
	}
	query := mvTargetsCTE + `
		SELECT count() AS num_mv_targets
		FROM mv_targets
		WHERE target_table != ''
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "num_mv_targets", Value: float64(rows[0].Count)}, nil
}

// LandingTables returns how many tables receive data via direct INSERT: not
// a materialized view or plain view (neither takes direct inserts), not the
// TO target of any MV, and not an MV's internal `.inner*` storage table
// (implicit target, no explicit TO clause).
func LandingTables(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"num_landing_tables"`
	}
	query := mvTargetsCTE + `
		SELECT count() AS num_landing_tables
		FROM system.tables
		WHERE engine NOT IN ('MaterializedView', 'View')
		  AND database NOT IN ` + systemDatabases + `
		  AND name NOT LIKE '.inner%'
		  AND (database, name) NOT IN (
		      SELECT target_database, target_table
		      FROM mv_targets
		      WHERE target_table != ''
		  )
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "num_landing_tables", Value: float64(rows[0].Count)}, nil
}

// MVsLinkedToLanding returns how many MVs read directly from a landing
// table (as opposed to reading from another MV's output further down the
// chain). dependencies_table on a source table lists the views that
// `SELECT ... FROM` it directly, so summing its length across landing
// tables gives the total.
func MVsLinkedToLanding(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"mvs_linked_to_landing"`
	}
	query := mvTargetsCTE + `,
		landing_tables AS
		(
			SELECT database, name, dependencies_table
			FROM system.tables
			WHERE engine NOT IN ('MaterializedView', 'View')
			  AND database NOT IN ` + systemDatabases + `
			  AND name NOT LIKE '.inner%'
			  AND (database, name) NOT IN (
			      SELECT target_database, target_table
			      FROM mv_targets
			      WHERE target_table != ''
			  )
		)
		SELECT sum(length(dependencies_table)) AS mvs_linked_to_landing
		FROM landing_tables
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "mvs_linked_to_landing", Value: float64(rows[0].Count)}, nil
}

// FanInTargets returns how many target tables are written to by more than
// one MV. A single MV can only have one TO target, but several different
// MVs can point at the same target table (fan-in) — worth flagging since it
// can indicate orphaned MVs from old deploys that were never dropped.
func FanInTargets(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"num_fan_in_targets"`
	}
	query := mvTargetsCTE + `
		SELECT count() AS num_fan_in_targets
		FROM
		(
			SELECT target_database, target_table
			FROM mv_targets
			WHERE target_table != ''
			GROUP BY target_database, target_table
			HAVING count() > 1
		)
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "num_fan_in_targets", Value: float64(rows[0].Count)}, nil
}

// MVSanityCheck returns the distinct MV target count (lower than the raw MV
// count whenever FanInTargets > 0) and the number of plain, non-materialized
// views — useful to reconcile against TotalTables/TotalMaterializedViews.
func MVSanityCheck(ctx context.Context, client *ch.Client) ([]Metric, error) {
	var rows []struct {
		DistinctTargets uint64 `ch:"num_distinct_mv_targets"`
		PlainViews      uint64 `ch:"num_plain_views"`
	}
	query := mvTargetsCTE + `
		SELECT
		    (SELECT count(DISTINCT (target_database, target_table)) FROM mv_targets WHERE target_table != '') AS num_distinct_mv_targets,
		    (SELECT count() FROM system.tables WHERE engine = 'View' AND database NOT IN ` + systemDatabases + `) AS num_plain_views
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return nil, err
	}
	r := rows[0]
	return []Metric{
		{Name: "num_distinct_mv_targets", Value: float64(r.DistinctTargets)},
		{Name: "num_plain_views", Value: float64(r.PlainViews)},
	}, nil
}
