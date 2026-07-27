package analyze

import (
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestColdTables(t *testing.T) {
	now := time.Now()
	old := now.Add(-90 * 24 * time.Hour)
	recent := now.Add(-10 * 24 * time.Hour)

	tests := []struct {
		name      string
		tables    []TableInfo
		accesses  []TableAccess
		days      int
		wantCount int
		wantMsg   string
	}{
		{
			name: "flags tables with no recent activity and old modification time",
			tables: []TableInfo{
				{Name: "old_logs", Engine: "MergeTree", TotalRows: ptr(uint64(5000)), TotalBytes: ptr(uint64(125_000)), LastModified: &old},
			},
			wantCount: 1,
			wantMsg:   "5000 rows",
		},
		{
			name: "does not flag tables with recent query activity",
			tables: []TableInfo{
				{Name: "old_logs", Engine: "MergeTree", TotalRows: ptr(uint64(5000)), TotalBytes: ptr(uint64(125_000)), LastModified: &old},
			},
			accesses:  []TableAccess{{Name: "old_logs", QueryCount: 5}},
			wantCount: 0,
		},
		{
			name: "does not flag tables with recent modification time",
			tables: []TableInfo{
				{Name: "recent_table", Engine: "MergeTree", TotalRows: ptr(uint64(1000)), TotalBytes: ptr(uint64(50_000)), LastModified: &recent},
			},
			wantCount: 0,
		},
		{
			name: "does not flag empty tables",
			tables: []TableInfo{
				{Name: "empty_table", Engine: "MergeTree", TotalRows: ptr(uint64(0)), TotalBytes: ptr(uint64(0)), LastModified: &old},
			},
			wantCount: 0,
		},
		{
			name: "does not flag views",
			tables: []TableInfo{
				{Name: "old_view", Engine: "View", IsView: true, LastModified: &old},
			},
			wantCount: 0,
		},
		{
			name: "flags tables with nil last_modified as cold",
			tables: []TableInfo{
				{Name: "mystery_table", Engine: "MergeTree", TotalRows: ptr(uint64(100)), TotalBytes: ptr(uint64(5000))},
			},
			wantCount: 1,
		},
		{
			// With a 30-day access window and a fixed 60-day modification
			// threshold, a table last modified 45 days ago would fall in
			// the 31-59 day blind spot: outside the window we checked for
			// reads, but not yet stale enough by the old fixed threshold.
			// The threshold must be capped at `days` to catch this.
			name: "flags tables modified just past the access window",
			tables: []TableInfo{
				{Name: "warm_ish", Engine: "MergeTree", TotalRows: ptr(uint64(10)), TotalBytes: ptr(uint64(1000)), LastModified: ptr(now.Add(-45 * 24 * time.Hour))},
			},
			days:      30,
			wantCount: 1,
		},
		{
			name: "does not cap the threshold below 60 days when the window is wider",
			tables: []TableInfo{
				{Name: "warm_ish", Engine: "MergeTree", TotalRows: ptr(uint64(10)), TotalBytes: ptr(uint64(1000)), LastModified: ptr(now.Add(-45 * 24 * time.Hour))},
			},
			days:      90,
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			days := tc.days
			if days == 0 {
				days = 30
			}
			recs := coldTables(tc.tables, tc.accesses, "demo", days, now)
			if len(recs) != tc.wantCount {
				t.Fatalf("got %d recommendations, want %d: %+v", len(recs), tc.wantCount, recs)
			}
			if tc.wantCount == 1 {
				rec := recs[0]
				if rec.Severity != SeverityLow {
					t.Errorf("severity = %q, want low", rec.Severity)
				}
				if tc.wantMsg != "" && !strings.Contains(rec.Message, tc.wantMsg) {
					t.Errorf("message %q does not contain %q", rec.Message, tc.wantMsg)
				}
			}
		})
	}
}
