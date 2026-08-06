package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/josemimbre/ch-compass/internal/analyze"
)

// noteStyle renders a "Note: ..." line as a distinct terminal warning,
// reusing the same amber as a medium-severity finding.
var noteStyle = lipgloss.NewStyle().Foreground(severityColors[analyze.SeverityMedium])

// NoteWriter wraps w so any write beginning with "Note: " — the
// convention internal/analyze uses for degraded-detection warnings, a
// skipped check, or a retention shortfall — renders as a styled warning
// line instead of plain text. internal/analyze deliberately knows nothing
// about terminal styling (see the Layout section of the README); this is
// where that plain-text convention becomes presentation, at the boundary
// where it actually reaches a terminal. Anything not starting with
// "Note: " passes through unstyled.
type NoteWriter struct {
	w io.Writer
}

// NewNoteWriter wraps w so "Note: " lines written to it are styled.
func NewNoteWriter(w io.Writer) NoteWriter {
	return NoteWriter{w: w}
}

func (n NoteWriter) Write(p []byte) (int, error) {
	s := strings.TrimSuffix(string(p), "\n")
	if !strings.HasPrefix(s, "Note: ") {
		return n.w.Write(p)
	}
	if _, err := fmt.Fprintln(n.w, noteStyle.Render("⚠ "+s)); err != nil {
		return 0, err
	}
	return len(p), nil
}
