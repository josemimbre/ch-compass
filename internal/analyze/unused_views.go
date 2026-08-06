package analyze

import (
	"fmt"
	"io"
)

// unusedViews flags regular views with no matching entry in accesses,
// meaning no query referenced them in the last `days` days.
func unusedViews(tables []TableInfo, accesses []TableAccess, database string, days int) []Recommendation {
	return unusedViewsOfType(tables, accesses, database, days, ViewTypeRegular, TypeUnusedView,
		"View '%s' has not been accessed in the last %d days")
}

// unusedMaterializedViews flags materialized views with no matching entry
// in accesses, meaning no insert into their source table triggered them in
// the last `days` days. Materialized views in systemSourced (see
// systemSourcedMaterializedViews) are excluded entirely rather than
// checked: their trigger activity structurally never reaches
// system.query_views_log, so this check would always be a false positive
// for them. Excluding them is noted, so it reads as "excluded" rather than
// "confirmed fine".
func unusedMaterializedViews(tables []TableInfo, accesses []TableAccess, database string, days int, systemSourced []string, notes io.Writer) []Recommendation {
	excluded := make(map[string]bool, len(systemSourced))
	for _, name := range systemSourced {
		excluded[name] = true
	}

	checked := make([]TableInfo, 0, len(tables))
	skipped := 0
	for _, t := range tables {
		if t.ViewType == ViewTypeMaterialized && excluded[t.Name] {
			skipped++
			continue
		}
		checked = append(checked, t)
	}
	if skipped > 0 {
		fmt.Fprintf(notes, "Note: %d materialized view(s) skipped for unused-materialized-view detection (sourced from a system.* table, so system.query_views_log can never show their trigger activity).\n", skipped)
	}

	return unusedViewsOfType(checked, accesses, database, days, ViewTypeMaterialized, TypeUnusedMaterializedView,
		"Materialized view '%s' has not been triggered in the last %d days")
}

func unusedViewsOfType(tables []TableInfo, accesses []TableAccess, database string, days int, viewType ViewType, recType RecType, messageFormat string) []Recommendation {
	accessed := make(map[string]bool, len(accesses))
	for _, a := range accesses {
		accessed[a.Name] = true
	}

	var recs []Recommendation
	for _, t := range tables {
		if t.ViewType != viewType || accessed[t.Name] {
			continue
		}

		recs = append(recs, Recommendation{
			Type:       recType,
			Object:     t.Name,
			Database:   database,
			Severity:   SeverityMedium,
			Message:    fmt.Sprintf(messageFormat, t.Name, days),
			Suggestion: fmt.Sprintf("DROP VIEW IF EXISTS %s.%s", database, t.Name),
		})
	}
	return recs
}
