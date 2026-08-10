-- Only mounted into ch2 (see compose.yml) — deliberately NOT run on ch1, so
-- the two "replicas" end up with a different table count. Covers:
-- replica_table_count desync, meant to be inspected via
-- `ch-census collect --cluster test_cluster`.
CREATE DATABASE IF NOT EXISTS main;

CREATE TABLE main.only_on_ch2 (
    id UInt64
) ENGINE = MergeTree()
ORDER BY id;
