package model

// SubscriptionFeed describes a persisted, publicly readable subscription
// projection. The token itself is never stored; TokenHash is the SHA-256
// digest used for lookup and TokenPrefix is only a display hint.
type SubscriptionFeed struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	PlatformID          string `json:"platform_id"`
	SubscriptionIDsJSON string `json:"subscription_ids_json"`
	RelayEnabled        bool   `json:"relay_enabled"`
	RelayHost           string `json:"relay_host"`
	RelayPort           int    `json:"relay_port"`
	RelayTLS            bool   `json:"relay_tls"`
	RelayServerName     string `json:"relay_server_name"`
	DefaultFormat       string `json:"default_format"`
	EnabledFormatsJSON  string `json:"enabled_formats_json"`
	UnsupportedPolicy   string `json:"unsupported_policy"`
	Pretty              bool   `json:"pretty"`
	Enabled             bool   `json:"enabled"`
	TokenHash           string `json:"-"`
	TokenPrefix         string `json:"token_prefix"`
	CreatedAtNs         int64  `json:"created_at_ns"`
	UpdatedAtNs         int64  `json:"updated_at_ns"`
}
