package census

import (
	"context"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// TotalTables returns the total number of tables across all databases.
func TotalTables(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"total_tables"`
	}
	query := `
		SELECT count() AS total_tables
		FROM system.tables
		WHERE database NOT IN ` + systemDatabases + `
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "total_tables", Value: float64(rows[0].Count)}, nil
}

// TableCountByEngine breaks down table count per engine.
func TableCountByEngine(ctx context.Context, client *ch.Client) ([]Metric, error) {
	var rows []struct {
		Engine string `ch:"engine"`
		Count  uint64 `ch:"num_tables"`
	}
	query := `
		SELECT engine, count() AS num_tables
		FROM system.tables
		WHERE database NOT IN ` + systemDatabases + `
		GROUP BY engine
		ORDER BY num_tables DESC
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return nil, err
	}
	metrics := make([]Metric, len(rows))
	for i, r := range rows {
		metrics[i] = Metric{Name: "table_count_by_engine", Labels: map[string]string{"engine": r.Engine}, Value: float64(r.Count)}
	}
	return metrics, nil
}

// EmptyTables returns how many tables have zero rows, based on the
// (normally reliable, but cached) system.tables.total_rows column. See
// EmptyTablesByParts for an independent cross-check against system.parts.
func EmptyTables(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"num_empty_tables"`
	}
	query := `
		SELECT count() AS num_empty_tables
		FROM system.tables
		WHERE database NOT IN ` + systemDatabases + `
		  AND engine NOT IN ` + nonStorageEngines + `
		  AND total_rows = 0
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "num_empty_tables", Value: float64(rows[0].Count)}, nil
}

// EmptyTablesByParts cross-checks EmptyTables independently via
// system.parts instead of the cached total_rows column, and distinguishes
// two flavors of "empty": tables with zero active parts at all (never
// received an insert) vs. tables that have parts but those parts sum to
// zero rows (e.g. everything in them was deleted).
func EmptyTablesByParts(ctx context.Context, client *ch.Client) ([]Metric, error) {
	var rows []struct {
		ZeroParts     uint64 `ch:"num_tables_zero_parts"`
		ZeroRowsParts uint64 `ch:"num_tables_zero_rows_with_parts"`
		Total         uint64 `ch:"num_empty_tables_by_parts"`
	}
	query := `
		WITH part_stats AS
		(
			SELECT
				database,
				table,
				count() AS num_active_parts,
				sum(rows) AS rows_from_parts
			FROM system.parts
			WHERE active
			GROUP BY database, table
		)
		SELECT
			countIf(p.num_active_parts IS NULL OR p.num_active_parts = 0) AS num_tables_zero_parts,
			countIf(p.num_active_parts > 0 AND p.rows_from_parts = 0) AS num_tables_zero_rows_with_parts,
			count() AS num_empty_tables_by_parts
		FROM system.tables AS t
		LEFT JOIN part_stats AS p
			ON p.database = t.database AND p.table = t.name
		WHERE t.database NOT IN ` + systemDatabases + `
		  AND t.engine NOT IN ` + nonStorageEngines + `
		  AND (p.num_active_parts IS NULL OR p.num_active_parts = 0 OR p.rows_from_parts = 0)
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return nil, err
	}
	r := rows[0]
	return []Metric{
		{Name: "num_tables_zero_parts", Value: float64(r.ZeroParts)},
		{Name: "num_tables_zero_rows_with_parts", Value: float64(r.ZeroRowsParts)},
		{Name: "num_empty_tables_by_parts", Value: float64(r.Total)},
	}, nil
}
