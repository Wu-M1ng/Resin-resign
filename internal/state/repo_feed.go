package state

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Resinat/Resin/internal/model"
)

// InsertFeed persists a new subscription feed. The caller must provide a
// generated token hash; plaintext tokens are intentionally not accepted here.
func (r *StateRepo) InsertFeed(feed model.SubscriptionFeed) error {
	if strings.TrimSpace(feed.SubscriptionIDsJSON) == "" {
		feed.SubscriptionIDsJSON = "[]"
	}
	if err := validateFeed(feed); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.db.Exec(`
		INSERT INTO subscription_feeds (
			id, name, platform_id, subscription_ids_json, relay_enabled, relay_host, relay_port, relay_tls, relay_server_name, default_format, enabled_formats_json,
			unsupported_policy, pretty, enabled, token_hash, token_prefix,
			created_at_ns, updated_at_ns
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, feed.ID, feed.Name, feed.PlatformID, feed.SubscriptionIDsJSON, boolInt(feed.RelayEnabled), feed.RelayHost, feed.RelayPort, boolInt(feed.RelayTLS), feed.RelayServerName, feed.DefaultFormat, feed.EnabledFormatsJSON,
		feed.UnsupportedPolicy, boolInt(feed.Pretty), boolInt(feed.Enabled),
		feed.TokenHash, feed.TokenPrefix, feed.CreatedAtNs, feed.UpdatedAtNs)
	return err
}

// GetFeed returns one feed by its stable ID.
func (r *StateRepo) GetFeed(id string) (*model.SubscriptionFeed, error) {
	return scanFeed(r.db.QueryRow(feedSelect+" WHERE id = ?", id))
}

// ListFeeds returns feeds in stable creation/name order.
func (r *StateRepo) ListFeeds() ([]model.SubscriptionFeed, error) {
	rows, err := r.db.Query(feedSelect + " ORDER BY created_at_ns, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	feeds := make([]model.SubscriptionFeed, 0)
	for rows.Next() {
		feed, err := scanFeedRow(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, *feed)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return feeds, nil
}

// UpdateFeed updates mutable feed settings and token metadata. CreatedAtNs is
// preserved by the database update so callers cannot accidentally reset it.
func (r *StateRepo) UpdateFeed(feed model.SubscriptionFeed) error {
	if strings.TrimSpace(feed.SubscriptionIDsJSON) == "" {
		feed.SubscriptionIDsJSON = "[]"
	}
	if err := validateFeed(feed); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	result, err := r.db.Exec(`
		UPDATE subscription_feeds SET
			name = ?, platform_id = ?, subscription_ids_json = ?, relay_enabled = ?, relay_host = ?, relay_port = ?, relay_tls = ?, relay_server_name = ?, default_format = ?, enabled_formats_json = ?,
			unsupported_policy = ?, pretty = ?, enabled = ?, token_hash = ?,
			token_prefix = ?, updated_at_ns = ?
		WHERE id = ?
	`, feed.Name, feed.PlatformID, feed.SubscriptionIDsJSON, boolInt(feed.RelayEnabled), feed.RelayHost, feed.RelayPort, boolInt(feed.RelayTLS), feed.RelayServerName, feed.DefaultFormat, feed.EnabledFormatsJSON,
		feed.UnsupportedPolicy, boolInt(feed.Pretty), boolInt(feed.Enabled),
		feed.TokenHash, feed.TokenPrefix, feed.UpdatedAtNs, feed.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFeed removes a feed by ID. It has no foreign-key side effects on
// subscriptions, nodes, or platforms.
func (r *StateRepo) DeleteFeed(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	result, err := r.db.Exec("DELETE FROM subscription_feeds WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// FindEnabledByTokenHash looks up an enabled feed by a complete token hash.
// Callers should pass HashToken(token), never the public token prefix.
func (r *StateRepo) FindEnabledByTokenHash(tokenHash string) (*model.SubscriptionFeed, error) {
	return scanFeed(r.db.QueryRow(feedSelect+" WHERE token_hash = ? AND enabled = 1", tokenHash))
}

const feedSelect = `SELECT id, name, platform_id, subscription_ids_json, relay_enabled, relay_host, relay_port, relay_tls, relay_server_name, default_format,
	enabled_formats_json, unsupported_policy, pretty, enabled, token_hash,
	token_prefix, created_at_ns, updated_at_ns FROM subscription_feeds`

type feedRow interface {
	Scan(dest ...any) error
}

func scanFeed(row *sql.Row) (*model.SubscriptionFeed, error) {
	feed, err := scanFeedRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return feed, nil
}

func scanFeedRow(row feedRow) (*model.SubscriptionFeed, error) {
	var feed model.SubscriptionFeed
	var pretty, enabled, relayEnabled, relayTLS int
	if err := row.Scan(
		&feed.ID, &feed.Name, &feed.PlatformID, &feed.SubscriptionIDsJSON, &relayEnabled, &feed.RelayHost, &feed.RelayPort, &relayTLS, &feed.RelayServerName, &feed.DefaultFormat,
		&feed.EnabledFormatsJSON, &feed.UnsupportedPolicy, &pretty, &enabled,
		&feed.TokenHash, &feed.TokenPrefix, &feed.CreatedAtNs, &feed.UpdatedAtNs,
	); err != nil {
		return nil, err
	}
	feed.Pretty = pretty != 0
	feed.Enabled = enabled != 0
	feed.RelayEnabled = relayEnabled != 0
	feed.RelayTLS = relayTLS != 0
	return &feed, nil
}

func validateFeed(feed model.SubscriptionFeed) error {
	if strings.TrimSpace(feed.ID) == "" {
		return fmt.Errorf("feed id must not be empty")
	}
	if strings.TrimSpace(feed.Name) == "" {
		return fmt.Errorf("feed name must not be empty")
	}
	if strings.TrimSpace(feed.DefaultFormat) == "" {
		return fmt.Errorf("feed default_format must not be empty")
	}
	if strings.TrimSpace(feed.EnabledFormatsJSON) == "" {
		return fmt.Errorf("feed enabled_formats_json must not be empty")
	}
	if strings.TrimSpace(feed.UnsupportedPolicy) == "" {
		return fmt.Errorf("feed unsupported_policy must not be empty")
	}
	if strings.TrimSpace(feed.TokenHash) == "" {
		return fmt.Errorf("feed token_hash must not be empty")
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
