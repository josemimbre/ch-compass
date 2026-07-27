package analyze

import "fmt"

// partitionThreshold is the active-partition count above which a table is
// flagged as over-partitioned, typically a sign of daily partitioning on a
// DateTime column where monthly would suffice.
const partitionThreshold = 100

// partitionStrategy flags non-view tables with more active partitions than
// partitionThreshold.
func partitionStrategy(tables []TableInfo, database string) []Recommendation {
	var recs []Recommendation

	for _, t := range tables {
		if t.IsView || t.PartitionCount <= partitionThreshold {
			continue
		}

		recs = append(recs, Recommendation{
			Type:       TypeOverPartitioned,
			Object:     t.Name,
			Database:   database,
			Severity:   SeverityHigh,
			Message:    fmt.Sprintf("%d active partitions (likely daily partitioning on a DateTime column)", t.PartitionCount),
			Suggestion: "Consider PARTITION BY toYYYYMM(column) instead of toYYYYMMDD(column)",
		})
	}

	return recs
}
