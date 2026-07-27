package analyze

import "testing"

func TestFilter(t *testing.T) {
	recs := []Recommendation{
		{Object: "a", Severity: SeverityLow},
		{Object: "b", Severity: SeverityMedium},
		{Object: "c", Severity: SeverityHigh},
	}

	if got := Filter(recs, nil); len(got) != 3 {
		t.Fatalf("Filter with nil min = %d recs, want 3", len(got))
	}

	medium := SeverityMedium
	got := Filter(recs, &medium)
	if len(got) != 2 {
		t.Fatalf("Filter(medium) = %d recs, want 2", len(got))
	}
	for _, r := range got {
		if r.Severity == SeverityLow {
			t.Errorf("Filter(medium) unexpectedly included a low severity recommendation")
		}
	}
}

func TestParseSeverity(t *testing.T) {
	for _, s := range []string{"low", "medium", "high"} {
		if _, err := ParseSeverity(s); err != nil {
			t.Errorf("ParseSeverity(%q) returned error: %v", s, err)
		}
	}

	if _, err := ParseSeverity("critical"); err == nil {
		t.Error("ParseSeverity(critical) expected an error, got nil")
	}
}
