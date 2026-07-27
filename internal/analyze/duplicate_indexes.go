package analyze

import (
	"fmt"
	"strings"
)

// duplicateIndexes flags skip indexes whose columns are already covered by
// their table's sorting-key prefix, since the sparse primary index already
// prunes granules for those columns — the skip index wastes disk space and
// slows down merges for no query benefit.
func duplicateIndexes(tables []TableInfo, indexes []SkipIndex, database string) []Recommendation {
	sortingKeys := sortingKeyColumns(tables)

	var recs []Recommendation
	for _, idx := range indexes {
		keyColumns, ok := sortingKeys[idx.Table]
		if !ok || !coveredBy(idx.Expr, keyColumns) {
			continue
		}

		recs = append(recs, Recommendation{
			Type:     TypeDuplicateIndex,
			Object:   fmt.Sprintf("%s.%s", idx.Table, idx.Name),
			Database: database,
			Severity: SeverityLow,
			Message: fmt.Sprintf(
				"Skip index '%s' on '%s' is redundant — column is already in the sorting key of table '%s'",
				idx.Name, idx.Expr, idx.Table,
			),
			Suggestion: fmt.Sprintf("ALTER TABLE %s.%s DROP INDEX %s", database, idx.Table, idx.Name),
		})
	}
	return recs
}

func sortingKeyColumns(tables []TableInfo) map[string][]string {
	keys := make(map[string][]string)
	for _, t := range tables {
		if t.IsView || t.SortingKey == "" {
			continue
		}
		keys[t.Name] = splitColumns(t.SortingKey)
	}
	return keys
}

// coveredBy reports whether every column in expr also appears in keyColumns.
func coveredBy(expr string, keyColumns []string) bool {
	inKey := make(map[string]bool, len(keyColumns))
	for _, c := range keyColumns {
		inKey[c] = true
	}

	for _, c := range splitColumns(expr) {
		if !inKey[c] {
			return false
		}
	}
	return true
}

func splitColumns(s string) []string {
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}
