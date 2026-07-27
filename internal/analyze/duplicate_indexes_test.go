package analyze

import (
	"strings"
	"testing"
)

func TestDuplicateIndexes(t *testing.T) {
	t.Run("flags skip index on a sorting key column", func(t *testing.T) {
		tables := []TableInfo{{Name: "search_logs", Engine: "MergeTree", SortingKey: "url, ts"}}
		indexes := []SkipIndex{{Table: "search_logs", Name: "idx_url", Type: "minmax", Expr: "url"}}

		recs := duplicateIndexes(tables, indexes, "analytics")
		if len(recs) != 1 {
			t.Fatalf("got %d recommendations, want 1", len(recs))
		}
		rec := recs[0]
		if rec.Type != TypeDuplicateIndex || rec.Object != "search_logs.idx_url" || rec.Database != "analytics" || rec.Severity != SeverityLow {
			t.Errorf("unexpected recommendation: %+v", rec)
		}
		if !strings.Contains(rec.Message, "idx_url") || !strings.Contains(rec.Message, "redundant") {
			t.Errorf("unexpected message: %q", rec.Message)
		}
		if !strings.Contains(rec.Suggestion, "DROP INDEX idx_url") {
			t.Errorf("unexpected suggestion: %q", rec.Suggestion)
		}
	})

	t.Run("does not flag skip index on a non-sorting-key column", func(t *testing.T) {
		tables := []TableInfo{{Name: "events", Engine: "MergeTree", SortingKey: "event_type, user_id"}}
		indexes := []SkipIndex{{Table: "events", Name: "idx_payload", Type: "bloom_filter", Expr: "payload"}}
		if recs := duplicateIndexes(tables, indexes, "demo"); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("flags multi-column index when all columns are in the sorting key", func(t *testing.T) {
		tables := []TableInfo{{Name: "events", Engine: "MergeTree", SortingKey: "a, b, c"}}
		indexes := []SkipIndex{{Table: "events", Name: "idx_ab", Type: "minmax", Expr: "a, b"}}

		recs := duplicateIndexes(tables, indexes, "demo")
		if len(recs) != 1 || recs[0].Object != "events.idx_ab" {
			t.Fatalf("unexpected recommendations: %+v", recs)
		}
	})

	t.Run("does not flag when only some index columns are in the sorting key", func(t *testing.T) {
		tables := []TableInfo{{Name: "events", Engine: "MergeTree", SortingKey: "a, b"}}
		indexes := []SkipIndex{{Table: "events", Name: "idx_ac", Type: "minmax", Expr: "a, c"}}
		if recs := duplicateIndexes(tables, indexes, "demo"); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("ignores indexes on tables with no sorting key", func(t *testing.T) {
		tables := []TableInfo{{Name: "events", Engine: "MergeTree", SortingKey: ""}}
		indexes := []SkipIndex{{Table: "events", Name: "idx_x", Type: "minmax", Expr: "x"}}
		if recs := duplicateIndexes(tables, indexes, "demo"); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("ignores views", func(t *testing.T) {
		tables := []TableInfo{{Name: "my_view", Engine: "View", SortingKey: "a", IsView: true}}
		indexes := []SkipIndex{{Table: "my_view", Name: "idx_a", Type: "minmax", Expr: "a"}}
		if recs := duplicateIndexes(tables, indexes, "demo"); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("returns empty when no indexes exist", func(t *testing.T) {
		tables := []TableInfo{{Name: "events", Engine: "MergeTree", SortingKey: "a, b"}}
		if recs := duplicateIndexes(tables, nil, "demo"); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})
}
