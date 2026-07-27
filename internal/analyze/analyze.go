package analyze

import (
	"context"
	"io"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// Database collects table, query, mutation, and index metrics for database
// and runs every analyzer against them. The four collectors are
// independent, so they run concurrently rather than one after another.
// Notes about unavailable system tables are written to notes.
func Database(ctx context.Context, client *ch.Client, database string, days int, notes io.Writer) (Result, error) {
	var (
		tables    []TableInfo
		accesses  []TableAccess
		mutations []MutationInfo
		indexes   []SkipIndex
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		tables, err = tableStats(gctx, client, database)
		return err
	})
	g.Go(func() (err error) {
		accesses, err = queryPatterns(gctx, client, database, days, notes)
		return err
	})
	g.Go(func() (err error) {
		mutations, err = mutationStats(gctx, client, database)
		return err
	})
	g.Go(func() (err error) {
		indexes, err = indexStats(gctx, client, database)
		return err
	})
	if err := g.Wait(); err != nil {
		return Result{}, err
	}

	now := time.Now()
	var recs []Recommendation
	recs = append(recs, partitionStrategy(tables, database)...)
	recs = append(recs, coldTables(tables, accesses, database, days, now)...)
	recs = append(recs, stuckMutations(mutations, database, now)...)
	recs = append(recs, unusedViews(tables, accesses, database, days)...)
	recs = append(recs, unusedMaterializedViews(tables, accesses, database, days)...)
	recs = append(recs, duplicateIndexes(tables, indexes, database)...)

	var plainTables, views []TableInfo
	for _, t := range tables {
		if t.IsView {
			views = append(views, t)
		} else {
			plainTables = append(plainTables, t)
		}
	}

	return Result{
		Database:        database,
		Tables:          plainTables,
		Views:           views,
		Recommendations: recs,
	}, nil
}

// AllRecommendations flattens recommendations across every result, filtered
// to minSeverity.
func AllRecommendations(results []Result, minSeverity *Severity) []Recommendation {
	var all []Recommendation
	for _, r := range results {
		all = append(all, r.Recommendations...)
	}
	return Filter(all, minSeverity)
}
