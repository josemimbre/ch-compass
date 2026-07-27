package report

import (
	"strings"
	"testing"

	"github.com/josemimbre/ch-compass/internal/analyze"
)

func TestWriteHTML(t *testing.T) {
	var buf strings.Builder
	if err := WriteHTML(&buf, sampleResults(), "24.1", nil); err != nil {
		t.Fatalf("WriteHTML returned error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"<!DOCTYPE html>",
		"<title>ch-compass Report</title>",
		`<div class="value">24.1</div>`,
		"<tr><td>events</td><td>MergeTree</td><td>100</td><td>2.0 KB</td><td>3</td></tr>",
		`<div class="rec high">`,
		`<span class="badge high">HIGH</span>`,
		"Over-partitioned table 'events'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output missing %q\n---\n%s", want, out)
		}
	}
}

func TestWriteHTMLEscapesUserData(t *testing.T) {
	results := []analyze.Result{{
		Database: "demo",
		Recommendations: []analyze.Recommendation{{
			Type: analyze.TypeColdTable, Object: "<script>alert(1)</script>", Database: "demo",
			Severity: analyze.SeverityLow, Message: "no reads", Suggestion: "drop it",
		}},
	}}

	var buf strings.Builder
	if err := WriteHTML(&buf, results, "24.1", nil); err != nil {
		t.Fatalf("WriteHTML returned error: %v", err)
	}

	if strings.Contains(buf.String(), "<script>") {
		t.Errorf("expected object name to be HTML-escaped, got:\n%s", buf.String())
	}
}

func TestWriteHTMLNoRecommendations(t *testing.T) {
	var buf strings.Builder
	results := []analyze.Result{{Database: "demo"}}
	if err := WriteHTML(&buf, results, "24.1", nil); err != nil {
		t.Fatalf("WriteHTML returned error: %v", err)
	}

	if !strings.Contains(buf.String(), `class="empty"`) {
		t.Errorf("expected empty-state markup, got:\n%s", buf.String())
	}
}
