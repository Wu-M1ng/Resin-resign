ALTER TABLE subscription_feeds ADD COLUMN relay_tls INTEGER NOT NULL DEFAULT 0;
ALTER TABLE subscription_feeds ADD COLUMN relay_server_name TEXT NOT NULL DEFAULT '';
