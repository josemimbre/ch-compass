package census

import (
	"context"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// TotalDatabases returns the total number of user databases.
func TotalDatabases(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"total_databases"`
	}
	query := `
		SELECT count() AS total_databases
		FROM system.databases
		WHERE engine NOT IN ('Memory')
		  AND name NOT IN ` + systemDatabases + `
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "total_databases", Value: float64(rows[0].Count)}, nil
}

// DatabaseCountByEngine breaks down database count per engine (Atomic, Ordinary...).
func DatabaseCountByEngine(ctx context.Context, client *ch.Client) ([]Metric, error) {
	var rows []struct {
		Engine string `ch:"engine"`
		Count  uint64 `ch:"num_databases"`
	}
	query := `
		SELECT engine, count() AS num_databases
		FROM system.databases
		WHERE engine NOT IN ('Memory')
		  AND name NOT IN ` + systemDatabases + `
		GROUP BY engine
		ORDER BY num_databases DESC
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return nil, err
	}
	metrics := make([]Metric, len(rows))
	for i, r := range rows {
		metrics[i] = Metric{Name: "database_count_by_engine", Labels: map[string]string{"engine": r.Engine}, Value: float64(r.Count)}
	}
	return metrics, nil
}

// EmptyDatabases returns how many databases contain zero tables.
func EmptyDatabases(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"num_empty_databases"`
	}
	query := `
		SELECT count() AS num_empty_databases
		FROM system.databases
		WHERE name NOT IN ` + systemDatabases + `
		  AND name NOT IN (SELECT DISTINCT database FROM system.tables)
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "num_empty_databases", Value: float64(rows[0].Count)}, nil
}

// MaxMinTablesPerDB returns the largest and smallest database by table
// count. It must LEFT JOIN from system.databases rather than just GROUP BY
// on system.tables — a database with zero tables never shows up in
// system.tables, so grouping on it alone silently ignores empty databases
// and skews the min upward.
func MaxMinTablesPerDB(ctx context.Context, client *ch.Client) ([]Metric, error) {
	var rows []struct {
		Max uint64 `ch:"max_tables_per_db"`
		Min uint64 `ch:"min_tables_per_db"`
	}
	query := `
		SELECT
			max(num_tables) AS max_tables_per_db,
			min(num_tables) AS min_tables_per_db
		FROM
		(
			SELECT
				d.name AS database,
				ifNull(tc.num_tables, 0) AS num_tables
			FROM system.databases AS d
			LEFT JOIN
			(
				SELECT database, count() AS num_tables
				FROM system.tables
				WHERE database NOT IN ` + systemDatabases + `
				GROUP BY database
			) AS tc
			ON tc.database = d.name
			WHERE d.name NOT IN ` + systemDatabases + `
		)
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return nil, err
	}
	r := rows[0]
	return []Metric{
		{Name: "max_tables_per_db", Value: float64(r.Max)},
		{Name: "min_tables_per_db", Value: float64(r.Min)},
	}, nil
}

// DatabasesAllEmpty returns how many databases have at least one table (so
// they don't count toward EmptyDatabases) but every one of those tables
// holds zero rows.
func DatabasesAllEmpty(ctx context.Context, client *ch.Client) (Metric, error) {
	var rows []struct {
		Count uint64 `ch:"num_databases_all_empty"`
	}
	query := `
		SELECT count() AS num_databases_all_empty
		FROM
		(
			SELECT database, sum(ifNull(total_rows, 0)) AS total_rows_in_db
			FROM system.tables
			WHERE database NOT IN ` + systemDatabases + `
			GROUP BY database
			HAVING total_rows_in_db = 0
		)
	`
	if err := client.Select(ctx, &rows, query); err != nil {
		return Metric{}, err
	}
	return Metric{Name: "num_databases_all_empty", Value: float64(rows[0].Count)}, nil
}
