package analyze

import (
	"strings"
	"testing"
)

func TestUnusedViews(t *testing.T) {
	t.Run("flags regular views with no query activity", func(t *testing.T) {
		tables := []TableInfo{
			{Name: "events", Engine: "MergeTree"},
			{Name: "daily_events", Engine: "View", IsView: true, ViewType: ViewTypeRegular},
			{Name: "user_stats", Engine: "View", IsView: true, ViewType: ViewTypeRegular},
		}
		accesses := []TableAccess{{Name: "events", QueryCount: 50}}

		recs := unusedViews(tables, accesses, "demo", 30)
		if len(recs) != 2 {
			t.Fatalf("got %d recommendations, want 2", len(recs))
		}

		objects := map[string]bool{}
		for _, r := range recs {
			if r.Type != TypeUnusedView || r.Severity != SeverityMedium {
				t.Errorf("unexpected recommendation: %+v", r)
			}
			objects[r.Object] = true
		}
		if !objects["daily_events"] || !objects["user_stats"] {
			t.Errorf("expected both views flagged, got %+v", recs)
		}
	})

	t.Run("does not flag views that have been accessed", func(t *testing.T) {
		tables := []TableInfo{{Name: "daily_events", Engine: "View", ViewType: ViewTypeRegular}}
		accesses := []TableAccess{{Name: "daily_events", QueryCount: 10}}
		if recs := unusedViews(tables, accesses, "demo", 30); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("does not flag regular tables", func(t *testing.T) {
		tables := []TableInfo{{Name: "events", Engine: "MergeTree"}}
		if recs := unusedViews(tables, nil, "demo", 30); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("does not flag materialized views", func(t *testing.T) {
		tables := []TableInfo{{Name: "hourly_events", Engine: "MaterializedView", ViewType: ViewTypeMaterialized}}
		if recs := unusedViews(tables, nil, "demo", 30); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("suggestion includes database name", func(t *testing.T) {
		tables := []TableInfo{{Name: "old_view", Engine: "View", ViewType: ViewTypeRegular}}
		recs := unusedViews(tables, nil, "production", 30)
		if len(recs) != 1 || recs[0].Suggestion != "DROP VIEW IF EXISTS production.old_view" {
			t.Fatalf("unexpected recommendation: %+v", recs)
		}
	})

	t.Run("message uses the days parameter", func(t *testing.T) {
		tables := []TableInfo{{Name: "old_view", Engine: "View", ViewType: ViewTypeRegular}}
		recs := unusedViews(tables, nil, "demo", 7)
		if len(recs) != 1 || !strings.Contains(recs[0].Message, "last 7 days") {
			t.Fatalf("unexpected message: %+v", recs)
		}
	})
}

func TestUnusedMaterializedViews(t *testing.T) {
	t.Run("flags materialized views with no activity", func(t *testing.T) {
		tables := []TableInfo{
			{Name: "event_counts", Engine: "MaterializedView", ViewType: ViewTypeMaterialized},
			{Name: "hourly_events", Engine: "MaterializedView", ViewType: ViewTypeMaterialized},
		}

		recs := unusedMaterializedViews(tables, nil, "demo", 30)
		if len(recs) != 2 {
			t.Fatalf("got %d recommendations, want 2", len(recs))
		}
		for _, r := range recs {
			if r.Type != TypeUnusedMaterializedView || r.Severity != SeverityMedium {
				t.Errorf("unexpected recommendation: %+v", r)
			}
		}
	})

	t.Run("does not flag materialized views that have been triggered", func(t *testing.T) {
		tables := []TableInfo{{Name: "hourly_events", Engine: "MaterializedView", ViewType: ViewTypeMaterialized}}
		accesses := []TableAccess{{Name: "hourly_events", QueryCount: 5}}
		if recs := unusedMaterializedViews(tables, accesses, "demo", 30); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("does not flag regular views", func(t *testing.T) {
		tables := []TableInfo{{Name: "daily_events", Engine: "View", ViewType: ViewTypeRegular}}
		if recs := unusedMaterializedViews(tables, nil, "demo", 30); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("message mentions materialized view and includes database in suggestion", func(t *testing.T) {
		tables := []TableInfo{{Name: "old_mv", Engine: "MaterializedView", ViewType: ViewTypeMaterialized}}
		recs := unusedMaterializedViews(tables, nil, "demo", 30)
		if len(recs) != 1 {
			t.Fatalf("got %d recommendations, want 1", len(recs))
		}
		if !strings.Contains(recs[0].Message, "Materialized view") {
			t.Errorf("message missing 'Materialized view': %q", recs[0].Message)
		}
		if recs[0].Suggestion != "DROP VIEW IF EXISTS demo.old_mv" {
			t.Errorf("unexpected suggestion: %q", recs[0].Suggestion)
		}
	})
}
