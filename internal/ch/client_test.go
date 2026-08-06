package ch

import "testing"

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "prod", "`prod`"},
		{"embedded backtick", "pro`d", "`pro``d`"},
		{"empty", "", "``"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QuoteIdentifier(tt.in); got != tt.want {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSQLStringLiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "prod", "'prod'"},
		{"embedded single quote", "o'brien", "'o''brien'"},
		{"empty", "", "''"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SQLStringLiteral(tt.in); got != tt.want {
				t.Errorf("SQLStringLiteral(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestClientAllReplicasSource(t *testing.T) {
	t.Run("no cluster returns table unqualified", func(t *testing.T) {
		c := &Client{}
		if got := c.AllReplicasSource("system.query_log"); got != "system.query_log" {
			t.Errorf("got %q, want %q", got, "system.query_log")
		}
	})

	t.Run("with cluster wraps in clusterAllReplicas", func(t *testing.T) {
		c := &Client{cluster: "prod"}
		want := "clusterAllReplicas('prod', system.query_log)"
		if got := c.AllReplicasSource("system.query_log"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestClientShardedSource(t *testing.T) {
	t.Run("no cluster returns table unqualified", func(t *testing.T) {
		c := &Client{}
		if got := c.ShardedSource("system.parts"); got != "system.parts" {
			t.Errorf("got %q, want %q", got, "system.parts")
		}
	})

	t.Run("with cluster wraps in cluster()", func(t *testing.T) {
		c := &Client{cluster: "prod"}
		want := "cluster('prod', system.parts)"
		if got := c.ShardedSource("system.parts"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("cluster name with embedded quote is escaped, not injected", func(t *testing.T) {
		c := &Client{cluster: "o'brien"}
		want := "cluster('o''brien', system.parts)"
		if got := c.ShardedSource("system.parts"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
