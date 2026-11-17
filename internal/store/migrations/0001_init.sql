-- QuotaPulse schema, version 1.
--
-- Times are Unix seconds in UTC. Calendar days (reminder dates) are
-- 'YYYY-MM-DD' text: a day has no zone. JSON goes in TEXT columns where a
-- value has structure of its own but is only ever read whole.

CREATE TABLE kv (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) WITHOUT ROWID;

-- Provider definitions saved from the dashboard. The built-in catalog lives
-- in the binary, so an upgrade can fix a definition without a migration.
CREATE TABLE providers (
    id         TEXT PRIMARY KEY,
    definition TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE accounts (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    provider     TEXT NOT NULL,
    credential   BLOB NOT NULL,              -- sealed JSON
    grp          TEXT NOT NULL DEFAULT '',
    interval_s   INTEGER NOT NULL DEFAULT 0, -- 0: the default interval
    enabled      INTEGER NOT NULL DEFAULT 1,
    status       TEXT NOT NULL DEFAULT 'pending',
    last_error   TEXT NOT NULL DEFAULT '',
    failures     INTEGER NOT NULL DEFAULT 0,
    sampled_at   INTEGER,
    succeeded_at INTEGER,
    next_at      INTEGER,
    latency_ms   INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE meters (
    id          TEXT PRIMARY KEY,
    account_id  TEXT REFERENCES accounts(id) ON DELETE CASCADE, -- NULL: a pushed meter
    key         TEXT NOT NULL,
    name        TEXT NOT NULL,
    renamed     INTEGER NOT NULL DEFAULT 0,  -- 1 once a person has named it
    kind        TEXT NOT NULL,
    unit        TEXT NOT NULL DEFAULT '',
    value       REAL,
    limit_value REAL,
    resets_at   INTEGER,
    window_s    INTEGER,
    sampled_at  INTEGER,
    rules       TEXT NOT NULL DEFAULT '{}',
    muted_until INTEGER,
    hidden      INTEGER NOT NULL DEFAULT 0,
    sort        INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) WITHOUT ROWID;

-- A meter's key is unique within its account; pushed meters share one namespace.
CREATE UNIQUE INDEX meters_account_key ON meters (account_id, key) WHERE account_id IS NOT NULL;
CREATE UNIQUE INDEX meters_pushed_key ON meters (key) WHERE account_id IS NULL;

-- Clustered by meter and time: a meter's history is one contiguous range scan.
CREATE TABLE readings (
    meter_id    TEXT NOT NULL REFERENCES meters(id) ON DELETE CASCADE,
    ts          INTEGER NOT NULL,
    value       REAL NOT NULL,
    limit_value REAL,
    resets_at   INTEGER,
    PRIMARY KEY (meter_id, ts)
) WITHOUT ROWID;

CREATE TABLE reminders (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    kind          TEXT NOT NULL DEFAULT 'custom',
    rule          TEXT NOT NULL,
    lead_days     TEXT NOT NULL DEFAULT '[0]',
    amount        REAL,
    currency      TEXT NOT NULL DEFAULT '',
    grp           TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    done_through  TEXT,
    snoozed_until TEXT,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE incidents (
    id           TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_id   TEXT NOT NULL,
    kind         TEXT NOT NULL,
    dedup        TEXT NOT NULL DEFAULT '',
    severity     TEXT NOT NULL,
    state        TEXT NOT NULL,
    reason       TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL,
    data         TEXT NOT NULL DEFAULT '{}',
    opened_at    INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    resolved_at  INTEGER,
    acked_at     INTEGER,
    notified_at  INTEGER,
    notify_count INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;

-- At most one open incident per subject, kind and occurrence. The database
-- enforces what the incident engine relies on.
CREATE UNIQUE INDEX incidents_open ON incidents (subject_type, subject_id, kind, dedup) WHERE state = 'open';
CREATE INDEX incidents_opened ON incidents (opened_at);
CREATE INDEX incidents_subject ON incidents (subject_type, subject_id, opened_at);

CREATE TABLE channels (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    type         TEXT NOT NULL,
    config       BLOB NOT NULL,              -- sealed JSON
    topics       TEXT NOT NULL,
    min_severity TEXT NOT NULL DEFAULT 'info',
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) WITHOUT ROWID;

-- The outbox and its history in one table: a row is written before anything
-- is sent, so a restart mid-delivery loses nothing.
CREATE TABLE deliveries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id TEXT NOT NULL DEFAULT '',
    event       TEXT NOT NULL,
    channel_id  TEXT NOT NULL,
    channel     TEXT NOT NULL DEFAULT '',    -- the channel's name at the time
    message     TEXT NOT NULL,
    status      TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    not_before  INTEGER NOT NULL,
    last_error  TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    sent_at     INTEGER
);

CREATE INDEX deliveries_pending ON deliveries (not_before) WHERE status = 'pending';
CREATE INDEX deliveries_incident ON deliveries (incident_id);
CREATE INDEX deliveries_channel ON deliveries (channel_id, id);

CREATE TABLE tokens (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    hash         TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,
    scopes       TEXT NOT NULL,
    expires_at   INTEGER,
    last_used_at INTEGER,
    created_at   INTEGER NOT NULL
) WITHOUT ROWID;
