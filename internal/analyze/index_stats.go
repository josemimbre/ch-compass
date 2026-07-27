package analyze

import (
	"context"

	"github.com/josemimbre/ch-compass/internal/ch"
)

type skipIndexRow struct {
	Table string `ch:"table"`
	Name  string `ch:"name"`
	Type  string `ch:"type"`
	Expr  string `ch:"expr"`
}

// indexStats collects all skip indexes for tables in database from
// system.data_skipping_indices.
func indexStats(ctx context.Context, client *ch.Client, database string) ([]SkipIndex, error) {
	var rows []skipIndexRow
	err := client.Select(ctx, &rows, `
		-- Skip index metadata: list all data skipping indices
		SELECT table, name, type, expr
		FROM system.data_skipping_indices
		WHERE database = {database:String}
	`, ch.Named("database", database))
	if err != nil {
		return nil, err
	}

	indexes := make([]SkipIndex, 0, len(rows))
	for _, r := range rows {
		indexes = append(indexes, SkipIndex{Table: r.Table, Name: r.Name, Type: r.Type, Expr: r.Expr})
	}
	return indexes, nil
}
