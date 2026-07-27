package analyze

import (
	"fmt"
	"time"
)

// defaultColdThreshold is how long a table must go without reads or writes
// before it's flagged as cold, when that's not limited further by the
// query-access window (see coldTables).
const defaultColdThreshold = 60 * 24 * time.Hour

// coldTables flags non-view, non-empty tables with no query activity and no
// part modification within the cold threshold of now. accesses only covers
// the last `days` days (see queryPatterns), so we can only vouch for the
// absence of reads within that window. Capping the threshold at `days`
// avoids a blind spot where a table read just outside that window, but
// still within defaultColdThreshold, would otherwise be misreported as cold.
func coldTables(tables []TableInfo, accesses []TableAccess, database string, days int, now time.Time) []Recommendation {
	threshold := defaultColdThreshold
	if d := time.Duration(days) * 24 * time.Hour; d < threshold {
		threshold = d
	}

	accessed := make(map[string]bool, len(accesses))
	for _, a := range accesses {
		accessed[a.Name] = true
	}

	var recs []Recommendation

	for _, t := range tables {
		if t.IsView || accessed[t.Name] || t.TotalRows == nil || *t.TotalRows == 0 {
			continue
		}
		if !isStale(t.LastModified, now, threshold) {
			continue
		}

		sizeMB := 0.0
		if t.TotalBytes != nil {
			sizeMB = float64(*t.TotalBytes) / 1_048_576
		}

		recs = append(recs, Recommendation{
			Type:     TypeColdTable,
			Object:   t.Name,
			Database: database,
			Severity: SeverityLow,
			Message: fmt.Sprintf(
				"No reads or writes in %d days. %d rows, %.2f MB",
				daysSince(t.LastModified, now), *t.TotalRows, sizeMB,
			),
			Suggestion: "Consider archiving or dropping if no longer needed",
		})
	}

	return recs
}

func isStale(lastModified *time.Time, now time.Time, threshold time.Duration) bool {
	if lastModified == nil {
		return true
	}
	return now.Sub(*lastModified) >= threshold
}

func daysSince(t *time.Time, now time.Time) int {
	if t == nil {
		return 999
	}
	return int(now.Sub(*t).Hours() / 24)
}
