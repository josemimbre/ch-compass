CREATE DATABASE IF NOT EXISTS demo;

-- A well-used table
-- The skip index on event_type is redundant: it's already the first column in ORDER BY
CREATE TABLE demo.events (
    event_date Date,
    event_time DateTime,
    user_id UInt64,
    event_type String,
    payload String,
    INDEX idx_event_type event_type TYPE set(100) GRANULARITY 4
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_type, user_id, event_time);

INSERT INTO demo.events
SELECT
    today() - number % 90,
    now() - number * 60,
    rand() % 1000,
    arrayElement(['click', 'view', 'purchase', 'signup'], (number % 4) + 1),
    ''
FROM numbers(10000);

-- A large table with no recent access (cold table candidate)
CREATE TABLE demo.old_logs (
    log_date Date,
    message String,
    level String
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(log_date)
ORDER BY log_date;

INSERT INTO demo.old_logs
SELECT
    today() - 365 - (number % 90),
    concat('Log message ', toString(number)),
    arrayElement(['INFO', 'WARN', 'ERROR'], (number % 3) + 1)
FROM numbers(5000);

-- Regular view that is actively queried (detected via query text in system.query_log)
CREATE VIEW demo.daily_events AS
SELECT
    event_date,
    event_type,
    count() AS cnt
FROM demo.events
GROUP BY event_date, event_type;

-- Simulate usage so the view name appears in system.query_log query text
SELECT count() FROM demo.daily_events FORMAT Null;
SELECT event_type, sum(cnt) FROM demo.daily_events GROUP BY event_type FORMAT Null;

-- Regular view that is never queried (unused view candidate)
CREATE VIEW demo.user_stats AS
SELECT
    user_id,
    count() AS total_events,
    min(event_time) AS first_seen,
    max(event_time) AS last_seen
FROM demo.events
GROUP BY user_id;

-- Materialized view that is actively used
CREATE TABLE demo.hourly_events_store (
    hour DateTime,
    event_type String,
    cnt UInt64
) ENGINE = SummingMergeTree()
ORDER BY (hour, event_type);

CREATE MATERIALIZED VIEW demo.hourly_events TO demo.hourly_events_store AS
SELECT
    toStartOfHour(event_time) AS hour,
    event_type,
    count() AS cnt
FROM demo.events
GROUP BY hour, event_type;

-- Insert after MV creation so hourly_events triggers and appears in query_views_log
INSERT INTO demo.events
SELECT
    today(),
    now() - number * 10,
    rand() % 1000,
    arrayElement(['click', 'view', 'purchase', 'signup'], (number % 4) + 1),
    ''
FROM numbers(100);

-- Materialized view that is never triggered (unused candidate)
-- Created after all inserts into events, so it never fires
CREATE TABLE demo.event_counts_store (
    event_date Date,
    total UInt64
) ENGINE = SummingMergeTree()
ORDER BY event_date;

CREATE MATERIALIZED VIEW demo.event_counts TO demo.event_counts_store AS
SELECT
    event_date,
    count() AS total
FROM demo.events
GROUP BY event_date;

-- Table with too many partitions (bad partitioning candidate)
CREATE TABLE demo.over_partitioned (
    ts DateTime,
    value Float64
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(ts)
ORDER BY ts
SETTINGS max_partitions_per_insert_block = 500;

INSERT INTO demo.over_partitioned
SELECT
    now() - number * 3600,
    rand() / 1000000
FROM numbers(5000)
SETTINGS max_partitions_per_insert_block = 500;

-- ============================================================
-- Second database: analytics — additional problems for testing
-- ============================================================

CREATE DATABASE IF NOT EXISTS analytics;

-- Well-structured table with recent data (baseline, no recommendations)
CREATE TABLE analytics.page_views (
    view_date Date,
    view_time DateTime,
    url String,
    user_id UInt64,
    duration_ms UInt32
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(view_date)
ORDER BY (url, user_id, view_time);

INSERT INTO analytics.page_views
SELECT
    today() - number % 60,
    now() - number * 120,
    concat('/page/', toString(number % 50)),
    rand() % 2000,
    rand() % 10000
FROM numbers(20000);

-- Over-partitioned table (partitioned by day — too granular for this data)
CREATE TABLE analytics.metrics (
    ts DateTime,
    metric_name String,
    value Float64
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(ts)
ORDER BY (metric_name, ts)
SETTINGS max_partitions_per_insert_block = 500;

INSERT INTO analytics.metrics
SELECT
    now() - number * 3600,
    arrayElement(['cpu', 'memory', 'disk', 'network', 'latency'], (number % 5) + 1),
    rand() / 1000000
FROM numbers(5000)
SETTINGS max_partitions_per_insert_block = 500;

-- Cold table — old data, never queried
CREATE TABLE analytics.legacy_sessions (
    session_date Date,
    session_id String,
    user_id UInt64,
    pages UInt32
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(session_date)
ORDER BY (session_date, session_id);

INSERT INTO analytics.legacy_sessions
SELECT
    today() - 200 - (number % 120),
    concat('sess-', toString(number)),
    rand() % 5000,
    rand() % 20
FROM numbers(8000);

-- Another cold table — small but abandoned
CREATE TABLE analytics.ab_tests (
    test_date Date,
    test_name String,
    variant String,
    conversions UInt32
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(test_date)
ORDER BY (test_name, variant);

INSERT INTO analytics.ab_tests
SELECT
    today() - 300 - (number % 60),
    arrayElement(['signup_flow', 'pricing_page', 'onboarding'], (number % 3) + 1),
    arrayElement(['control', 'variant_a', 'variant_b'], (number % 3) + 1),
    rand() % 500
FROM numbers(1000);

-- Regular view — actively queried (should NOT be flagged)
CREATE VIEW analytics.daily_page_views AS
SELECT
    view_date,
    count() AS views,
    uniq(user_id) AS unique_users
FROM analytics.page_views
GROUP BY view_date;

SELECT count() FROM analytics.daily_page_views FORMAT Null;

-- Regular view — never queried (unused view candidate)
CREATE VIEW analytics.slow_pages AS
SELECT
    url,
    avg(duration_ms) AS avg_duration,
    max(duration_ms) AS max_duration
FROM analytics.page_views
GROUP BY url
HAVING avg_duration > 5000;

-- Another unused regular view
CREATE VIEW analytics.user_journey AS
SELECT
    user_id,
    groupArray(url) AS pages,
    count() AS page_count
FROM analytics.page_views
GROUP BY user_id;

-- Materialized view — actively triggered by inserts
CREATE TABLE analytics.hourly_metrics_store (
    hour DateTime,
    metric_name String,
    avg_value Float64,
    max_value Float64
) ENGINE = AggregatingMergeTree()
ORDER BY (hour, metric_name);

CREATE MATERIALIZED VIEW analytics.hourly_metrics TO analytics.hourly_metrics_store AS
SELECT
    toStartOfHour(ts) AS hour,
    metric_name,
    avg(value) AS avg_value,
    max(value) AS max_value
FROM analytics.metrics
GROUP BY hour, metric_name;

-- Trigger the MV with a fresh insert
INSERT INTO analytics.metrics
SELECT
    now() - number * 30,
    arrayElement(['cpu', 'memory'], (number % 2) + 1),
    rand() / 1000000
FROM numbers(200);

-- Materialized view — never triggered (unused candidate)
CREATE TABLE analytics.url_stats_store (
    view_date Date,
    url String,
    cnt UInt64
) ENGINE = SummingMergeTree()
ORDER BY (view_date, url);

CREATE MATERIALIZED VIEW analytics.url_stats TO analytics.url_stats_store AS
SELECT
    view_date,
    url,
    count() AS cnt
FROM analytics.page_views
GROUP BY view_date, url;

-- Table with a redundant skip index (duplicate index candidate)
-- The skip index on url is useless because url is already the first column in ORDER BY
CREATE TABLE analytics.search_logs (
    url String,
    query String,
    ts DateTime,
    results UInt32,
    INDEX idx_url url TYPE minmax GRANULARITY 3
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(ts)
ORDER BY (url, ts);

INSERT INTO analytics.search_logs
SELECT
    concat('/search/', toString(number % 100)),
    concat('query-', toString(number)),
    now() - number * 60,
    rand() % 1000
FROM numbers(5000);

-- Flush logs so query_views_log is populated for analysis
SYSTEM FLUSH LOGS;
