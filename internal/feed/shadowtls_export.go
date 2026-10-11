package feed

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type feedDependency struct {
	raw       map[string]any
	transport map[string]any // Generated native transport for an SS plugin node.
	err       error
	dep       int
}

// prepareFeedDependencies resolves original tags before exporters rename them.
// Native output retains dependencies; flat formats combine SS and ShadowTLS.
func prepareFeedDependencies(nodes []ExportNode, format Format, options ExportOptions) ([]ExportNode, ExportResult, error) {
	entries := make([]feedDependency, len(nodes))
	type reference struct{ source, tag string }
	index := make(map[reference]map[int]struct{})
	for i, item := range nodes {
		entries[i].raw, entries[i].err = decodeObject(item.RawOptions)
		entries[i].dep = -1
		aliases := item.SourceTags
		if len(aliases) == 0 {
			tag, _ := stringValue(entries[i].raw, "tag")
			aliases = map[string][]string{"": {tag}}
		}
		for source, tags := range aliases {
			for _, tag := range tags {
				if tag == "" {
					continue
				}
				key := reference{source, tag}
				if index[key] == nil {
					index[key] = make(map[int]struct{})
				}
				index[key][i] = struct{}{}
			}
		}
	}
	for i, item := range nodes {
		entry := &entries[i]
		detour, ok := stringValue(entry.raw, "detour")
		if !ok || entry.err != nil {
			continue
		}
		sources := item.SourceTags
		if len(sources) == 0 {
			sources = map[string][]string{"": nil}
		}
		matches := make(map[int]struct{})
		for source := range sources {
			candidates := index[reference{source, detour}]
			// A deduplicated parent must resolve consistently in every source.
			if len(candidates) != 1 {
				entry.err = fmt.Errorf("missing or ambiguous detour %q in source subscription", detour)
				break
			}
			for candidate := range candidates {
				matches[candidate] = struct{}{}
			}
		}
		if entry.err == nil && len(matches) != 1 {
			entry.err = fmt.Errorf("detour %q resolves to different nodes across subscriptions", detour)
		}
		if entry.err == nil {
			for candidate := range matches {
				entry.dep = candidate
			}
		}
	}
	// Convert before checking dependency failures so a rejected plugin also
	// rejects nodes depending on it; never leave a dangling native detour.
	if format == FormatSingbox {
		for i := range entries {
			e := &entries[i]
			if e.err != nil {
				continue
			}
			inner, outer, err := expandShadowTLSPlugin(e.raw)
			e.err = err
			if err == nil {
				e.raw, e.transport = inner, outer
			}
		}
	}
	// Reject cycles and broken dependencies, including failures further down a chain.
	state := make([]uint8, len(entries))
	var visit func(int) error
	visit = func(i int) error {
		e := &entries[i]
		if e.err != nil || state[i] == 2 {
			return e.err
		}
		if state[i] == 1 {
			e.err = fmt.Errorf("cyclic outbound detour")
			return e.err
		}
		state[i] = 1
		if e.dep >= 0 {
			e.err = visit(e.dep)
		}
		state[i] = 2
		return e.err
	}
	for i := range entries {
		visit(i)
	}
	used := make([]bool, len(entries))
	for i := range entries {
		e := &entries[i]
		if e.err != nil || e.dep < 0 || outboundType(e.raw) == "shadowtls" {
			continue
		}
		if format != FormatSingbox {
			if outboundType(e.raw) != "shadowsocks" || outboundType(entries[e.dep].raw) != "shadowtls" {
				e.err = fmt.Errorf("this format cannot represent this outbound detour")
				continue
			}
			var combined map[string]any
			combined, e.err = combineShadowTLS(e.raw, entries[e.dep].raw)
			if e.err != nil {
				continue
			}
			e.raw = combined
		}
		for dep := e.dep; dep >= 0; dep = entries[dep].dep {
			used[dep] = true
		}
	}
	// Allocate unique display tags so native references cannot collide.
	tags := make([]string, len(nodes))
	allocated := make(map[string]bool)
	for i, item := range nodes {
		base := effectiveTag(item, entries[i].raw)
		tags[i] = base
		for suffix := 2; allocated[tags[i]]; suffix++ {
			tags[i] = fmt.Sprintf("%s (%d)", base, suffix)
		}
		allocated[tags[i]] = true
	}
	// Reserve every source tag first, including source names matching this suffix.
	for i := range entries {
		e := &entries[i]
		if e.transport == nil || e.err != nil {
			continue
		}
		base := tags[i] + " [ShadowTLS]"
		tag := base
		for suffix := 2; allocated[tag]; suffix++ {
			tag = fmt.Sprintf("%s (%d)", base, suffix)
		}
		allocated[tag] = true
		e.transport["tag"] = tag
		e.raw["detour"] = tag
	}
	prepared := make([]ExportNode, 0, len(nodes))
	var dropped ExportResult
	skipped := make(map[string]struct{})
	for i, item := range nodes {
		e := &entries[i]
		typ := outboundType(e.raw)
		if typ == "shadowtls" && e.err == nil {
			if !used[i] {
				e.err = fmt.Errorf("standalone shadowtls has no usable inner proxy; Shadowsocks method and password cannot be inferred")
			} else if format != FormatSingbox {
				continue // An incorporated transport is not a skipped proxy.
			}
		}
		if e.err != nil {
			if options.UnsupportedPolicy == UnsupportedError {
				return nil, ExportResult{}, fmt.Errorf("feed: outbound %q: %w", tags[i], e.err)
			}
			dropped.SkippedCount++
			if typ == "" {
				typ = "invalid"
			}
			skipped[typ] = struct{}{}
			continue
		}
		if format == FormatSingbox && e.dep >= 0 {
			if e.transport != nil {
				e.transport["detour"] = tags[e.dep]
			} else {
				e.raw["detour"] = tags[e.dep]
			}
		}
		item.Tag = tags[i]
		var err error
		item.RawOptions, err = json.Marshal(e.raw)
		if err != nil {
			return nil, ExportResult{}, fmt.Errorf("feed: encode prepared outbound: %w", err)
		}
		prepared = append(prepared, item)
		if e.transport != nil {
			raw, err := json.Marshal(e.transport)
			if err != nil {
				return nil, ExportResult{}, fmt.Errorf("feed: encode native ShadowTLS transport: %w", err)
			}
			prepared = append(prepared, ExportNode{Tag: e.transport["tag"].(string), RawOptions: raw})
		}
	}
	dropped.SkippedTypes = skippedType(skipped)
	return prepared, dropped, nil
}

