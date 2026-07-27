package analyze

import (
	"context"
	"strings"
	"time"

	"github.com/josemimbre/ch-compass/internal/ch"
)

type tableRow struct {
	Name       string  `ch:"name"`
	Engine     string  `ch:"engine"`
	TotalRows  *uint64 `ch:"total_rows"`
	TotalBytes *uint64 `ch:"total_bytes"`
	SortingKey string  `ch:"sorting_key"`
}

type partRow struct {
	Table          string    `ch:"table"`
	PartitionCount uint64    `ch:"partition_count"`
	PartCount      uint64    `ch:"part_count"`
	LastModified   time.Time `ch:"last_modified"`
}

// tableStats collects table and partition info for all tables and views in
// database, from system.tables merged with system.parts.
func tableStats(ctx context.Context, client *ch.Client, database string) ([]TableInfo, error) {
	var tables []tableRow
	err := client.Select(ctx, &tables, `
		-- Table metadata: list all tables/views with engine, row count, size and sorting key
		SELECT
			name,
			engine,
			total_rows,
			total_bytes,
			sorting_key
		FROM system.tables
		WHERE database = {database:String}
	`, ch.Named("database", database))
	if err != nil {
		return nil, err
	}

	var parts []partRow
	err = client.Select(ctx, &parts, `
		-- Part stats: partition count, part count and last modification per table
		SELECT
			table,
			count(DISTINCT partition) AS partition_count,
			count() AS part_count,
			max(modification_time) AS last_modified
		FROM system.parts
		WHERE database = {database:String}
			AND active = 1
		GROUP BY table
	`, ch.Named("database", database))
	if err != nil {
		return nil, err
	}

	partsByTable := make(map[string]partRow, len(parts))
	for _, p := range parts {
		partsByTable[p.Table] = p
	}

	infos := make([]TableInfo, 0, len(tables))
	for _, t := range tables {
		info := TableInfo{
			Name:       t.Name,
			Engine:     t.Engine,
			TotalRows:  t.TotalRows,
			TotalBytes: t.TotalBytes,
			IsView:     strings.Contains(t.Engine, "View"),
			ViewType:   viewType(t.Engine),
			SortingKey: t.SortingKey,
		}

		if p, ok := partsByTable[t.Name]; ok {
			info.PartitionCount = p.PartitionCount
			info.PartCount = p.PartCount
			lastModified := p.LastModified
			info.LastModified = &lastModified
		}

		infos = append(infos, info)
	}

	return infos, nil
}

func viewType(engine string) ViewType {
	switch engine {
	case "MaterializedView":
		return ViewTypeMaterialized
	case "View":
		return ViewTypeRegular
	case "LiveView":
		return ViewTypeLive
	default:
		return ViewTypeNone
	}
}
