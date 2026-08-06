# ch-compass

A ClickHouse optimization recommendation engine, built with [Cobra](https://github.com/spf13/cobra).

It connects to a ClickHouse server, inspects `system.tables`, `system.parts`,
`system.query_log`, `system.query_views_log`, `system.mutations`, and
`system.data_skipping_indices`, and flags things worth looking at:

- **Over-partitioned tables** — more than 100 active partitions, usually
  daily partitioning on a DateTime column where monthly would do.
- **Cold tables** — no reads or writes in 60+ days despite holding data.
- **Stuck mutations** — incomplete mutations running longer than an hour.
- **Unused views** — regular views with no query activity in the analysis
  window.
- **Unused materialized views** — MVs never triggered by an insert into
  their source table. MVs sourced from a `system.*` table (e.g. a
  persisted copy of `system.query_log`) are excluded: ClickHouse's own
  system-table writes never go through the query pipeline that populates
  `system.query_views_log`, so this check can never observe their trigger
  activity either way.
- **Redundant skip indexes** — skip indexes on columns already covered by
  the table's sorting-key prefix.

## Build

```sh
go build -o bin/ch-compass ./cmd/ch-compass
```

## Run

```sh
go run ./cmd/ch-compass analyze --database mydb
go run ./cmd/ch-compass analyze --database mydb,other_db --format json
go run ./cmd/ch-compass analyze --database mydb --severity high --quiet
go run ./cmd/ch-compass analyze --all-databases --format html --output report.html
go run ./cmd/ch-compass analyze --database mydb --format md --output report.md
go run ./cmd/ch-compass analyze --database mydb --cluster my_cluster
```

## Permissions

ch-compass is read-only — it never executes the `DROP VIEW` / `ALTER TABLE
... DROP INDEX` / `KILL MUTATION` statements it suggests, only prints them.
It only ever runs `SELECT` queries plus `SYSTEM FLUSH LOGS`, so the
ClickHouse user it connects as only needs grants for those.

Minimum grants to analyze a database, with every check fully working:

```sql
GRANT SELECT ON mydb.* TO ch_compass;

GRANT SELECT ON system.tables TO ch_compass;
GRANT SELECT ON system.parts TO ch_compass;
GRANT SELECT ON system.mutations TO ch_compass;
GRANT SELECT ON system.data_skipping_indices TO ch_compass;
GRANT SELECT ON system.query_log TO ch_compass;
GRANT SELECT ON system.query_views_log TO ch_compass;
GRANT SYSTEM FLUSH LOGS ON *.* TO ch_compass;
```

`GRANT SELECT ON mydb.*` matters even though ch-compass never reads a row of
actual table data: ClickHouse filters `system.tables`/`system.parts`/etc. to
only the databases/tables the connecting user can see, so without it those
system tables look empty for `mydb` regardless of the `system.*` grants
above.

`--all-databases` additionally needs `GRANT SELECT ON system.databases TO
ch_compass` plus `SELECT` on every database it should discover (or just
`GRANT SELECT ON *.* TO ch_compass` for simplicity).

Every system table ch-compass reads is per-node, which matters on a
replicated/sharded server: connecting to a single host only sees that
host's slice. Pass `--cluster <name>` to read cluster-wide instead — it
needs the same grants as above readable on every node (typically identical
users/grants across the cluster already), plus `GRANT SYSTEM FLUSH LOGS ON
*.* TO ch_compass` to flush cluster-wide via `SYSTEM FLUSH LOGS ON CLUSTER
<name>`. Two different ClickHouse table functions are used depending on
what the underlying table holds:

- `system.query_log`/`system.query_views_log` hold **events** — a query or
  MV trigger is logged once, on whichever node handled it, never
  duplicated across replicas. Without `--cluster`, activity that landed on
  a different host than the one ch-compass is connected to won't show up,
  which can make a still-used view, materialized view, or table look
  unused/cold. `--cluster` reads these via `clusterAllReplicas(...)`,
  safely unioning activity from every replica of every shard.
- `system.tables`/`system.parts`/`system.mutations`/
  `system.data_skipping_indices` hold **state** — on a sharded table each
  shard holds a different slice of the data, but every replica of the same
  shard holds (near-)identical state. `--cluster` reads these via
  `cluster(...)` instead, one replica per shard, so row counts/sizes/part
  counts sum correctly across shards without double-counting replicas.

None of this is strictly required — `system.query_log`,
`system.query_views_log`, and `SYSTEM FLUSH LOGS` are all optional. Missing
any of them prints a `Note: ...` explaining what's degraded (e.g. "unused
view" detection becomes unreliable without `system.query_log`) and the run
still completes, rather than aborting. `system.tables`/`system.parts`/
`system.mutations`/`system.data_skipping_indices` are effectively required
in practice — without them the corresponding analyzer has nothing to work
from.

`system.query_log` and `system.query_views_log` also typically apply their
own TTL/rotation, independent of `--days` — if that TTL is shorter than the
requested window, table/view access, cold tables, and unused views/MVs all
silently lose accuracy: no data that far back looks exactly like no
activity that far back. ch-compass checks this once per run and prints a
`Note: ... only retains N day(s) of history ...` when retention falls short
of `--days`, so that blind spot doesn't pass for a clean result.

## Dev ClickHouse

`compose.yml` spins up a ClickHouse server seeded with sample tables/views
(over-partitioned table, cold tables, materialized views, ...) for trying
the tool out locally:

```sh
docker compose up -d
go run ./cmd/ch-compass analyze --database demo
docker compose down
```

### Flags

| Flag              | Default      | Description                                                               |
| ----------------- | ------------ | ------------------------------------------------------------------------- |
| `--host`          | `localhost`  | ClickHouse host                                                           |
| `-p, --port`      | `8123`       | ClickHouse HTTP port                                                      |
| `-d, --database`  | (required\*) | Database name(s), comma-separated                                         |
| `--all-databases` |              | Analyze every database (excludes system schemas), instead of `--database` |
| `-u, --user`      |              | ClickHouse username                                                       |
| `--password`      |              | ClickHouse password                                                       |
| `-f, --format`    | `text`       | Output format: `text`, `json`, `html`, or `md`                            |
| `-o, --output`    |              | Write output to a file instead of stdout (for `html`/`md`)                |
| `-s, --severity`  |              | Minimum severity to show: low, medium, high                               |
| `--days`          | `30`         | Analysis window in days                                                   |
| `-q, --quiet`     |              | Only print recommendations                                                |
| `--debug`         |              | Print SQL queries as they run                                             |
| `--secure`        |              | Use HTTPS/TLS for the ClickHouse connection                               |
| `--cluster`       |              | ClickHouse cluster name; widens every check cluster-wide instead of the connected node |

\* required unless `--all-databases` is passed.

Exit code is `0` when no recommendation survives the severity filter, `1`
otherwise (or on a connection/query error) — handy for CI gating.

## Layout

```
cmd/ch-compass/     main entrypoint
internal/cli/       command tree (root, version, analyze)
internal/ch/        ClickHouse connection + query wrapper
internal/analyze/   collectors + analyzers + the Recommendation model
internal/report/    text (terminal), JSON, HTML, and Markdown rendering
```

`internal/analyze` collects table/query/mutation/index metrics concurrently
for each database and runs every analyzer against them, returning one
`analyze.Result` per database. `internal/cli` and `internal/report` are the
only things that know about flags, exit codes, and how the output looks.
See [docs/ROADMAP.md](docs/ROADMAP.md) for ideas on what could be added
next.

## Add an analyzer

1. Add the rule as an unexported function in `internal/analyze/<name>.go`
   returning `[]Recommendation`. If it needs data no existing collector
   gathers, add a collector function too and wire it into the `errgroup` in
   `Database()`.
2. Call the rule from `Database()` in `internal/analyze/analyze.go`.
3. Add a human-readable label for the new `RecType` to `typeLabels` in
   `internal/report/text.go` (used by all four output formats).

## Version at build time

```sh
go build -ldflags "-X github.com/josemimbre/ch-compass/internal/cli.version=1.0.0" ./cmd/ch-compass
```
