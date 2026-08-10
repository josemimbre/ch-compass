package census

import (
	"context"

	"github.com/josemimbre/ch-compass/internal/ch"
)

// ReplicaSummary breaks down table/MV/view counts per replica of a
// ClickHouse cluster, to spot desync between replicas. The client must have
// been Connect'd with Options.Cluster set (see ch.Client.AllReplicasSource),
// which is also what safely quotes the cluster name into the query. Relies
// on clusterAllReplicas evaluating hostName() on the replica that actually
// processes each row.
func ReplicaSummary(ctx context.Context, client *ch.Client) ([]Metric, error) {
	var rows []struct {
		Replica string `ch:"replica"`
		Tables  uint64 `ch:"num_tables"`
		MVs     uint64 `ch:"num_mvs"`
		Views   uint64 `ch:"num_views"`
	}
	query := `
		SELECT
		    hostName() AS replica,
		    count() AS num_tables,
		    countIf(engine = 'MaterializedView') AS num_mvs,
		    countIf(engine = 'View') AS num_views
		FROM ` + client.AllReplicasSource("system.tables") + `
		WHERE database NOT IN ` + systemDatabases + `
		GROUP BY replica
		ORDER BY replica
	`

	if err := client.Select(ctx, &rows, query); err != nil {
		return nil, err
	}

	metrics := make([]Metric, 0, len(rows)*3)
	for _, r := range rows {
		metrics = append(metrics,
			Metric{Name: "replica_table_count", Labels: map[string]string{"replica": r.Replica}, Value: float64(r.Tables)},
			Metric{Name: "replica_num_mvs", Labels: map[string]string{"replica": r.Replica}, Value: float64(r.MVs)},
			Metric{Name: "replica_num_views", Labels: map[string]string{"replica": r.Replica}, Value: float64(r.Views)},
		)
	}
	return metrics, nil
}
