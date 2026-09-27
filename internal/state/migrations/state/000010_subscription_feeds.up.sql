CREATE TABLE IF NOT EXISTS subscription_feeds (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL UNIQUE,
    platform_id          TEXT NOT NULL,
    default_format       TEXT NOT NULL,
    enabled_formats_json TEXT NOT NULL DEFAULT '[]',
    unsupported_policy   TEXT NOT NULL DEFAULT 'skip',
    pretty               INTEGER NOT NULL DEFAULT 0,
    enabled              INTEGER NOT NULL DEFAULT 1,
    token_hash           TEXT NOT NULL UNIQUE,
    token_prefix         TEXT NOT NULL DEFAULT '',
    created_at_ns        INTEGER NOT NULL,
    updated_at_ns        INTEGER NOT NULL
);
