package analyze

import (
	"strings"
	"testing"
	"time"
)

func TestRetentionNote(t *testing.T) {
	now := time.Date(2024, 6, 30, 12, 0, 0, 0, time.UTC)

	t.Run("empty table (zero earliest) is not a retention problem", func(t *testing.T) {
		if note := retentionNote("system.query_log", time.Time{}, now, 30); note != "" {
			t.Errorf("got %q, want empty", note)
		}
	})

	t.Run("retention shorter than the requested window warns", func(t *testing.T) {
		earliest := now.Add(-10 * 24 * time.Hour) // only 10 days retained
		note := retentionNote("system.query_log", earliest, now, 30)
		if note == "" {
			t.Fatal("got empty note, want a warning")
		}
		if !strings.Contains(note, "system.query_log") || !strings.Contains(note, "10 day") || !strings.Contains(note, "--days 30") {
			t.Errorf("unexpected note: %q", note)
		}
	})

	t.Run("retention at least as long as the window is silent", func(t *testing.T) {
		earliest := now.Add(-45 * 24 * time.Hour)
		if note := retentionNote("system.query_log", earliest, now, 30); note != "" {
			t.Errorf("got %q, want empty", note)
		}
	})

	t.Run("retention exactly equal to the window is silent", func(t *testing.T) {
		earliest := now.Add(-30 * 24 * time.Hour)
		if note := retentionNote("system.query_log", earliest, now, 30); note != "" {
			t.Errorf("got %q, want empty", note)
		}
	})
}
