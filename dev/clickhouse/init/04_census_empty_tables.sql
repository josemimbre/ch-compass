-- Empty table with zero active parts: created but never inserted into.
-- Covers: num_tables_zero_parts.
CREATE TABLE main.never_touched (
    id UInt64,
    note String
) ENGINE = MergeTree()
ORDER BY id;

-- Attempts the "has parts, but they sum to zero rows" case: insert, then
-- delete everything via a synchronous mutation. In practice ClickHouse
-- tends to drop a part entirely once a mutation empties it out completely,
-- rather than keeping a zero-row part around — so this table usually still
-- ends up counted under num_tables_zero_parts rather than
-- num_tables_zero_rows_with_parts. Kept here as a documented, honest
-- best-effort attempt rather than a guaranteed repro of that distinction;
-- if your ClickHouse version/settings behave differently you may see it
-- land in the other bucket instead.
CREATE TABLE main.fully_deleted (
    id UInt64,
    note String
) ENGINE = MergeTree()
ORDER BY id;

INSERT INTO main.fully_deleted SELECT number, 'to be deleted' FROM numbers(500);

ALTER TABLE main.fully_deleted DELETE WHERE 1 SETTINGS mutations_sync = 1;
