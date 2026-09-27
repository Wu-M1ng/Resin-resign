package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Resinat/Resin/internal/feed"
	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/state"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/google/uuid"
)

var allowedFeedFormats = map[feed.Format]struct{}{
	feed.FormatSingbox: {}, feed.FormatClashMeta: {}, feed.FormatV2RayBase64: {}, feed.FormatURI: {},
}

type FeedResponse struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	PlatformID        string   `json:"platform_id"`
	SubscriptionIDs   []string `json:"subscription_ids"`
	RelayEnabled      bool     `json:"relay_enabled"`
	RelayHost         string   `json:"relay_host"`
	RelayPort         int      `json:"relay_port"`
	RelayTLS          bool     `json:"relay_tls"`
	RelayServerName   string   `json:"relay_server_name"`
	DefaultFormat     string   `json:"default_format"`
	EnabledFormats    []string `json:"enabled_formats"`
	UnsupportedPolicy string   `json:"unsupported_policy"`
	Pretty            bool     `json:"pretty"`
	Enabled           bool     `json:"enabled"`
	TokenPrefix       string   `json:"token_prefix"`
	PublicToken       string   `json:"public_token,omitempty"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

type CreateFeedRequest struct {
	Name              string   `json:"name"`
	PlatformID        string   `json:"platform_id"`
	SubscriptionIDs   []string `json:"subscription_ids"`
	RelayEnabled      bool     `json:"relay_enabled"`
	RelayHost         string   `json:"relay_host"`
	RelayPort         int      `json:"relay_port"`
	RelayTLS          bool     `json:"relay_tls"`
	RelayServerName   string   `json:"relay_server_name"`
	DefaultFormat     string   `json:"default_format"`
	EnabledFormats    []string `json:"enabled_formats"`
	UnsupportedPolicy string   `json:"unsupported_policy"`
	Pretty            bool     `json:"pretty"`
	Enabled           *bool    `json:"enabled"`
}

type FeedPreview struct {
	Format       string   `json:"format"`
	Body         string   `json:"body"`
	NodeCount    int      `json:"node_count"`
	SkippedCount int      `json:"skipped_count"`
	SkippedTypes []string `json:"skipped_types"`
	ContentType  string   `json:"content_type"`
}

type feedRenderCacheEntry struct {
	FeedUpdatedAt int64
	GeneratedAt   time.Time
	ETag          string
	Result        feed.ExportResult
}

func (s *ControlPlaneService) initFeedCache() {
	if s.feedCache == nil {
		s.feedCache = make(map[string]feedRenderCacheEntry)
	}
}

func decodeFeedFormats(raw string) ([]string, error) {
	var formats []string
	if err := json.Unmarshal([]byte(raw), &formats); err != nil {
		return nil, err
	}
	if formats == nil {
		formats = []string{}
	}
	return formats, nil
}

func encodeFeedStrings(values []string) string {
	if values == nil {
		values = []string{}
	}
	b, _ := json.Marshal(values)
	return string(b)
}

func validateFeedSettings(name, platformID string, subscriptionIDs []string, relayEnabled bool, relayHost string, relayPort int, relayTLS bool, relayServerName string, defaultFormat string, formats []string, policy string) *ServiceError {
	if strings.TrimSpace(name) == "" {
		return invalidArg("name is required")
	}
	hasPlatform := strings.TrimSpace(platformID) != ""
	hasSubscriptions := len(subscriptionIDs) > 0
	if hasPlatform == hasSubscriptions {
		return invalidArg("exactly one of platform_id or subscription_ids is required")
	}
	if relayEnabled {
		if !hasPlatform {
			return invalidArg("relay_enabled requires a platform source")
		}
		if strings.TrimSpace(relayHost) == "" {
			return invalidArg("relay_host is required when relay_enabled is true")
		}
		if strings.Contains(relayHost, "://") || strings.ContainsAny(relayHost, "/?#\t\r\n ") {
			return invalidArg("relay_host must be a hostname or IP address without a scheme")
		}
		if relayPort < 1 || relayPort > 65535 {
			return invalidArg("relay_port must be between 1 and 65535")
		}
		if relayTLS && strings.TrimSpace(relayServerName) == "" {
			return invalidArg("relay_server_name is required when relay_tls is true")
		}
	}
	if len(formats) == 0 {
		return invalidArg("enabled_formats must contain at least one format")
	}
	seen := make(map[string]struct{}, len(formats))
	for _, raw := range formats {
		value := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := allowedFeedFormats[feed.Format(value)]; !ok {
			return invalidArg(fmt.Sprintf("enabled_formats: unsupported format %q", raw))
		}
		if _, ok := seen[value]; ok {
			return invalidArg(fmt.Sprintf("enabled_formats: duplicate format %q", value))
		}
		seen[value] = struct{}{}
	}
	if _, ok := seen[strings.ToLower(strings.TrimSpace(defaultFormat))]; !ok {
		return invalidArg("default_format must be one of enabled_formats")
	}
	if policy == "" {
		policy = string(feed.UnsupportedSkip)
	}
	if policy != string(feed.UnsupportedSkip) && policy != string(feed.UnsupportedError) {
		return invalidArg("unsupported_policy must be skip or error")
	}
	return nil
}

func feedToResponse(m model.SubscriptionFeed, publicToken string) (FeedResponse, error) {
	formats, err := decodeFeedFormats(m.EnabledFormatsJSON)
	if err != nil {
		return FeedResponse{}, err
	}
	subs, err := decodeFeedFormats(m.SubscriptionIDsJSON)
	if err != nil {
		return FeedResponse{}, err
	}
	return FeedResponse{ID: m.ID, Name: m.Name, PlatformID: m.PlatformID, SubscriptionIDs: subs, RelayEnabled: m.RelayEnabled, RelayHost: m.RelayHost, RelayPort: m.RelayPort, RelayTLS: m.RelayTLS, RelayServerName: m.RelayServerName, DefaultFormat: m.DefaultFormat, EnabledFormats: formats, UnsupportedPolicy: m.UnsupportedPolicy, Pretty: m.Pretty, Enabled: m.Enabled, TokenPrefix: m.TokenPrefix, PublicToken: publicToken, CreatedAt: time.Unix(0, m.CreatedAtNs).UTC().Format(time.RFC3339Nano), UpdatedAt: time.Unix(0, m.UpdatedAtNs).UTC().Format(time.RFC3339Nano)}, nil
}

func (s *ControlPlaneService) getFeedModel(id string) (*model.SubscriptionFeed, error) {
	m, err := s.Engine.GetFeed(id)
	if errors.Is(err, state.ErrNotFound) {
		return nil, notFound("feed not found")
	}
	if err != nil {
		return nil, internal("get feed", err)
	}
	return m, nil
}

func (s *ControlPlaneService) validateFeedPlatform(id string) *ServiceError {
	if s == nil || s.Pool == nil {
		return internal("platform service unavailable", nil)
	}
	if _, ok := s.Pool.GetPlatform(id); !ok {
		return notFound("platform not found")
	}
	return nil
}

func (s *ControlPlaneService) validateFeedSubscriptions(ids []string) *ServiceError {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return invalidArg("subscription_ids cannot contain empty values")
		}
		if s.SubMgr == nil || s.SubMgr.Lookup(id) == nil {
			return notFound("subscription not found")
		}
	}
	return nil
}

func (s *ControlPlaneService) ListFeeds() ([]FeedResponse, error) {
	items, err := s.Engine.ListFeeds()
	if err != nil {
		return nil, internal("list feeds", err)
	}
	result := make([]FeedResponse, 0, len(items))
	for _, item := range items {
		resp, err := feedToResponse(item, "")
		if err != nil {
			return nil, internal("decode feed formats", err)
		}
		result = append(result, resp)
	}
	return result, nil
}

func (s *ControlPlaneService) GetFeed(id string) (*FeedResponse, error) {
	m, err := s.getFeedModel(id)
	if err != nil {
		return nil, err
	}
	resp, err := feedToResponse(*m, "")
	if err != nil {
		return nil, internal("decode feed formats", err)
	}
	return &resp, nil
}

func (s *ControlPlaneService) CreateFeed(req CreateFeedRequest) (*FeedResponse, error) {
	req.PlatformID = strings.TrimSpace(req.PlatformID)
	req.RelayHost = strings.TrimSpace(req.RelayHost)
	req.RelayServerName = strings.TrimSpace(req.RelayServerName)
	if req.RelayTLS && req.RelayServerName == "" {
		req.RelayServerName = req.RelayHost
	}
	for i := range req.EnabledFormats {
		req.EnabledFormats[i] = strings.ToLower(strings.TrimSpace(req.EnabledFormats[i]))
	}
	for i := range req.SubscriptionIDs {
		req.SubscriptionIDs[i] = strings.TrimSpace(req.SubscriptionIDs[i])
	}
	req.DefaultFormat = strings.ToLower(strings.TrimSpace(req.DefaultFormat))
	if req.UnsupportedPolicy == "" {
		req.UnsupportedPolicy = string(feed.UnsupportedSkip)
	}
	if verr := validateFeedSettings(req.Name, req.PlatformID, req.SubscriptionIDs, req.RelayEnabled, req.RelayHost, req.RelayPort, req.RelayTLS, req.RelayServerName, req.DefaultFormat, req.EnabledFormats, req.UnsupportedPolicy); verr != nil {
		return nil, verr
	}
	if req.PlatformID != "" {
		if verr := s.validateFeedPlatform(req.PlatformID); verr != nil {
			return nil, verr
		}
	}
	if verr := s.validateFeedSubscriptions(req.SubscriptionIDs); verr != nil {
		return nil, verr
	}
	plain, hash, prefix, err := feed.GenerateToken()
	if err != nil {
		return nil, internal("generate feed token", err)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := time.Now().UnixNano()
	m := model.SubscriptionFeed{ID: uuid.New().String(), Name: strings.TrimSpace(req.Name), PlatformID: req.PlatformID, SubscriptionIDsJSON: encodeFeedStrings(req.SubscriptionIDs), RelayEnabled: req.RelayEnabled, RelayHost: req.RelayHost, RelayPort: req.RelayPort, RelayTLS: req.RelayTLS, RelayServerName: req.RelayServerName, DefaultFormat: req.DefaultFormat, EnabledFormatsJSON: encodeFeedStrings(req.EnabledFormats), UnsupportedPolicy: req.UnsupportedPolicy, Pretty: req.Pretty, Enabled: enabled, TokenHash: hash, TokenPrefix: prefix, CreatedAtNs: now, UpdatedAtNs: now}
	if err := s.Engine.InsertFeed(m); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, conflict("feed name already exists")
		}
		return nil, internal("persist feed", err)
	}
	resp, err := feedToResponse(m, plain)
	if err != nil {
		return nil, internal("decode feed formats", err)
	}
	return &resp, nil
}

func (s *ControlPlaneService) UpdateFeed(id string, raw json.RawMessage) (*FeedResponse, error) {
	m, err := s.getFeedModel(id)
	if err != nil {
		return nil, err
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(raw, &patch); err != nil || len(patch) == 0 {
		return nil, invalidArg("invalid or empty feed patch")
	}
	var req CreateFeedRequest
	req.Name, req.PlatformID, req.RelayEnabled, req.RelayHost, req.RelayPort, req.RelayTLS, req.RelayServerName, req.DefaultFormat, req.UnsupportedPolicy, req.Pretty, req.Enabled = m.Name, m.PlatformID, m.RelayEnabled, m.RelayHost, m.RelayPort, m.RelayTLS, m.RelayServerName, m.DefaultFormat, m.UnsupportedPolicy, m.Pretty, &m.Enabled
	req.EnabledFormats, _ = decodeFeedFormats(m.EnabledFormatsJSON)
	req.SubscriptionIDs, _ = decodeFeedFormats(m.SubscriptionIDsJSON)
	sourcePatched := false
	for key, value := range patch {
		if string(value) == "null" {
			return nil, invalidArg(fmt.Sprintf("field %q cannot be null", key))
		}
		switch key {
		case "name":
			if err := json.Unmarshal(value, &req.Name); err != nil {
				return nil, invalidArg("name must be a string")
			}
		case "platform_id":
			sourcePatched = true
			if err := json.Unmarshal(value, &req.PlatformID); err != nil {
				return nil, invalidArg("platform_id must be a string")
			}
		case "subscription_ids":
			sourcePatched = true
			if err := json.Unmarshal(value, &req.SubscriptionIDs); err != nil {
				return nil, invalidArg("subscription_ids must be an array")
			}
		case "relay_enabled":
			if err := json.Unmarshal(value, &req.RelayEnabled); err != nil {
				return nil, invalidArg("relay_enabled must be a boolean")
			}
		case "relay_host":
			if err := json.Unmarshal(value, &req.RelayHost); err != nil {
				return nil, invalidArg("relay_host must be a string")
			}
		case "relay_port":
			if err := json.Unmarshal(value, &req.RelayPort); err != nil {
				return nil, invalidArg("relay_port must be an integer")
			}
		case "relay_tls":
			if err := json.Unmarshal(value, &req.RelayTLS); err != nil {
				return nil, invalidArg("relay_tls must be a boolean")
			}
		case "relay_server_name":
			if err := json.Unmarshal(value, &req.RelayServerName); err != nil {
				return nil, invalidArg("relay_server_name must be a string")
			}
		case "default_format":
			if err := json.Unmarshal(value, &req.DefaultFormat); err != nil {
				return nil, invalidArg("default_format must be a string")
			}
		case "enabled_formats":
			if err := json.Unmarshal(value, &req.EnabledFormats); err != nil {
				return nil, invalidArg("enabled_formats must be an array")
			}
		case "unsupported_policy":
			if err := json.Unmarshal(value, &req.UnsupportedPolicy); err != nil {
				return nil, invalidArg("unsupported_policy must be a string")
			}
		case "pretty":
			if err := json.Unmarshal(value, &req.Pretty); err != nil {
				return nil, invalidArg("pretty must be a boolean")
			}
		case "enabled":
			var valueBool bool
			if err := json.Unmarshal(value, &valueBool); err != nil {
				return nil, invalidArg("enabled must be a boolean")
			}
			req.Enabled = &valueBool
		default:
			return nil, invalidArg(fmt.Sprintf("field %q is unknown or read-only", key))
		}
	}
	req.PlatformID = strings.TrimSpace(req.PlatformID)
	req.RelayHost = strings.TrimSpace(req.RelayHost)
	req.RelayServerName = strings.TrimSpace(req.RelayServerName)
	if req.RelayTLS && req.RelayServerName == "" {
		req.RelayServerName = req.RelayHost
	}
	for i := range req.EnabledFormats {
		req.EnabledFormats[i] = strings.ToLower(strings.TrimSpace(req.EnabledFormats[i]))
	}
	for i := range req.SubscriptionIDs {
		req.SubscriptionIDs[i] = strings.TrimSpace(req.SubscriptionIDs[i])
	}
	req.DefaultFormat = strings.ToLower(strings.TrimSpace(req.DefaultFormat))
	if req.UnsupportedPolicy == "" {
		req.UnsupportedPolicy = string(feed.UnsupportedSkip)
	}
	if verr := validateFeedSettings(req.Name, req.PlatformID, req.SubscriptionIDs, req.RelayEnabled, req.RelayHost, req.RelayPort, req.RelayTLS, req.RelayServerName, req.DefaultFormat, req.EnabledFormats, req.UnsupportedPolicy); verr != nil {
		// Feeds created before source modes were exclusive may contain both
		// fields. Keep those records editable when the patch does not touch the
		// source selection; any new source selection must satisfy the XOR rule.
		legacyCombined := !sourcePatched && req.PlatformID != "" && len(req.SubscriptionIDs) > 0
		if !legacyCombined {
			return nil, verr
		}
	}
	if req.PlatformID != "" {
		if verr := s.validateFeedPlatform(req.PlatformID); verr != nil {
			return nil, verr
		}
	}
	if verr := s.validateFeedSubscriptions(req.SubscriptionIDs); verr != nil {
		return nil, verr
	}
	m.Name, m.PlatformID, m.SubscriptionIDsJSON, m.RelayEnabled, m.RelayHost, m.RelayPort, m.RelayTLS, m.RelayServerName, m.DefaultFormat, m.EnabledFormatsJSON, m.UnsupportedPolicy, m.Pretty, m.Enabled, m.UpdatedAtNs = strings.TrimSpace(req.Name), req.PlatformID, encodeFeedStrings(req.SubscriptionIDs), req.RelayEnabled, req.RelayHost, req.RelayPort, req.RelayTLS, req.RelayServerName, req.DefaultFormat, encodeFeedStrings(req.EnabledFormats), req.UnsupportedPolicy, req.Pretty, *req.Enabled, nextFeedUpdatedAt(m.UpdatedAtNs)
	if err := s.Engine.UpdateFeed(*m); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, conflict("feed name already exists")
		}
		if errors.Is(err, state.ErrNotFound) {
			return nil, notFound("feed not found")
		}
		return nil, internal("update feed", err)
	}
	s.invalidateFeedCache(id)
	resp, err := feedToResponse(*m, "")
	if err != nil {
		return nil, internal("decode feed formats", err)
	}
	return &resp, nil
}

func (s *ControlPlaneService) DeleteFeed(id string) error {
	if err := s.Engine.DeleteFeed(id); err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return notFound("feed not found")
		}
		return internal("delete feed", err)
	}
	s.invalidateFeedCache(id)
	return nil
}

func (s *ControlPlaneService) RotateFeedToken(id string) (*FeedResponse, error) {
	m, err := s.getFeedModel(id)
	if err != nil {
		return nil, err
	}
	plain, hash, prefix, err := feed.GenerateToken()
	if err != nil {
		return nil, internal("generate feed token", err)
	}
	m.TokenHash, m.TokenPrefix, m.UpdatedAtNs = hash, prefix, nextFeedUpdatedAt(m.UpdatedAtNs)
	if err := s.Engine.UpdateFeed(*m); err != nil {
		return nil, internal("rotate feed token", err)
	}
	s.invalidateFeedCache(id)
	resp, err := feedToResponse(*m, plain)
	if err != nil {
		return nil, internal("decode feed formats", err)
	}
	return &resp, nil
}

func (s *ControlPlaneService) invalidateFeedCache(id string) {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	for key := range s.feedCache {
		if strings.HasPrefix(key, id+":") {
			delete(s.feedCache, key)
		}
	}
}

func nextFeedUpdatedAt(previous int64) int64 {
	now := time.Now().UnixNano()
	if now <= previous {
		return previous + 1
	}
	return now
}

func (s *ControlPlaneService) buildFeedNodes(m model.SubscriptionFeed) ([]feed.ExportNode, error) {
	allowedSubs := map[string]struct{}{}
	if m.SubscriptionIDsJSON != "" {
		ids, err := decodeFeedFormats(m.SubscriptionIDsJSON)
		if err != nil {
			return nil, internal("decode feed subscriptions", err)
		}
		for _, id := range ids {
			allowedSubs[id] = struct{}{}
		}
	}
	if strings.TrimSpace(m.PlatformID) == "" {
		return s.buildSubscriptionFeedNodes(allowedSubs)
	}
	plat, ok := s.Pool.GetPlatform(m.PlatformID)
	if !ok || plat == nil {
		return nil, notFound("platform not found")
	}
	items := make([]feed.ExportNode, 0, plat.View().Size())
	plat.View().Range(func(h node.Hash) bool {
		entry, ok := s.Pool.GetEntry(h)
		if !ok || entry == nil {
			return true
		}
		if len(allowedSubs) > 0 {
			matched := false
			for _, id := range entry.SubscriptionIDs() {
				if _, ok := allowedSubs[id]; ok && s.SubMgr != nil {
					sub := s.SubMgr.Lookup(id)
					if sub == nil {
						continue
					}
					managed, managedOK := sub.ManagedNodes().LoadNode(h)
					if sub.Enabled() && managedOK && !managed.Evicted {
						matched = true
						break
					}
				}
			}
			if !matched {
				return true
			}
		}
		tag := s.Pool.ResolveNodeDisplayTag(h)
		if len(allowedSubs) > 0 {
			tag = s.resolveFeedTag(entry, h, allowedSubs)
		}
		if tag == "" {
			tag = h.Hex()
		}
		items = append(items, feed.ExportNode{Hash: h, Tag: tag, RawOptions: append([]byte(nil), entry.RawOptions...), Region: entry.GetRegion(nil)})
		return true
	})
	sort.Slice(items, func(i, j int) bool {
		if items[i].Tag == items[j].Tag {
			return items[i].Hash.Hex() < items[j].Hash.Hex()
		}
		return items[i].Tag < items[j].Tag
	})
	if m.RelayEnabled {
		return s.buildRelayFeedNodes(m, plat.Name, len(items) > 0)
	}
	return items, nil
}

// buildRelayFeedNodes replaces the selected platform's internal nodes with a
// single public SOCKS5 endpoint. Resin receives the client connection and
// routes it through the selected platform, so internal names such as
// "warp" never need to be resolvable by the client device.
func (s *ControlPlaneService) buildRelayFeedNodes(m model.SubscriptionFeed, platformName string, hasNodes bool) ([]feed.ExportNode, error) {
	if !hasNodes {
		return []feed.ExportNode{}, nil
	}
	password := ""
	if s.EnvCfg != nil {
		password = s.EnvCfg.ProxyToken
	}
	object := map[string]any{
		"type":        "socks",
		"tag":         strings.TrimSpace(m.Name) + " / Resin",
		"server":      strings.TrimSpace(m.RelayHost),
		"server_port": m.RelayPort,
		"version":     "5",
		"username":    platformName,
	}
	if password != "" {
		object["password"] = password
	}
	if m.RelayTLS {
		serverName := strings.TrimSpace(m.RelayServerName)
		if serverName == "" {
			serverName = strings.TrimSpace(m.RelayHost)
		}
		object["tls"] = map[string]any{
			"enabled":     true,
			"server_name": serverName,
		}
	}
	raw, err := json.Marshal(object)
	if err != nil {
		return nil, internal("marshal relay feed node", err)
	}
	return []feed.ExportNode{{
		Tag:        strings.TrimSpace(m.Name) + " / Resin",
		RawOptions: raw,
	}}, nil
}

// buildSubscriptionFeedNodes exports nodes directly from the selected source
// subscriptions. This mode intentionally does not require a platform's
// health/routing view, so a feed can expose refreshed source nodes before a
// platform has accepted them into its routable set.
func (s *ControlPlaneService) buildSubscriptionFeedNodes(allowedSubs map[string]struct{}) ([]feed.ExportNode, error) {
	if len(allowedSubs) == 0 {
		return nil, invalidArg("subscription_ids is required when platform_id is empty")
	}
	if s.SubMgr == nil || s.Pool == nil {
		return nil, internal("subscription service unavailable", nil)
	}

	hashes := make(map[node.Hash]struct{})
	for id := range allowedSubs {
		sub := s.SubMgr.Lookup(id)
		if sub == nil {
			return nil, notFound("subscription not found")
		}
		if !sub.Enabled() {
			continue
		}
		sub.ManagedNodes().RangeNodes(func(hash node.Hash, managed subscription.ManagedNode) bool {
			if !managed.Evicted {
				hashes[hash] = struct{}{}
			}
			return true
		})
	}

	items := make([]feed.ExportNode, 0, len(hashes))
	for hash := range hashes {
		entry, ok := s.Pool.GetEntry(hash)
		if !ok || entry == nil {
			continue
		}
		tag := s.resolveFeedTag(entry, hash, allowedSubs)
		if tag == "" {
			tag = hash.Hex()
		}
		items = append(items, feed.ExportNode{
			Hash:       hash,
			Tag:        tag,
			RawOptions: append([]byte(nil), entry.RawOptions...),
			Region:     entry.GetRegion(nil),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Tag == items[j].Tag {
			return items[i].Hash.Hex() < items[j].Hash.Hex()
		}
		return items[i].Tag < items[j].Tag
	})
	return items, nil
}

func (s *ControlPlaneService) resolveFeedTag(entry *node.NodeEntry, h node.Hash, allowed map[string]struct{}) string {
	type candidate struct {
		created       int64
		id, name, tag string
	}
	var best *candidate
	for _, id := range entry.SubscriptionIDs() {
		if _, ok := allowed[id]; !ok || s.SubMgr == nil {
			continue
		}
		sub := s.SubMgr.Lookup(id)
		if sub == nil || !sub.Enabled() {
			continue
		}
		managed, ok := sub.ManagedNodes().LoadNode(h)
		if !ok || managed.Evicted || len(managed.Tags) == 0 {
			continue
		}
		tag := managed.Tags[0]
		for _, item := range managed.Tags[1:] {
			if item < tag {
				tag = item
			}
		}
		item := &candidate{created: sub.CreatedAtNs, id: id, name: sub.Name(), tag: tag}
		if best == nil || item.created < best.created || (item.created == best.created && item.id < best.id) {
			best = item
		}
	}
	if best == nil || best.name == "" || best.tag == "" {
		return ""
	}
	return best.name + "/" + best.tag
}

func (s *ControlPlaneService) renderFeed(m model.SubscriptionFeed, format feed.Format) (feed.ExportResult, string, error) {
	cacheKey := m.ID + ":" + string(format)
	flightKey := cacheKey + ":" + strconv.FormatInt(m.UpdatedAtNs, 10)
	if cached, ok := s.feedCacheEntry(cacheKey, m.UpdatedAtNs); ok {
		return cached.Result, cached.ETag, nil
	}
	value, err, _ := s.feedFlight.Do(flightKey, func() (any, error) {
		// Another request may have populated the cache while this request was
		// waiting for the singleflight call to start.
		if cached, ok := s.feedCacheEntry(cacheKey, m.UpdatedAtNs); ok {
			return cached, nil
		}
		nodes, err := s.buildFeedNodes(m)
		if err != nil {
			return nil, err
		}
		result, err := feed.Export(format, nodes, feed.ExportOptions{UnsupportedPolicy: feed.UnsupportedPolicy(m.UnsupportedPolicy), Pretty: m.Pretty})
		if err != nil {
			return nil, internal("render feed", err)
		}
		sum := sha256.Sum256(result.Body)
		etag := `"` + hex.EncodeToString(sum[:]) + `"`
		entry := feedRenderCacheEntry{FeedUpdatedAt: m.UpdatedAtNs, GeneratedAt: time.Now(), ETag: etag, Result: result}
		s.feedMu.Lock()
		s.initFeedCache()
		s.feedCache[cacheKey] = entry
		s.feedMu.Unlock()
		return entry, nil
	})
	if err != nil {
		return feed.ExportResult{}, "", err
	}
	entry, ok := value.(feedRenderCacheEntry)
	if !ok {
		return feed.ExportResult{}, "", internal("render feed", fmt.Errorf("unexpected cache entry type %T", value))
	}
	return entry.Result, entry.ETag, nil
}

func (s *ControlPlaneService) feedCacheEntry(key string, updatedAt int64) (feedRenderCacheEntry, bool) {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	s.initFeedCache()
	cached, ok := s.feedCache[key]
	if !ok || cached.FeedUpdatedAt != updatedAt || time.Since(cached.GeneratedAt) >= 30*time.Second {
		return feedRenderCacheEntry{}, false
	}
	return cached, true
}

func (s *ControlPlaneService) RenderFeed(token, requested string) (model.SubscriptionFeed, feed.ExportResult, string, error) {
	m, err := s.Engine.FindEnabledByTokenHash(feed.HashToken(token))
	if errors.Is(err, state.ErrNotFound) {
		return model.SubscriptionFeed{}, feed.ExportResult{}, "", notFound("feed not found")
	}
	if err != nil {
		return model.SubscriptionFeed{}, feed.ExportResult{}, "", internal("lookup feed", err)
	}
	formats, err := decodeFeedFormats(m.EnabledFormatsJSON)
	if err != nil {
		return model.SubscriptionFeed{}, feed.ExportResult{}, "", internal("decode feed formats", err)
	}
	format := strings.ToLower(strings.TrimSpace(requested))
	if format == "" {
		format = m.DefaultFormat
	}
	enabled := false
	for _, f := range formats {
		if f == format {
			enabled = true
			break
		}
	}
	if !enabled {
		return model.SubscriptionFeed{}, feed.ExportResult{}, "", notFound("feed format not found")
	}
	result, etag, err := s.renderFeed(*m, feed.Format(format))
	return *m, result, etag, err
}

func (s *ControlPlaneService) PreviewFeed(id, format string) (*FeedPreview, error) {
	m, err := s.getFeedModel(id)
	if err != nil {
		return nil, err
	}
	formats, err := decodeFeedFormats(m.EnabledFormatsJSON)
	if err != nil {
		return nil, internal("decode feed formats", err)
	}
	format = strings.ToLower(strings.TrimSpace(format))
	enabled := false
	for _, f := range formats {
		if f == format {
			enabled = true
			break
		}
	}
	if !enabled {
		return nil, notFound("feed format not found")
	}
	result, _, err := s.renderFeed(*m, feed.Format(format))
	if err != nil {
		return nil, err
	}
	return &FeedPreview{Format: format, Body: string(result.Body), NodeCount: result.NodeCount, SkippedCount: result.SkippedCount, SkippedTypes: result.SkippedTypes, ContentType: result.ContentType}, nil
}
