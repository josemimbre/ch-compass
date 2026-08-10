package report

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/josemimbre/ch-compass/internal/census"
)

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	err := WriteCSV(&buf, []census.Metric{
		{Name: "total_tables", Value: 42},
		{Name: "table_count_by_engine", Labels: map[string]string{"engine": "MergeTree"}, Value: 7},
	})
	if err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("parsing CSV output: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected header + 2 rows, got %d: %v", len(records), records)
	}

	wantHeader := []string{"metric", "labels", "value"}
	for i, col := range wantHeader {
		if records[0][i] != col {
			t.Errorf("header[%d] = %q, want %q", i, records[0][i], col)
		}
	}

	if records[1][0] != "total_tables" || records[1][1] != "" || records[1][2] != "42" {
		t.Errorf("unexpected scalar row: %v", records[1])
	}
	if records[2][0] != "table_count_by_engine" || records[2][1] != `{"engine":"MergeTree"}` || records[2][2] != "7" {
		t.Errorf("unexpected breakdown row: %v", records[2])
	}
}

func TestEncodeLabels(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"nil map", nil, ""},
		{"empty map", map[string]string{}, ""},
		{"single key", map[string]string{"engine": "MergeTree"}, `{"engine":"MergeTree"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeLabels(tc.labels)
			if err != nil {
				t.Fatalf("encodeLabels: %v", err)
			}
			if got != tc.want {
				t.Errorf("encodeLabels(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}
