package analyze

import (
	"context"
	"time"

	"github.com/josemimbre/ch-compass/internal/ch"
)

type mutationRow struct {
	Table      string    `ch:"table"`
	MutationID string    `ch:"mutation_id"`
	Command    string    `ch:"command"`
	CreateTime time.Time `ch:"create_time"`
	PartsToDo  []string  `ch:"parts_to_do_names"`
	IsDone     uint8     `ch:"is_done"`
}

// mutationStats collects all mutations for database from system.mutations,
// most recent first.
func mutationStats(ctx context.Context, client *ch.Client, database string) ([]MutationInfo, error) {
	var rows []mutationRow
	err := client.Select(ctx, &rows, `
		-- Mutation status: list all mutations with progress and completion state
		SELECT
			table,
			mutation_id,
			command,
			create_time,
			parts_to_do_names,
			is_done
		FROM system.mutations
		WHERE database = {database:String}
		ORDER BY create_time DESC
	`, ch.Named("database", database))
	if err != nil {
		return nil, err
	}

	infos := make([]MutationInfo, 0, len(rows))
	for _, r := range rows {
		infos = append(infos, MutationInfo{
			Table:      r.Table,
			MutationID: r.MutationID,
			Command:    r.Command,
			CreateTime: r.CreateTime,
			PartsToDo:  r.PartsToDo,
			IsDone:     r.IsDone != 0,
		})
	}

	return infos, nil
}
