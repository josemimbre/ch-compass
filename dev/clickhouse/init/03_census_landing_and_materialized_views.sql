CREATE DATABASE IF NOT EXISTS main;

-- Landing table: receives direct inserts, not populated by any MV.
-- Covers: num_landing_tables.
CREATE TABLE main.events (
    event_date Date,
    event_time DateTime,
    user_id UInt64,
    event_type String
) ENGINE = MergeTree()
ORDER BY (event_type, event_time);

INSERT INTO main.events
SELECT
    today() - number % 30,
    now() - number * 60,
    rand() % 1000,
    arrayElement(['click', 'view', 'purchase'], (number % 3) + 1)
FROM numbers(2000);

-- MV with an explicit TO target, reading directly from the landing table.
-- Covers: num_mv_targets, mvs_linked_to_landing.
CREATE TABLE main.daily_counts_store (
    event_date Date,
    event_type String,
    cnt UInt64
) ENGINE = SummingMergeTree()
ORDER BY (event_date, event_type);

CREATE MATERIALIZED VIEW main.daily_counts TO main.daily_counts_store AS
SELECT event_date, event_type, count() AS cnt
FROM main.events
GROUP BY event_date, event_type;

-- MV with an implicit target (no TO clause): ClickHouse creates a hidden
-- `.inner_id.*` storage table for it instead. Covers: the "landing tables
-- must exclude .inner% tables" and "MVTargetCount only counts explicit TO"
-- behavior documented in internal/census/materialized_views.go — this MV is
-- NOT counted by num_mv_targets, and its inner table is NOT counted as a
-- landing table.
CREATE MATERIALIZED VIEW main.event_type_totals
ENGINE = SummingMergeTree()
ORDER BY event_type
AS
SELECT event_type, count() AS cnt
FROM main.events
GROUP BY event_type;

-- Fan-in: two different MVs writing TO the same target table.
-- Covers: num_fan_in_targets.
CREATE TABLE main.shared_target (
    event_type String,
    cnt UInt64
) ENGINE = SummingMergeTree()
ORDER BY event_type;

CREATE MATERIALIZED VIEW main.fan_in_a TO main.shared_target AS
SELECT event_type, count() AS cnt
FROM main.events
WHERE event_type = 'click'
GROUP BY event_type;

CREATE MATERIALIZED VIEW main.fan_in_b TO main.shared_target AS
SELECT event_type, count() AS cnt
FROM main.events
WHERE event_type = 'view'
GROUP BY event_type;

-- Plain (non-materialized) view: doesn't hold data itself.
-- Covers: num_plain_views.
CREATE VIEW main.recent_purchases AS
SELECT * FROM main.events WHERE event_type = 'purchase';

-- MVs only fire on inserts that happen after they're created — the first
-- insert into main.events (above) predates all of them, so without this
-- second insert daily_counts_store/shared_target/event_type_totals's inner
-- table would all be empty. This is what actually populates them, so the
-- "landing table feeding live MVs" scenario is real data, not just schema.
INSERT INTO main.events
SELECT
    today(),
    now() - number * 5,
    rand() % 1000,
    arrayElement(['click', 'view', 'purchase'], (number % 3) + 1)
FROM numbers(300);
