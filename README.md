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
  their source table.
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
```

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
