package analyze

import (
	"context"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// ListDatabases returns every database on the server except ClickHouse's own
// system schemas, for --all-databases.
func ListDatabases(ctx context.Context, client *ch.Client) ([]string, error) {
	var rows []struct {
		Name string `ch:"name"`
	}
	err := client.Select(ctx, &rows, `
		-- List user databases, excluding ClickHouse's own system schemas
		SELECT name
		FROM system.databases
		WHERE name NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA')
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	return names, nil
}
