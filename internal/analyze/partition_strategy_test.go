package analyze

import "testing"

func TestPartitionStrategy(t *testing.T) {
	t.Run("flags tables over the partition threshold", func(t *testing.T) {
		tables := []TableInfo{
			{Name: "over_partitioned", Engine: "MergeTree", PartitionCount: 209},
		}

		recs := partitionStrategy(tables, "demo")
		if len(recs) != 1 {
			t.Fatalf("got %d recommendations, want 1", len(recs))
		}
		if recs[0].Severity != SeverityHigh || recs[0].Object != "over_partitioned" {
			t.Errorf("unexpected recommendation: %+v", recs[0])
		}
	})

	t.Run("does not flag tables at or below the threshold", func(t *testing.T) {
		tables := []TableInfo{
			{Name: "fine", Engine: "MergeTree", PartitionCount: partitionThreshold},
		}

		if recs := partitionStrategy(tables, "demo"); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("does not flag views regardless of partition count", func(t *testing.T) {
		tables := []TableInfo{
			{Name: "big_view", Engine: "View", IsView: true, PartitionCount: 1000},
		}

		if recs := partitionStrategy(tables, "demo"); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})
}
