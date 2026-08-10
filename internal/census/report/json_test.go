package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/josemimbre/ch-compass/internal/census"
)

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSON(&buf, []census.Metric{
		{Name: "total_tables", Value: 42},
		{Name: "table_count_by_engine", Labels: map[string]string{"engine": "MergeTree"}, Value: 7},
	})
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var got []census.Metric
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshalling output: %v\noutput:\n%s", err, buf.String())
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(got))
	}
	if got[0].Name != "total_tables" || got[0].Value != 42 || got[0].Labels != nil {
		t.Errorf("scalar metric round-tripped wrong: %+v", got[0])
	}
	if got[1].Labels["engine"] != "MergeTree" {
		t.Errorf("breakdown metric round-tripped wrong: %+v", got[1])
	}

	// omitempty: a scalar metric's raw JSON must not carry a "labels" key at
	// all, not just an empty one.
	var raw []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshalling raw output: %v", err)
	}
	if _, ok := raw[0]["labels"]; ok {
		t.Errorf("expected no \"labels\" key for a scalar metric, got: %v", raw[0])
	}
	if _, ok := raw[1]["labels"]; !ok {
		t.Errorf("expected a \"labels\" key for a breakdown metric, got: %v", raw[1])
	}
}
