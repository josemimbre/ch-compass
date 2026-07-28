# Code Review — ch-compass

Full review of the project (~1700 lines, 20 Go files). `go vet ./...`,
`gofmt -l .`, and `go build ./...` report nothing. No concurrency or SQL
injection issues were found: the goroutines in `analyze.go` don't share
state, and every query uses bind params (`{name:Type}`), with no string
concatenation of user input.

## High priority

### 1. Connection leak on failed ping — ✅ fixed
[../internal/ch/client.go:59-66](../internal/ch/client.go#L59-L66)

`clickhouse.Open` creates the connection pool, and if the subsequent `Ping`
fails, `conn` is never closed before returning the error. Every failed
connection attempt leaves the pool/goroutines alive.

```go
conn, err := clickhouse.Open(chOpts)
if err != nil {
    return nil, err
}

if err := conn.Ping(ctx); err != nil {
    conn.Close() // missing
    return nil, err
}
```

### 2. The "cold table" threshold isn't tied to `--days` — ✅ fixed
[../internal/analyze/cold_tables.go:10](../internal/analyze/cold_tables.go#L10)

`coldThreshold` was hardcoded to 60 days, but the access window that
populates `accesses` uses the `--days` flag (default **30**,
[../internal/cli/analyze.go:84](../internal/cli/analyze.go#L84)). With the
default, a table queried 31-59 days ago won't show up in `accesses`, yet it
still gets flagged cold once `LastModified` crosses 60 days → a false
positive baked into the default configuration.

Suggested fix: derive `coldThreshold` from `days`, or require `days >= 60`
for cold-table analysis.

### 3. View-usage detection only matched the fully-qualified `database.view` name — ✅ fixed
[../internal/analyze/query_patterns.go](../internal/analyze/query_patterns.go)

`collectRegularViewAccess` did a substring match of `database.view` against
the raw query text. If queries used `USE database` and referenced the view
unqualified (a very common pattern), it wouldn't match, and a view that's
actually in use ended up recommended for archiving/dropping.

Fixed by matching a word-boundary regex against either the bare view name
or the qualified `database.view` form. Bare matches are additionally scoped
to queries where `has(databases, {database:String})` is true, so a
same-named view/table in a completely unrelated database can't be
conflated with this one. Verified against a live ClickHouse instance: an
unqualified `SELECT ... FROM user_stats` (run with `database=demo` as
session context) now correctly marks `user_stats` as used, where it
previously stayed flagged as unused.

### 4. Inconsistent degradation when system tables are restricted
[../internal/analyze/query_patterns.go](../internal/analyze/query_patterns.go)

Only `collectMaterializedViewActivity` (lines 196-208) gracefully handles a
missing/restricted system table via `ExceptionCode`. `SYSTEM FLUSH LOGS`
(line 45) and `collectTableAccess`/`collectRegularViewAccess` (which need
`system.query_log`) have no equivalent fallback — a user without the flush
logs privilege aborts the entire `analyze` run instead of degrading the way
the MV path does.

## Medium priority

### 5. No validation on `--days`
[../internal/cli/analyze.go:84](../internal/cli/analyze.go#L84), used
directly as `{days:UInt32}` in several queries. A value of 0 or negative
fails obscurely inside the driver/ClickHouse instead of a clear CLI error.

### 6. `-v/--verbose` is a no-op
Declared and bound in
[../internal/cli/root.go:13,24](../internal/cli/root.go) but never read
anywhere else in the code (confirmed via grep). Either wire it up or remove
it.

### 7. `os.Exit` called from inside `RunE` — ✅ fixed
[../internal/cli/analyze.go:70-71](../internal/cli/analyze.go#L70-L71).
Bypasses cobra's own error handling and leaves an unreachable `return nil`
right after it. Prefer returning an error/exit code from `RunE` and exiting
in `main.go`.

### 8. Test coverage gaps
No tests for `internal/analyze/analyze.go`, `databases.go`,
`mutation_stats.go`, `table_stats.go`, the pure helpers in
`query_patterns.go` (`mergeAccess`, `shortName`), `internal/report/json.go`,
or all of `internal/cli` (e.g. `splitTrimmed`, the mutual-exclusion
validation in `runAnalyze`), despite containing cheaply-testable pure logic.

## Nitpicks

- **9.** `--password`
  ([../internal/cli/analyze.go:81](../internal/cli/analyze.go#L81)) is
  plaintext-only, with no environment-variable fallback — exposed in shell
  history / `ps`.
- **10.** `defer client.Close()`
  ([../internal/cli/analyze.go:129](../internal/cli/analyze.go#L129))
  silently discards its error.
- **11.** `mergeAccess`
  ([../internal/analyze/query_patterns.go:239-265](../internal/analyze/query_patterns.go#L239-L265))
  returns in map-iteration order — non-deterministic, harmless today but
  fragile if it's ever rendered directly at some point.
