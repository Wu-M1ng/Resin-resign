// Package feed contains subscription feed rendering and format encoders.
package feed

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Resinat/Resin/internal/node"
)

// Format identifies a public subscription representation.
type Format string

const (
	FormatSingbox     Format = "singbox"
	FormatClashMeta   Format = "clash-meta"
	FormatV2RayBase64 Format = "v2ray-base64"
	FormatURI         Format = "uri"
)

// UnsupportedPolicy controls what happens when a node cannot be represented.
type UnsupportedPolicy string

const (
	UnsupportedSkip  UnsupportedPolicy = "skip"
	UnsupportedError UnsupportedPolicy = "error"
)

// ExportNode is the format-neutral node passed to an encoder.
type ExportNode struct {
	Hash       node.Hash
	Tag        string
	Tags       []string
	RawOptions json.RawMessage
	Region     string
}

// ExportOptions controls an export operation.
type ExportOptions struct {
	UnsupportedPolicy UnsupportedPolicy
	Pretty            bool
}

// ExportResult contains the encoded response and conversion statistics.
type ExportResult struct {
	Body         []byte
	ContentType  string
	NodeCount    int
	SkippedCount int
	SkippedTypes []string
}

// Export encodes nodes in one of the supported public formats.
func Export(format Format, nodes []ExportNode, options ExportOptions) (ExportResult, error) {
	if options.UnsupportedPolicy == "" {
		options.UnsupportedPolicy = UnsupportedSkip
	}
	if options.UnsupportedPolicy != UnsupportedSkip && options.UnsupportedPolicy != UnsupportedError {
		return ExportResult{}, fmt.Errorf("feed: unsupported policy %q", options.UnsupportedPolicy)
	}
	unique := deduplicateNodes(nodes)
	switch format {
	case FormatSingbox:
		return exportSingbox(unique, options)
	case FormatClashMeta:
		return exportClashMeta(unique, options)
	case FormatV2RayBase64, FormatURI:
		return exportURI(unique, format, options)
	default:
		return ExportResult{}, fmt.Errorf("feed: unknown format %q", format)
	}
}

func deduplicateNodes(nodes []ExportNode) []ExportNode {
	result := make([]ExportNode, 0, len(nodes))
	seen := make(map[node.Hash]struct{}, len(nodes))
	for _, item := range nodes {
		if _, ok := seen[item.Hash]; ok {
			continue
		}
		seen[item.Hash] = struct{}{}
		item.RawOptions = append(json.RawMessage(nil), item.RawOptions...)
		item.Tags = append([]string(nil), item.Tags...)
		result = append(result, item)
	}
	return result
}

func skippedType(types map[string]struct{}) []string {
	result := make([]string, 0, len(types))
	for typ := range types {
		result = append(result, typ)
	}
	sort.Strings(result)
	return result
}

func effectiveTag(item ExportNode, raw map[string]any) string {
	if tag := strings.TrimSpace(item.Tag); tag != "" {
		return tag
	}
	if tag, ok := raw["tag"].(string); ok && strings.TrimSpace(tag) != "" {
		return strings.TrimSpace(tag)
	}
	if !item.Hash.IsZero() {
		return item.Hash.Hex()
	}
	return "node"
}

func decodeObject(raw json.RawMessage) (map[string]any, error) {
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode outbound: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("decode outbound: expected object")
	}
	return object, nil
}

func outboundType(object map[string]any) string {
	typ, _ := object["type"].(string)
	return strings.ToLower(strings.TrimSpace(typ))
}