func combineShadowTLS(inner, outer map[string]any) (map[string]any, error) {
	if _, ok := requiredString(inner, "method"); !ok {
		return nil, fmt.Errorf("ShadowTLS inner Shadowsocks has no method")
	}
	if _, ok := requiredString(inner, "password"); !ok {
		return nil, fmt.Errorf("ShadowTLS inner Shadowsocks has no password")
	}
	// Do not silently drop an existing plugin, another hop or protocol features.
	for key := range inner {
		switch key {
		case "type", "tag", "server", "server_port", "method", "password", "detour", "network":
		default:
			return nil, fmt.Errorf("cannot combine Shadowsocks option %q", key)
		}
	}
	if network, ok := inner["network"]; ok && network != "tcp" {
		return nil, fmt.Errorf("ShadowTLS combination supports TCP only")
	}
	for key := range outer {
		switch key {
		case "type", "tag", "server", "server_port", "version", "password", "tls":
		default:
			return nil, fmt.Errorf("cannot combine ShadowTLS option %q", key)
		}
	}
	server, ok := requiredString(outer, "server")
	if !ok {
		return nil, fmt.Errorf("ShadowTLS transport has no server")
	}
	port, ok := uintValue(outer, "server_port")
	if !ok || port > 65535 {
		return nil, fmt.Errorf("ShadowTLS transport has no valid port")
	}
	version := 1 // Native ShadowTLS defaults to v1; the plugin defaults to v2.
	if value, present := outer["version"]; present {
		var err error
		version, err = strconv.Atoi(fmt.Sprint(value))
		if err != nil || version < 0 || version > 3 {
			return nil, fmt.Errorf("invalid ShadowTLS version")
		}
		if version == 0 {
			version = 1
		}
	}
	password, _ := outer["password"].(string)
	if version >= 2 && password == "" {
		return nil, fmt.Errorf("ShadowTLS transport has no password")
	}
	tls, ok := outer["tls"].(map[string]any)
	if !ok || tls["enabled"] != true {
		return nil, fmt.Errorf("ShadowTLS transport requires TLS")
	}
	// Native TLS sends no ALPN by default; the Resin plugin otherwise defaults
	// to h2/http1.1. An explicit empty value preserves the native handshake.
	opts := map[string]string{"version": strconv.Itoa(version), "alpn": ""}
	if password != "" {
		opts["password"] = password
	}
	for key, value := range tls {
		switch key {
		case "enabled", "server_name":
		case "insecure":
			enabled, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("invalid ShadowTLS TLS insecure option")
			}
			opts["skip-cert-verify"] = strconv.FormatBool(enabled)
		case "alpn":
			alpn := stringSliceValue(tls, key)
			if len(alpn) == 0 {
				return nil, fmt.Errorf("cannot combine empty ShadowTLS ALPN")
			}
			opts["alpn"] = strings.Join(alpn, ",")
		case "utls":
			utls, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid ShadowTLS uTLS options")
			}
			if enabled, _ := boolValue(utls, "enabled"); enabled {
				fingerprint, ok := requiredString(utls, "fingerprint")
				if !ok || version == 1 {
					return nil, fmt.Errorf("cannot combine ShadowTLS uTLS fingerprint")
				}
				opts["client-fingerprint"] = fingerprint
			}
		default:
			return nil, fmt.Errorf("cannot combine ShadowTLS TLS option %q without losing its semantics; use singbox format", key)
		}
	}
	host, _ := stringValue(tls, "server_name")
	if host == "" {
		host = server
	}
	opts["host"] = host
	combined := make(map[string]any, len(inner))
	for key, value := range inner {
		combined[key] = value
	}
	delete(combined, "detour")
	combined["server"], combined["server_port"] = server, port
	combined["plugin"], combined["plugin_opts"] = "shadow-tls", encodePluginOptions(opts)
	combined["network"] = "tcp"
	return combined, nil
}

func encodePluginOptions(options map[string]string) string {
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	escape := strings.NewReplacer(`\`, `\\`, `;`, `\;`, `=`, `\=`)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, escape.Replace(key)+"="+escape.Replace(options[key]))
	}
	return strings.Join(parts, ";")
}
