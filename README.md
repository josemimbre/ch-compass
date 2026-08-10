# ch-compass

A ClickHouse operational toolkit, built with [Cobra](https://github.com/spf13/cobra).
Two subcommands, one binary: `analyze` recommends optimizations for a
database, `census` collects cluster-wide inventory metrics for comparing
clusters over time.

## Build

```sh
go build -o bin/ch-compass ./cmd/ch-compass
```

## analyze

Connects to a ClickHouse server, inspects `system.tables`, `system.parts`,
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

### Run

```sh
go run ./cmd/ch-compass analyze --database mydb
go run ./cmd/ch-compass analyze --database mydb,other_db --format json
go run ./cmd/ch-compass analyze --database mydb --severity high --quiet
go run ./cmd/ch-compass analyze --all-databases --format html --output report.html
go run ./cmd/ch-compass analyze --database mydb --format md --output report.md
go run ./cmd/ch-compass analyze --database mydb --cluster my_cluster
```

### Permissions

`analyze` is read-only — it never executes the `DROP VIEW` / `ALTER TABLE
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

`GRANT SELECT ON mydb.*` matters even though `analyze` never reads a row of
actual table data: ClickHouse filters `system.tables`/`system.parts`/etc. to
only the databases/tables the connecting user can see, so without it those
system tables look empty for `mydb` regardless of the `system.*` grants
above.

`--all-databases` additionally needs `GRANT SELECT ON system.databases TO
ch_compass` plus `SELECT` on every database it should discover (or just
`GRANT SELECT ON *.* TO ch_compass` for simplicity).

Every system table `analyze` reads is per-node, which matters on a
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
  a different host than the one `analyze` is connected to won't show up,
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
activity that far back. `analyze` checks actual retention once per run
and, when it falls short of `--days`, prints a `Note: ... only retains N
day(s) of history ...` and caps table/view access, cold tables, and unused
views/MVs to that shorter window instead of confidently claiming the full
`--days`. Once retention is down to nothing usable (e.g. logging was just
enabled), the affected check is skipped outright for that database rather
than reporting "unused" from what's actually "no history to check".

## census

Collects cluster-wide inventory metrics — not tied to a single database,
unlike `analyze` — meant for comparing clusters against each other, or the
same cluster over time:

- **Database/table counts** — totals, per-engine breakdowns, max/min tables
  per database, and how many databases have zero tables.
- **Materialized view topology** — how many MVs have a resolvable `TO`
  target, how many tables are "landing" tables (receive direct inserts, not
  populated by an MV), how many MVs read directly from a landing table vs.
  from another MV's output, and fan-in (multiple MVs writing to the same
  target table — often a sign of orphaned MVs from old deploys).
- **Empty tables** — how many tables hold zero rows, cross-checked both via
  the cached `total_rows` column and independently via `system.parts`.
- **Replica desync** — per-replica table/MV/view counts, to spot a replica
  falling behind or diverging from the rest of the cluster.

### Run

```sh
go run ./cmd/ch-compass census --host localhost --user default
go run ./cmd/ch-compass census --host localhost --format json --output census.json
go run ./cmd/ch-compass census --host localhost --cluster my_cluster --format csv
```

- `--cluster` (optional): a ClickHouse cluster name registered in
  `system.clusters`. When set, adds per-replica desync metrics gathered via
  `clusterAllReplicas(...)`. Omit it to skip those and only collect
  cluster-wide metrics.
- `--format`: `text` (default, aligned table), `json`, or `csv`.
- `--output`: write to a file instead of stdout.

Every metric is `{metric, labels, value}` — `labels` is empty/omitted for
scalar metrics (e.g. `total_tables`). For a breakdown, `labels` is a map
keyed by what it identifies, so a row is self-describing without needing to
already know what each metric name means: `{"metric":
"table_count_by_engine", "labels": {"engine": "ReplicatedMergeTree"},
"value": 11065}`, or `{"metric": "replica_table_count", "labels": {"replica":
"ch1"}, "value": 13}`. In CSV, `labels` is that same map JSON-encoded into
one column (there's no native map type in CSV). This flat, long shape is
meant to be easy to feed into a time-series store for cross-cluster
comparison — nothing in `census` ships the data anywhere itself, it only
collects and prints/exports.

### Permissions

`census` is read-only — it only runs `SELECT` queries against
`system.databases`, `system.tables`, and `system.parts` (plus
`clusterAllReplicas` over `system.tables` when `--cluster` is set). Minimum
grants:

```sql
GRANT SELECT ON system.databases TO ch_compass;
GRANT SELECT ON system.tables TO ch_compass;
GRANT SELECT ON system.parts TO ch_compass;
```

## Dev ClickHouse

`compose.yml` spins up a two-node ClickHouse cluster (`ch1`, `ch2`, wired
into `test_cluster` via `dev/clickhouse/config/cluster.xml`) seeded with a
fixture covering both subcommands: `analyze`'s sample tables/views
(over-partitioned table, cold tables, materialized views, ...) and
`census`'s topology cases (empty databases, a database with tables but all
of them empty, a landing table feeding live materialized views — explicit
`TO`, implicit/inner-table target, and two MVs fanning into the same
target — a plain view, and empty tables with zero parts). `ch2`
additionally gets one extra table `ch1` doesn't have, so `--cluster
test_cluster` has a real, reproducible desync to detect. See
`dev/clickhouse/init/*.sql` for exactly which file covers which case.

A third service, `keeper` (ClickHouse Keeper), backs distributed DDL for
both nodes: `census`'s cluster-wide reads are plain `SELECT`s and don't
need it, but `analyze --cluster` also issues `SYSTEM FLUSH LOGS ON
CLUSTER`, which is DDL and requires a ZooKeeper/Keeper configured.

`--host localhost` with the default port always reaches `ch1` alone, so the
non-cluster examples above work unchanged against a single node:

```sh
docker compose up -d
go run ./cmd/ch-compass analyze --database demo
go run ./cmd/ch-compass census --host localhost
docker compose down -v
```

Pass `--cluster test_cluster` to either subcommand to exercise both nodes
at once:

```sh
docker compose up -d
go run ./cmd/ch-compass analyze --database demo --cluster test_cluster
go run ./cmd/ch-compass census --host localhost --cluster test_cluster
docker compose down -v
```

### Flags

`analyze`:

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

`census`:

| Flag           | Default     | Description                                                                     |
| -------------- | ----------- | -------------------------------------------------------------------------------- |
| `--host`       | `localhost` | ClickHouse host                                                                  |
| `-p, --port`   | `8123`      | ClickHouse HTTP port                                                             |
| `-u, --user`   |             | ClickHouse username                                                              |
| `--password`   |             | ClickHouse password                                                              |
| `-f, --format` | `text`      | Output format: `text`, `json`, or `csv`                                         |
| `-o, --output` |             | Write output to a file instead of stdout                                        |
| `--debug`      |             | Print SQL queries as they run                                                   |
| `--secure`     |             | Use HTTPS/TLS for the ClickHouse connection                                     |
| `--cluster`    |             | ClickHouse cluster name (`system.clusters`); enables per-replica desync metrics |

Exit code is `0` on success, `1` on a connection/query/render error.

## Layout

```
cmd/ch-compass/         main entrypoint
internal/cli/           command tree (root, version, analyze, census)
internal/ch/            ClickHouse connection + query wrapper (shared)
internal/analyze/       collectors + analyzers + the Recommendation model
internal/analyze/report/  text (terminal), JSON, HTML, and Markdown rendering for analyze
internal/census/        cluster-wide inventory collectors + the Metric model
internal/census/report/  text (terminal), JSON, and CSV rendering for census
```

`internal/analyze` collects table/query/mutation/index metrics concurrently
for each database and runs every analyzer against them, returning one
`analyze.Result` per database. `internal/census` collects cluster-wide
metrics in one pass, returning `[]census.Metric`. `internal/cli` and the two
`report` packages are the only things that know about flags, exit codes,
and how the output looks. See [docs/ROADMAP.md](docs/ROADMAP.md) for ideas
on what could be added next.

## Add an analyzer

1. Add the rule as an unexported function in `internal/analyze/<name>.go`
   returning `[]Recommendation`. If it needs data no existing collector
   gathers, add a collector function too and wire it into the `errgroup` in
   `Database()`.
2. Call the rule from `Database()` in `internal/analyze/analyze.go`.
3. Add a human-readable label for the new `RecType` to `typeLabels` in
   `internal/analyze/report/text.go` (used by all four output formats).

## Version at build time

```sh
go build -ldflags "-X github.com/josemimbre/ch-compass/internal/cli.version=1.0.0" ./cmd/ch-compass
```
