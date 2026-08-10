# Merging ch-census into ch-compass

Status: proposed, not started. Written after comparing both repos
file-by-file; nothing below is implemented yet.

## Why

`ch-compass` and `ch-census` are two independent repos, but they're not
independent tools — same author, same domain (read-only ClickHouse
operational tooling), same stack (Cobra + `clickhouse-go` + `lipgloss`),
and a large fraction of each repo is boilerplate duplicated verbatim in
the other:

- **`internal/ch/client.go`** — census's version is a strict subset of
  compass's. Compass's has cluster support (`AllReplicasSource`,
  `ShardedSource`, `FlushLogs`), `Exec`, `Named`, and `ExceptionCode`;
  census's has none of that. They're not "similar", one is an older
  version of the other.
- **Connection flags** — `--host`, `-p/--port`, `-u/--user`,
  `--password`, `--secure`, `--debug`, `--cluster`, `-f/--format`,
  `-o/--output` are defined with near-identical help text and the same
  `RunE` pattern (connect → handle error → run → write to stdout/file →
  exit code via `SilenceErrors`) in both `internal/cli/analyze.go` and
  `internal/cli/census.go`.
- **`internal/cli/root.go` / `version.go`** — same skeleton, only the
  `Use`/`Short`/`Long` strings differ.
- **`Makefile` and `.goreleaser.yaml`** — identical except for the binary
  name.

What's genuinely different and should stay that way:

- The analyzers/collectors (`internal/analyze/*` vs `internal/census/*`)
  are unrelated business logic — nothing to share beyond both taking a
  `*ch.Client`.
- The `report` packages have incompatible signatures already
  (`WriteJSON(w, results, version, severity)` vs `WriteJSON(w, metrics)`)
  and non-overlapping formats (html/md only in compass, csv only in
  census). These should stay as separate subpackages, not be forced into
  one.

## Decision

Rather than inventing a new umbrella name, `ch-census` is absorbed into
`ch-compass` as a second subcommand. The `ch-compass` repo, module
(`github.com/josemimbre/ch-compass`), and binary name all stay as they
are — `ch-census` is retired as a separate repo/binary:

```sh
ch-compass analyze --database mydb ...
ch-compass census --cluster my_cluster ...
```

(Considered, rejected: a new shared name like `ch-tools` for both. No
benefit here — `ch-compass` already has the more mature `internal/ch`,
an existing user base, and an existing release history; renaming it
would just churn install instructions for no functional gain.)

(Also considered, rejected: keeping two separate binaries —
`cmd/ch-compass` and `cmd/ch-census` — in one monorepo. A single binary
means one install, one release artifact, and one root command surface,
at no real cost since the two subcommands don't compete for flag names.)

## Target layout (inside the existing ch-compass repo)

```
cmd/ch-compass/main.go
internal/cli/            root.go (Short/Long updated to cover both),
                          version.go, analyze.go, census.go
                          + shared connection-flags helper
internal/ch/              client.go (unchanged — already the superset)
internal/analyze/         unchanged
internal/analyze/report/  unchanged (report/ moved under it)
internal/census/          moved from ch-census, unchanged
internal/census/report/   moved from ch-census, unchanged
```

## Migration steps

1. Copy `internal/census/` and `internal/report/` from `ch-census` into
   `ch-compass` as `internal/census/` and `internal/census/report/`
   (renaming the import path from `github.com/josemimbre/ch-census` to
   `github.com/josemimbre/ch-compass` throughout). Move `ch-compass`'s
   own `internal/report/` to `internal/analyze/report/` first, so
   nothing collides on the `report` package name.
2. Delete `ch-census`'s `internal/ch/client.go` — census's collectors
   now use `ch-compass`'s version unchanged, gaining cluster support and
   `ExceptionCode` for free.
3. Add `newCensusCmd()` (ported from `ch-census`'s `internal/cli/census.go`)
   as a second subcommand registered in `ch-compass`'s
   `internal/cli/root.go`, alongside the existing `newAnalyzeCmd()`.
   Factor the repeated connection-flag registration and
   connect/run/exit-code boilerplate into one shared helper instead of
   duplicating it a third time.
4. Update `root.go`'s `Short`/`Long` to describe both subcommands instead
   of just analyze. Leave `Use: "ch-compass"` and `version.go` as-is.
5. Merge `go.mod`/`go.sum` (align on Go 1.26.5, the higher of the two —
   census's deps are already a subset of compass's).
6. Fold `ch-census`'s `README.md` content into `ch-compass`'s (a
   `## census` section alongside the existing analyze docs), merge
   `compose.yml`/`dev/` fixtures, and merge the `.github/` workflows
   (test, zizmor, release) — `ch-compass`'s versions win where they
   differ, since they're the ones staying.
7. Archive the `ch-census` repo (or leave it read-only) once the last
   tagged `ch-compass` release ships with `census` built in.

## Open questions

- Backward compatibility: does anything invoke `ch-census` by name
  (scripts, CI, docs elsewhere)? If so, worth a deprecation note in
  `ch-census`'s README pointing at `ch-compass census` before archiving.
- Whether `census`'s `collect` subcommand name should be renamed to
  `census` for symmetry with `analyze`, or kept as `collect`.
