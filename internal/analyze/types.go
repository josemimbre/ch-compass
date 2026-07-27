// Package analyze collects ClickHouse table, query, and mutation metrics
// and turns them into optimization recommendations.
package analyze

import (
	"fmt"
	"time"
)

// ViewType classifies the kind of view a table represents, empty for plain
// tables.
type ViewType string

const (
	ViewTypeNone         ViewType = ""
	ViewTypeRegular      ViewType = "regular"
	ViewTypeMaterialized ViewType = "materialized"
	ViewTypeLive         ViewType = "live"
)

// TableInfo holds table metadata, size, and partition info.
type TableInfo struct {
	Name           string
	Engine         string
	TotalRows      *uint64
	TotalBytes     *uint64
	PartitionCount uint64
	PartCount      uint64
	LastModified   *time.Time
	IsView         bool
	ViewType       ViewType
	SortingKey     string
}

// TableAccess holds per-table access frequency and read statistics.
type TableAccess struct {
	Name           string
	QueryCount     uint64
	LastAccessed   *time.Time
	TotalReadRows  uint64
	TotalReadBytes uint64
}

// MutationInfo holds mutation metadata and completion state.
type MutationInfo struct {
	Table      string
	MutationID string
	Command    string
	CreateTime time.Time
	PartsToDo  []string
	IsDone     bool
}

// SkipIndex holds skip index metadata from system.data_skipping_indices.
type SkipIndex struct {
	Table string
	Name  string
	Type  string
	Expr  string
}

// Severity is the priority level of a Recommendation.
type Severity string

const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

var severityRank = map[Severity]int{
	SeverityLow:    0,
	SeverityMedium: 1,
	SeverityHigh:   2,
}

// ParseSeverity validates a user-supplied severity string.
func ParseSeverity(s string) (Severity, error) {
	sev := Severity(s)
	if _, ok := severityRank[sev]; !ok {
		return "", fmt.Errorf("invalid severity %q, must be low, medium, or high", s)
	}
	return sev, nil
}

// RecType identifies the kind of issue a Recommendation reports.
type RecType string

const (
	TypeOverPartitioned        RecType = "over_partitioned"
	TypeColdTable              RecType = "cold_table"
	TypeStuckMutation          RecType = "stuck_mutation"
	TypeUnusedView             RecType = "unused_view"
	TypeUnusedMaterializedView RecType = "unused_materialized_view"
	TypeDuplicateIndex         RecType = "duplicate_index"
)

// Recommendation is a single optimization finding for a database object.
type Recommendation struct {
	Type       RecType  `json:"type"`
	Object     string   `json:"object"`
	Database   string   `json:"database"`
	Severity   Severity `json:"severity"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion"`
}

// Result is the analysis outcome for a single database.
type Result struct {
	Database        string
	Tables          []TableInfo
	Views           []TableInfo
	Recommendations []Recommendation
}

// Filter keeps only the recommendations at or above min. A nil min returns
// recs unchanged.
func Filter(recs []Recommendation, min *Severity) []Recommendation {
	if min == nil {
		return recs
	}

	minRank := severityRank[*min]
	filtered := make([]Recommendation, 0, len(recs))
	for _, rec := range recs {
		if severityRank[rec.Severity] >= minRank {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}
