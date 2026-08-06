package analyze

import (
	"strings"
	"testing"
)

func TestRetentionNote(t *testing.T) {
	t.Run("retention shorter than the requested window warns", func(t *testing.T) {
		note := retentionNote("system.query_log", 10, 30) // only 10 days retained
		if note == "" {
			t.Fatal("got empty note, want a warning")
		}
		if !strings.Contains(note, "system.query_log") || !strings.Contains(note, "10 day") || !strings.Contains(note, "--days 30") {
			t.Errorf("unexpected note: %q", note)
		}
	})

	t.Run("zero days retained still warns", func(t *testing.T) {
		note := retentionNote("system.query_views_log", 0, 30)
		if note == "" {
			t.Fatal("got empty note, want a warning")
		}
	})

	t.Run("retention at least as long as the window is silent", func(t *testing.T) {
		if note := retentionNote("system.query_log", 45, 30); note != "" {
			t.Errorf("got %q, want empty", note)
		}
	})

	t.Run("retention exactly equal to the window is silent", func(t *testing.T) {
		if note := retentionNote("system.query_log", 30, 30); note != "" {
			t.Errorf("got %q, want empty", note)
		}
	})
}

func TestLogDaysRetainedEffectiveDays(t *testing.T) {
	t.Run("unknown retention leaves requested untouched", func(t *testing.T) {
		var r logDaysRetained
		if got := r.effectiveDays(30); got != 30 {
			t.Errorf("got %d, want 30", got)
		}
	})

	t.Run("known retention shorter than requested caps it", func(t *testing.T) {
		r := logDaysRetained{days: 5, known: true}
		if got := r.effectiveDays(30); got != 5 {
			t.Errorf("got %d, want 5", got)
		}
	})

	t.Run("known retention at least as long as requested is untouched", func(t *testing.T) {
		r := logDaysRetained{days: 45, known: true}
		if got := r.effectiveDays(30); got != 30 {
			t.Errorf("got %d, want 30", got)
		}
	})

	t.Run("zero known retention caps to zero", func(t *testing.T) {
		r := logDaysRetained{days: 0, known: true}
		if got := r.effectiveDays(30); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})
}
