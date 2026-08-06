package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestNoteWriter(t *testing.T) {
	t.Run("styles a Note line and reports it fully written", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewNoteWriter(&buf)

		n, err := w.Write([]byte("Note: system.query_log only retains 0 day(s) of history\n"))
		if err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
		if n != len("Note: system.query_log only retains 0 day(s) of history\n") {
			t.Errorf("got n=%d, want len(p)", n)
		}

		got := buf.String()
		if !strings.Contains(got, "⚠") || !strings.Contains(got, "Note: system.query_log only retains 0 day(s) of history") {
			t.Errorf("unexpected output: %q", got)
		}
	})

	t.Run("passes non-Note lines through unchanged", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewNoteWriter(&buf)

		if _, err := w.Write([]byte("Analyzing database 'demo'...\n")); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
		if got := buf.String(); got != "Analyzing database 'demo'...\n" {
			t.Errorf("got %q, want passthrough", got)
		}
	})
}
