package report

import (
	"strings"
	"testing"

	"github.com/josemimbre/ch-compass/internal/analyze"
)

func sampleResults() []analyze.Result {
	return []analyze.Result{
		{
			Database: "demo",
			Tables:   []analyze.TableInfo{{Name: "events", Engine: "MergeTree", TotalRows: u64(100), TotalBytes: u64(2048), PartitionCount: 3}},
			Views:    []analyze.TableInfo{{Name: "daily_events", Engine: "View"}},
			Recommendations: []analyze.Recommendation{{
				Type: analyze.TypeOverPartitioned, Object: "events", Database: "demo",
				Severity: analyze.SeverityHigh, Message: "209 partitions", Suggestion: "PARTITION BY toYYYYMM(...)",
			}},
		},
	}
}

func TestWriteMarkdown(t *testing.T) {
	var buf strings.Builder
	if err := WriteMarkdown(&buf, sampleResults(), "24.1", nil); err != nil {
		t.Fatalf("WriteMarkdown returned error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"# ch-compass Report",
		"| **ClickHouse** | 24.1 |",
		"## Objects",
		"| events | MergeTree | 100 | 2.0 KB | 3 |",
		"| daily_events | View | - | - | - |",
		"#### [HIGH] Over-partitioned table 'events'",
		"```sql\nPARTITION BY toYYYYMM(...)\n```",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown output missing %q\n---\n%s", want, out)
		}
	}
}

func TestWriteMarkdownNoRecommendations(t *testing.T) {
	var buf strings.Builder
	results := []analyze.Result{{Database: "demo"}}
	if err := WriteMarkdown(&buf, results, "24.1", nil); err != nil {
		t.Fatalf("WriteMarkdown returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "> No recommendations found.") {
		t.Errorf("expected empty-state message, got:\n%s", buf.String())
	}
}
