-- Some pre-migrate databases recorded endpoint migration version 8 without
-- materializing the table. Keep the TLS migration self-contained so those
-- databases can still be upgraded.
CREATE TABLE IF NOT EXISTS endpoints (
    id                      TEXT PRIMARY KEY,
    port                    INTEGER NOT NULL UNIQUE CHECK (port BETWEEN 1 AND 65535),
    allow_management        INTEGER NOT NULL,
    allow_proxy             INTEGER NOT NULL,
    require_proxy_auth_info INTEGER NOT NULL DEFAULT 0,
    allow_http_forward      INTEGER NOT NULL,
    allow_http_reverse      INTEGER NOT NULL,
    allow_socks5             INTEGER NOT NULL,
    enabled                 INTEGER NOT NULL DEFAULT 1,
    created_at_ns           INTEGER NOT NULL,
    updated_at_ns           INTEGER NOT NULL
);

ALTER TABLE endpoints ADD COLUMN listen_address TEXT NOT NULL DEFAULT '';
ALTER TABLE endpoints ADD COLUMN tls_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE endpoints ADD COLUMN tls_cert_file TEXT NOT NULL DEFAULT '';
ALTER TABLE endpoints ADD COLUMN tls_key_file TEXT NOT NULL DEFAULT '';
