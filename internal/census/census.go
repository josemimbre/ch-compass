package census

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// Options configures which metrics Collect gathers.
type Options struct {
	// ClusterName is the ClickHouse cluster macro (registered in
	// system.clusters) used for the per-replica desync metrics. Leave empty
	// to skip those and only collect cluster-wide metrics.
	ClusterName string
}

// Collect runs every census query against client and returns the combined
// list of metrics. Every query is independent (each is a standalone SELECT
// keyed off systemDatabases/nonStorageEngines, none depend on another's
// result), so they run concurrently rather than one after another — same
// reasoning as internal/analyze.Database's collectors.
func Collect(ctx context.Context, client *ch.Client, opts Options) ([]Metric, error) {
	scalars := []struct {
		name    string
		collect func(context.Context, *ch.Client) (Metric, error)
	}{
		{"total_databases", TotalDatabases},
		{"num_empty_databases", EmptyDatabases},
		{"num_databases_all_empty", DatabasesAllEmpty},
		{"total_tables", TotalTables},
		{"num_empty_tables", EmptyTables},
		{"total_materialized_views", TotalMaterializedViews},
		{"num_mv_targets", MVTargetCount},
		{"num_landing_tables", LandingTables},
		{"mvs_linked_to_landing", MVsLinkedToLanding},
		{"num_fan_in_targets", FanInTargets},
	}
	breakdowns := []struct {
		name    string
		collect func(context.Context, *ch.Client) ([]Metric, error)
	}{
		{"database_count_by_engine", DatabaseCountByEngine},
		{"max_min_tables_per_db", MaxMinTablesPerDB},
		{"table_count_by_engine", TableCountByEngine},
		{"empty_tables_by_parts", EmptyTablesByParts},
		{"mv_sanity_check", MVSanityCheck},
	}

	scalarResults := make([]Metric, len(scalars))
	breakdownResults := make([][]Metric, len(breakdowns))
	var replicaResults []Metric

	g, gctx := errgroup.WithContext(ctx)
	for i, s := range scalars {
		g.Go(func() error {
			m, err := s.collect(gctx, client)
			if err != nil {
				return fmt.Errorf("collecting %s: %w", s.name, err)
			}
			scalarResults[i] = m
			return nil
		})
	}
	for i, b := range breakdowns {
		g.Go(func() error {
			ms, err := b.collect(gctx, client)
			if err != nil {
				return fmt.Errorf("collecting %s: %w", b.name, err)
			}
			breakdownResults[i] = ms
			return nil
		})
	}
	if opts.ClusterName != "" {
		g.Go(func() error {
			ms, err := ReplicaSummary(gctx, client)
			if err != nil {
				return fmt.Errorf("collecting replica summary: %w", err)
			}
			replicaResults = ms
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var metrics []Metric
	for _, m := range scalarResults {
		metrics = append(metrics, m)
	}
	for _, ms := range breakdownResults {
		metrics = append(metrics, ms...)
	}
	metrics = append(metrics, replicaResults...)

	return metrics, nil
}
