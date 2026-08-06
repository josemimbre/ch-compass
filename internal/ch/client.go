// Package ch wraps the ClickHouse driver with connection setup,
// debug query logging, and a custom user-agent so ch-compass can filter its
// own queries out of system.query_log.
package ch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// UserAgent is sent on every HTTP request to ClickHouse so ch-compass's own
// queries can be excluded when reading system.query_log.
const UserAgent = "ch-compass"

// Options configures a ClickHouse connection.
type Options struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	Secure   bool
	Debug    bool
	// Cluster is the ClickHouse cluster name this connection belongs to,
	// if any. When set, Client.AllReplicasSource, Client.ShardedSource,
	// and Client.FlushLogs widen reads of per-node system tables to every
	// host in the cluster instead of just this one — see those methods
	// for why that matters and how they differ.
	Cluster string
}

// Client executes queries against ClickHouse, optionally logging them.
// Safe for concurrent use: collectors run their queries in parallel.
type Client struct {
	conn    clickhouse.Conn
	debug   bool
	stderr  io.Writer
	cluster string

	logMu sync.Mutex
}

// Connect opens a connection pool to ClickHouse and verifies it is reachable.
func Connect(ctx context.Context, opts Options, stderr io.Writer) (*Client, error) {
	chOpts := &clickhouse.Options{
		Addr:     []string{fmt.Sprintf("%s:%d", opts.Host, opts.Port)},
		Protocol: clickhouse.HTTP,
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.Username,
			Password: opts.Password,
		},
		HttpHeaders: map[string]string{"User-Agent": UserAgent},
	}

	if opts.Secure {
		chOpts.TLS = &tls.Config{}
	}

	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(ctx); err != nil {
		conn.Close()
		return nil, err
	}

	return &Client{conn: conn, debug: opts.Debug, stderr: stderr, cluster: opts.Cluster}, nil
}

// Close releases the underlying connection pool.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Version returns the ClickHouse server version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	c.logQuery("SELECT version()", nil)

	var version string
	if err := c.conn.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		return "", err
	}

	return version, nil
}

// Select runs query and scans all result rows into dest, a pointer to a
// slice of structs tagged with `ch:"column_name"`.
func (c *Client) Select(ctx context.Context, dest any, query string, args ...any) error {
	c.logQuery(query, args)
	return c.conn.Select(ctx, dest, query, args...)
}

// Exec runs a statement that returns no rows (e.g. SYSTEM FLUSH LOGS).
func (c *Client) Exec(ctx context.Context, query string, args ...any) error {
	c.logQuery(query, args)
	return c.conn.Exec(ctx, query, args...)
}

func (c *Client) logQuery(query string, args []any) {
	if !c.debug {
		return
	}
	c.logMu.Lock()
	defer c.logMu.Unlock()
	if len(args) > 0 {
		fmt.Fprintf(c.stderr, "\n[DEBUG] %s -- args: %v\n", query, args)
	} else {
		fmt.Fprintf(c.stderr, "\n[DEBUG] %s\n", query)
	}
}

// Named binds value to a {name:Type} placeholder in a query passed to
// Select or Exec.
func Named(name string, value any) any {
	return clickhouse.Named(name, value)
}

// AllReplicasSource returns the FROM-clause source for a per-node event
// log, such as system.query_log or system.query_views_log. Those tables
// hold one row per event on whichever node happened to handle it — never
// duplicated across replicas — so on a replicated/sharded table the insert
// that triggers a materialized view (or the query that reads a
// table/view) may land on a different host in the cluster than the one
// this Client is connected to, making an MV/table/view look unused when it
// isn't. When the Client was connected with a Cluster, this wraps table in
// clusterAllReplicas so activity on every replica of every shard counts —
// safe to sum/union since no event is ever double-counted. Otherwise it
// returns table unqualified.
func (c *Client) AllReplicasSource(table string) string {
	if c.cluster == "" {
		return table
	}
	return "clusterAllReplicas(" + SQLStringLiteral(c.cluster) + ", " + table + ")"
}

// ShardedSource returns the FROM-clause source for a per-node state table,
// such as system.tables, system.parts, system.mutations, or
// system.data_skipping_indices. Those reflect locally-held state: on a
// sharded table each shard holds a different slice of the data, but every
// replica of the same shard holds (near-)identical state. Widening via
// AllReplicasSource/clusterAllReplicas would therefore double- (or
// triple-, ...-) count each shard once per replica. When the Client was
// connected with a Cluster, this instead reads one replica per shard via
// the cluster table function, so results can be summed across shards
// without duplicating replicas. Otherwise it returns table unqualified.
func (c *Client) ShardedSource(table string) string {
	if c.cluster == "" {
		return table
	}
	return "cluster(" + SQLStringLiteral(c.cluster) + ", " + table + ")"
}

// FlushLogs flushes ClickHouse's buffered system logs (query_log,
// query_views_log, ...) so a subsequent read of them reflects recent
// activity. When the Client was connected with a Cluster, the flush runs
// ON CLUSTER so every node's buffered entries are visible before a
// ClusterSource-wrapped query reads them.
func (c *Client) FlushLogs(ctx context.Context) error {
	query := "-- Force lazy system tables to be created and flush buffered log entries\nSYSTEM FLUSH LOGS"
	if c.cluster != "" {
		query += " ON CLUSTER " + QuoteIdentifier(c.cluster)
	}
	return c.Exec(ctx, query)
}

// SQLStringLiteral quotes s as a single-quoted SQL string literal, escaping
// embedded single quotes. Used for values ClickHouse doesn't accept as a
// bind parameter, such as the cluster name argument to clusterAllReplicas.
func SQLStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// QuoteIdentifier backtick-quotes s as a ClickHouse identifier, escaping
// embedded backticks. Used for values ClickHouse doesn't accept as a bind
// parameter, such as the cluster name in ON CLUSTER.
func QuoteIdentifier(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// exceptionCodePattern matches the "Code: N." prefix ClickHouse puts at the
// start of every plaintext exception body, e.g.
// `Code: 497. DB::Exception: ... (ACCESS_DENIED)`.
var exceptionCodePattern = regexp.MustCompile(`Code: (\d+)\.`)

// ExceptionCode returns the ClickHouse error code carried by err, if any.
//
// Over the native protocol the driver decodes a structured *clickhouse.Exception,
// but over HTTP (what Connect uses) it never does — client errors just wrap
// the raw response body as a plain string. So this also falls back to
// parsing the "Code: N." prefix ClickHouse always puts at the start of that
// body.
func ExceptionCode(err error) (int32, bool) {
	if err == nil {
		return 0, false
	}

	var exc *clickhouse.Exception
	if errors.As(err, &exc) {
		return exc.Code, true
	}

	if m := exceptionCodePattern.FindStringSubmatch(err.Error()); m != nil {
		if code, parseErr := strconv.ParseInt(m[1], 10, 32); parseErr == nil {
			return int32(code), true
		}
	}

	return 0, false
}
