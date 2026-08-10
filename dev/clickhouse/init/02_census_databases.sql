-- Empty database: appears in system.databases but has zero tables.
-- Covers: num_empty_databases.
CREATE DATABASE IF NOT EXISTS empty_db;

-- Has tables, but every one of them holds zero rows.
-- Covers: num_databases_all_empty (distinct from num_empty_databases above —
-- this database is NOT counted there, since it does have tables).
CREATE DATABASE IF NOT EXISTS mostly_empty_db;

CREATE TABLE mostly_empty_db.never_inserted (
    id UInt64,
    value String
) ENGINE = MergeTree()
ORDER BY id;
-- Deliberately no INSERT: zero rows, zero parts.

-- A plain view doesn't hold rows itself either, so this database still
-- counts as "all empty" even though it has 2 table-like objects.
CREATE VIEW mostly_empty_db.a_view AS
SELECT * FROM mostly_empty_db.never_inserted;
