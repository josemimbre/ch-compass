// Package census collects cluster-wide inventory metrics from ClickHouse:
// database/table counts, materialized view landing/fan-in relationships,
// replica desync, and empty tables — for comparing clusters over time.
package census

// Metric is one collected data point: a scalar count, or one row of a
// breakdown (per engine, per replica...) distinguished by Labels. Labels is
// nil/empty for scalar metrics.
//
// Labels is keyed by what it identifies (e.g. "engine", "replica"), so a
// row is self-describing on its own — without it, a consumer would have to
// already know, out of band, that e.g. table_count_by_engine's label is an
// engine name while replica_table_count's label is a hostname.
type Metric struct {
	Name   string            `json:"metric"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
}

// systemDatabases lists ClickHouse's own schemas, excluded from every count.
const systemDatabases = `('system', 'information_schema', 'INFORMATION_SCHEMA')`

// nonStorageEngines lists engines that don't hold their own row data, so
// `total_rows` is not meaningful for them (View, MaterializedView are
// virtual; Distributed/Merge/Dictionary read from elsewhere).
const nonStorageEngines = `('View', 'MaterializedView', 'Distributed', 'Merge', 'Dictionary')`
