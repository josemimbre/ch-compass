package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/josemimbre/ch-compass/internal/census"
)

func TestWriteTextScalarSection(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, []census.Metric{
		{Name: "total_tables", Value: 42},
	})
	out := buf.String()

	if !strings.Contains(out, "Tables") {
		t.Errorf("expected a Tables section header, got:\n%s", out)
	}
	if !strings.Contains(out, "Total tables") {
		t.Errorf("expected the friendly name for total_tables, got:\n%s", out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("expected the value 42, got:\n%s", out)
	}
	if strings.Contains(out, "Databases") {
		t.Errorf("did not expect a Databases section with no database metrics, got:\n%s", out)
	}
	if strings.Contains(out, "Replicas") {
		t.Errorf("did not expect a Replicas section with no replica metrics, got:\n%s", out)
	}
}

func TestWriteTextBreakdownSortedDescending(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, []census.Metric{
		{Name: "total_tables", Value: 10},
		{Name: "table_count_by_engine", Labels: map[string]string{"engine": "View"}, Value: 2},
		{Name: "table_count_by_engine", Labels: map[string]string{"engine": "MergeTree"}, Value: 7},
	})
	out := buf.String()

	mergeTreeIdx := strings.Index(out, "MergeTree")
	viewIdx := strings.Index(out, "View")
	if mergeTreeIdx == -1 || viewIdx == -1 {
		t.Fatalf("expected both engines in output, got:\n%s", out)
	}
	if mergeTreeIdx > viewIdx {
		t.Errorf("expected MergeTree (7) to sort before View (2), got:\n%s", out)
	}
}

func TestWriteTextReplicaPivot(t *testing.T) {
	var buf bytes.Buffer
	WriteText(&buf, []census.Metric{
		{Name: "replica_table_count", Labels: map[string]string{"replica": "ch2"}, Value: 14},
		{Name: "replica_num_mvs", Labels: map[string]string{"replica": "ch2"}, Value: 4},
		{Name: "replica_num_views", Labels: map[string]string{"replica": "ch2"}, Value: 2},
		{Name: "replica_table_count", Labels: map[string]string{"replica": "ch1"}, Value: 13},
		{Name: "replica_num_mvs", Labels: map[string]string{"replica": "ch1"}, Value: 4},
		{Name: "replica_num_views", Labels: map[string]string{"replica": "ch1"}, Value: 2},
	})
	out := buf.String()

	if !strings.Contains(out, "Replicas") {
		t.Fatalf("expected a Replicas section, got:\n%s", out)
	}

	ch1Idx := strings.Index(out, "ch1")
	ch2Idx := strings.Index(out, "ch2")
	if ch1Idx == -1 || ch2Idx == -1 {
		t.Fatalf("expected both replicas in output, got:\n%s", out)
	}
	if ch1Idx > ch2Idx {
		t.Errorf("expected replicas sorted alphabetically (ch1 before ch2), got:\n%s", out)
	}
	if !strings.Contains(out, "13") || !strings.Contains(out, "14") {
		t.Errorf("expected both replica table counts in output, got:\n%s", out)
	}
}

func TestWriteTextIgnoresUnrelatedMetricsInReplicaPivot(t *testing.T) {
	// Regression test: writeReplicas must only look at replica_* metrics —
	// it used to index ALL metrics by Label (now Labels["replica"]), so a
	// scalar metric with no Labels produced a bogus all-zero replica row
	// keyed by the empty string.
	var buf bytes.Buffer
	WriteText(&buf, []census.Metric{
		{Name: "total_tables", Value: 42},
	})
	out := buf.String()

	if strings.Contains(out, "Replicas") {
		t.Errorf("did not expect a Replicas section when no replica metrics were collected, got:\n%s", out)
	}
}

func TestFormatValue(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{42, "42"},
		{0, "0"},
		{3.5, "3.5"},
	}
	for _, tc := range cases {
		if got := formatValue(tc.in); got != tc.want {
			t.Errorf("formatValue(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if got := displayName("total_tables"); got != "Total tables" {
		t.Errorf("displayName(total_tables) = %q, want friendly name", got)
	}
	if got := displayName("some_unknown_metric"); got != "some_unknown_metric" {
		t.Errorf("displayName fallback = %q, want the raw metric name", got)
	}
}
