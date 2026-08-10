package report

import (
	"testing"

	"github.com/josemimbre/ch-compass/internal/analyze"
)

func u64(v uint64) *uint64 { return &v }

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   *uint64
		want string
	}{
		{nil, "0 B"},
		{u64(0), "0 B"},
		{u64(1023), "1023 B"},
		{u64(1024), "1.0 KB"},
		{u64(1_048_576), "1.0 MB"},
		{u64(1_073_741_824), "1.0 GB"},
	}

	for _, tc := range tests {
		if got := formatBytes(tc.in); got != tc.want {
			t.Errorf("formatBytes(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	if got := formatNumber(nil); got != "0" {
		t.Errorf("formatNumber(nil) = %q, want 0", got)
	}
	if got := formatNumber(u64(42)); got != "42" {
		t.Errorf("formatNumber(42) = %q, want 42", got)
	}
}

func TestFormatType(t *testing.T) {
	if got := formatType(analyze.TypeColdTable); got != "Cold table" {
		t.Errorf("formatType(cold_table) = %q, want Cold table", got)
	}
	if got := formatType(analyze.RecType("unknown_thing")); got != "Unknown thing" {
		t.Errorf("formatType(unknown_thing) = %q, want Unknown thing", got)
	}
}
