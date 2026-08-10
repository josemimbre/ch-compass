package census

import (
	"regexp"
	"strings"
	"testing"
)

func TestMVTargetRegexPattern(t *testing.T) {
	// Go's regexp package is RE2, the same engine ClickHouse uses for
	// extractGroups, so this is a faithful smoke test of the pattern text.
	// The pattern is embedded in a ClickHouse string literal with doubled
	// backslashes (`\\s` -> literal `\s` once ClickHouse parses the string),
	// so undo that doubling before compiling here.
	goPattern := strings.ReplaceAll(mvTargetRegexPattern, `\\`, `\`)
	re := regexp.MustCompile(goPattern)

	cases := []struct {
		name      string
		ddl       string
		wantMatch bool
		wantDB    string
		wantTable string
	}{
		{
			name:      "explicit TO",
			ddl:       "CREATE MATERIALIZED VIEW d_abc.mv_1 TO d_abc.t_target AS SELECT * FROM d_abc.landing",
			wantMatch: true,
			wantDB:    "d_abc",
			wantTable: "t_target",
		},
		{
			name:      "explicit TO with IF NOT EXISTS and backticks",
			ddl:       "CREATE MATERIALIZED VIEW IF NOT EXISTS `d_abc`.`mv_1` TO `d_abc`.`t_target` AS SELECT * FROM d_abc.landing",
			wantMatch: true,
			wantDB:    "d_abc",
			wantTable: "t_target",
		},
		{
			name:      "implicit target (no TO) must not false-match a later TO in the query body",
			ddl:       "CREATE MATERIALIZED VIEW d_abc.mv_1 AS SELECT * FROM d_abc.landing WHERE note = 'refers TO d_other.some_table'",
			wantMatch: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := re.FindStringSubmatch(tc.ddl)
			if tc.wantMatch && m == nil {
				t.Fatalf("expected a match, got none for: %s", tc.ddl)
			}
			if !tc.wantMatch {
				if m != nil {
					t.Fatalf("expected no match, but got %v for: %s", m, tc.ddl)
				}
				return
			}
			if m[1] != tc.wantDB || m[2] != tc.wantTable {
				t.Fatalf("got db=%q table=%q, want db=%q table=%q", m[1], m[2], tc.wantDB, tc.wantTable)
			}
		})
	}
}
