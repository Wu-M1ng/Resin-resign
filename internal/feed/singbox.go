package feed

import (
	"encoding/json"
	"fmt"
)

func exportSingbox(nodes []ExportNode, options ExportOptions) (ExportResult, error) {
	outbounds := make([]map[string]any, 0, len(nodes))
	skipped := make(map[string]struct{})
	for _, item := range nodes {
		object, err := decodeObject(item.RawOptions)
		if err != nil {
			if options.UnsupportedPolicy == UnsupportedError {
				return ExportResult{}, err
			}
			skipped["invalid"] = struct{}{}
			continue
		}
		tag := effectiveTag(item, object)
		object["tag"] = tag
		if outboundType(object) == "" {
			if options.UnsupportedPolicy == UnsupportedError {
				return ExportResult{}, fmt.Errorf("feed: outbound %q has no type", tag)
			}
			skipped["invalid"] = struct{}{}
			continue
		}
		outbounds = append(outbounds, object)
	}
	payload := map[string]any{"outbounds": outbounds}
	var body []byte
	var err error
	if options.Pretty {
		body, err = json.MarshalIndent(payload, "", "  ")
	} else {
		body, err = json.Marshal(payload)
	}
	if err != nil {
		return ExportResult{}, fmt.Errorf("feed: marshal sing-box: %w", err)
	}
	body = append(body, '\n')
	return ExportResult{
		Body: body, ContentType: "application/json; charset=utf-8",
		NodeCount: len(outbounds), SkippedCount: len(nodes) - len(outbounds), SkippedTypes: skippedType(skipped),
	}, nil
}
