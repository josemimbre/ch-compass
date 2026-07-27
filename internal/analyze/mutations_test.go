package analyze

import (
	"strings"
	"testing"
	"time"
)

func TestStuckMutations(t *testing.T) {
	now := time.Now()

	t.Run("flags mutations running longer than 1 hour", func(t *testing.T) {
		mutations := []MutationInfo{{
			Table:      "events",
			MutationID: "mutation_0000000001",
			Command:    "ALTER TABLE events DELETE WHERE user_id = 0",
			CreateTime: now.Add(-2 * time.Hour),
			PartsToDo:  []string{"part_1", "part_2"},
			IsDone:     false,
		}}

		recs := stuckMutations(mutations, "demo", now)
		if len(recs) != 1 {
			t.Fatalf("got %d recommendations, want 1", len(recs))
		}
		rec := recs[0]
		if rec.Object != "events" || rec.Severity != SeverityHigh {
			t.Errorf("unexpected recommendation: %+v", rec)
		}
		if !strings.Contains(rec.Message, "mutation_0000000001") || !strings.Contains(rec.Message, "ALTER TABLE events") {
			t.Errorf("message missing expected content: %q", rec.Message)
		}
		if !strings.Contains(rec.Suggestion, "KILL MUTATION") {
			t.Errorf("suggestion missing KILL MUTATION: %q", rec.Suggestion)
		}
	})

	t.Run("does not flag completed mutations", func(t *testing.T) {
		mutations := []MutationInfo{{
			Table:      "events",
			MutationID: "mutation_0000000001",
			CreateTime: now.Add(-2 * time.Hour),
			IsDone:     true,
		}}

		if recs := stuckMutations(mutations, "demo", now); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("does not flag recent in-progress mutations", func(t *testing.T) {
		mutations := []MutationInfo{{
			Table:      "events",
			MutationID: "mutation_0000000002",
			CreateTime: now.Add(-10 * time.Minute),
			IsDone:     false,
		}}

		if recs := stuckMutations(mutations, "demo", now); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})

	t.Run("flags multiple stuck mutations", func(t *testing.T) {
		mutations := []MutationInfo{
			{Table: "events", MutationID: "mutation_0000000001", CreateTime: now.Add(-3 * time.Hour), IsDone: false},
			{Table: "old_logs", MutationID: "mutation_0000000002", CreateTime: now.Add(-5 * time.Hour), IsDone: false},
		}

		recs := stuckMutations(mutations, "demo", now)
		if len(recs) != 2 {
			t.Fatalf("got %d recommendations, want 2", len(recs))
		}

		objects := map[string]bool{}
		for _, r := range recs {
			objects[r.Object] = true
		}
		if !objects["events"] || !objects["old_logs"] {
			t.Errorf("expected both events and old_logs flagged, got %+v", recs)
		}
	})

	t.Run("returns empty for no mutations", func(t *testing.T) {
		if recs := stuckMutations(nil, "demo", now); len(recs) != 0 {
			t.Fatalf("got %d recommendations, want 0", len(recs))
		}
	})
}
