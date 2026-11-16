package rosetta

import (
	"encoding/json"
	"strings"
)

// SanitizeAndRewriteRequest rewrites the target model ID and strips cross-vendor incompatible
// fields (e.g. Anthropic cryptographic thinking signatures when routing to non-Anthropic upstreams).
func SanitizeAndRewriteRequest(body []byte, targetModel string, targetProto string) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	payload["model"] = targetModel

	// If routing to a non-Anthropic upstream, strip thinking.signature blocks that cause HTTP 400
	if targetProto != "anthropic" && targetProto != "oauth-claude" {
		stripAnthropicSignatures(payload)
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func stripAnthropicSignatures(payload map[string]any) {
	msgs, ok := payload["messages"].([]any)
	if !ok {
		return
	}
	for _, m := range msgs {
		msgMap, ok := m.(map[string]any)
		if !ok {
			continue
		}
		blocks, ok := msgMap["content"].([]any)
		if !ok {
			continue
		}
		filtered := make([]any, 0, len(blocks))
		for _, b := range blocks {
			bMap, ok := b.(map[string]any)
			if !ok {
				filtered = append(filtered, b)
				continue
			}
			bType, _ := bMap["type"].(string)
			if bType == "thinking" || bType == "redacted_thinking" {
				// Convert thinking block to plain text note or drop its cryptographic signature
				delete(bMap, "signature")
				if thinkingText, ok := bMap["thinking"].(string); ok && strings.TrimSpace(thinkingText) != "" {
					filtered = append(filtered, map[string]any{
						"type": "text",
						"text": "<thinking>\n" + thinkingText + "\n</thinking>",
					})
				}
				continue
			}
			// Strip Anthropic cache_control if upstream is pure OpenAI
			delete(bMap, "cache_control")
			filtered = append(filtered, bMap)
		}
		msgMap["content"] = filtered
	}
}
