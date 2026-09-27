ALTER TABLE subscription_feeds ADD COLUMN relay_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE subscription_feeds ADD COLUMN relay_host TEXT NOT NULL DEFAULT '';
ALTER TABLE subscription_feeds ADD COLUMN relay_port INTEGER NOT NULL DEFAULT 0;
