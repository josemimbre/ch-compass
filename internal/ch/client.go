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
}

// Client executes queries against ClickHouse, optionally logging them.
// Safe for concurrent use: collectors run their queries in parallel.
type Client struct {
	conn   clickhouse.Conn
	debug  bool
	stderr io.Writer

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

	return &Client{conn: conn, debug: opts.Debug, stderr: stderr}, nil
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
