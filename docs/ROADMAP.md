# Roadmap

Ideas for what could be added next. Nothing here is implemented — this is
a list of directions, not a spec. Most of these originate from the
original Elixir `compass` project's own design doc (phases it planned but
never built either); a few are Go-specific opportunities that came up
while porting.

## New analyzers

- **Column compression** — use `system.columns` to flag low-cardinality
  string columns still on the default `LZ4` codec where `ZSTD` or
  `LowCardinality` would save real space.
- **Merge pressure** — flag tables with many small active parts (e.g.
  >300) that indicate merge lag, or unusually large parts that risk memory
  pressure during merges. Data source: `system.parts`, already collected
  by `tableStats` as `TableInfo.PartCount` — no new collector needed, this
  is analyzer-logic-only. Worth a `high` severity tier as active parts
  approach ~5,000, the point where ZooKeeper's `jute.maxbuffer` starts
  rejecting ALTERs on `Replicated*MergeTree` tables and queries slow down
  noticeably ([Altinity KB](https://kb.altinity.com/altinity-kb-schema-design/how-much-is-too-much/)).
- **Materialized views per source table** — flag source tables with many
  MVs attached (Altinity KB: "up to a few" is optimal). Each MV re-runs its
  query on every insert into the source table, so a source table with a
  dozen MVs multiplies insert cost and adds inconsistency risk if one MV
  lags or fails. Data source: `system.tables` (MV `engine_full`/dependency
  info already exposed there) — extends the existing
  `unusedMaterializedViews` collector rather than needing a new one.
- **Secondary index overload** — flag tables with many skip indexes (KB:
  roughly one to a dozen is normal) or multiple `bloom_filter` indexes in
  particular, since a bloom filter index costs ~100x more to
  build/maintain than a `minmax` one. Data source: `system.data_skipping_indices`,
  already collected by `indexStats`.
- **Wide tables** — flag tables with hundreds-to-thousands of columns;
  KB puts "a few hundred" as fine and "thousands" as degrading insert/merge
  performance and RAM usage. Needs a new `system.columns`-based collector
  (`count() GROUP BY table`).
- **Slow queries** — flag queries in `system.query_log` with a high
  `read_rows`/`read_bytes`-to-result-size ratio, or that exceed a duration
  threshold. Suggest a projection or materialized view.
- **TTL misconfiguration** — cross-reference `system.tables` TTL
  expressions against actual data ranges in `system.parts`. Flag tables
  with a TTL that never expires anything, or growing old data with no TTL
  set at all.

## Robustness & UX

- **Connection timeout / retry** — a `--timeout` flag and brief
  backoff-retry on transient network errors instead of failing
  immediately on the first connection attempt.
- **Permission-aware degradation** — if a `system.*` table is
  unreadable for the given user, skip just the dependent collector/analyzer
  and warn about what's disabled, instead of failing the whole run. The
  `query_views_log`-missing fallback in `queryPatterns` is the existing
  pattern to extend.
- **Progress indicator** — a spinner or per-database progress line while
  collectors run, useful for `--all-databases` against a server with many
  databases.
- **`--include` / `--exclude` table filters** — scope analysis to a
  subset of tables (e.g. `--include "events_*"`), useful on databases with
  hundreds of tables where only a few matter.

## Performance

- **Parallelize `--all-databases`** — `internal/analyze.Database()`
  already collects a single database's four metric sets concurrently via
  `errgroup`; the *outer* loop over databases in `runAnalyze`
  (`internal/cli/analyze.go`) is still sequential. Worth an `errgroup` (or
  a bounded worker pool) there too if `--all-databases` becomes the common
  path against servers with many databases — watch out for
  interleaved/garbled non-`--quiet` text output if attempted, since
  `report.WriteText` currently assumes it's the only writer to stdout.

## Code quality

- **Integration tests** — a test suite that runs against a real
  ClickHouse instance (e.g. via `testcontainers-go`), gated behind a build
  tag or `-short` skip, to catch SQL/version compatibility issues that
  pure unit tests on fixtures can't. Currently only the analyzer logic and
  report rendering have unit tests; the collector queries and `internal/ch`
  are exercised manually against `compose.yml` rather than in CI.
