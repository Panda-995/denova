package assetstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"denova/internal/agents/conversationjournal"
)

func rewriteJSON(raw json.RawMessage, replacements *strings.Replacer) (json.RawMessage, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	changed := false
	var rewrite func(any) any
	rewrite = func(value any) any {
		switch v := value.(type) {
		case string:
			if strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://") {
				return v
			}
			// Decode nested JSON before replacing so remote URL leaves retain
			// their opaque value even in serialized tool results.
			if json.Valid([]byte(v)) && (strings.HasPrefix(strings.TrimSpace(v), "{") || strings.HasPrefix(strings.TrimSpace(v), "[")) {
				next, err := rewriteJSON([]byte(v), replacements)
				if err != nil {
					return v
				}
				if !bytes.Equal(next, []byte(v)) {
					changed = true
					return string(next)
				}
				return v
			}
			next := rewriteText(v, replacements)
			if next != v {
				changed = true
				return next
			}
			// Existing change/receipt records store text snapshots as JSON bytes.
			if decoded, err := base64.StdEncoding.DecodeString(v); err == nil && utf8.Valid(decoded) {
				next := rewriteText(string(decoded), replacements)
				if next != string(decoded) {
					changed = true
					return base64.StdEncoding.EncodeToString([]byte(next))
				}
			}
		case []any:
			for i := range v {
				v[i] = rewrite(v[i])
			}
		case map[string]any:
			for key, child := range v {
				nextKey := replacements.Replace(key)
				if nextKey != key {
					changed = true
					delete(v, key)
				}
				v[nextKey] = rewrite(child)
			}
		}
		return value
	}
	value = rewrite(value)
	if !changed {
		return raw, nil
	}
	return json.Marshal(value)
}

func rewriteReferences(name string, data []byte, replacements *strings.Replacer) ([]byte, error) {
	if strings.HasSuffix(name, ".jsonl") {
		if !bytes.Contains(data, []byte("assets/")) {
			return data, nil
		}
		return conversationjournal.RewritePayloads(data, func(raw json.RawMessage) (json.RawMessage, error) {
			return rewriteJSON(raw, replacements)
		})
	}
	if strings.HasSuffix(name, ".json") {
		return rewriteJSON(data, replacements)
	}
	return []byte(rewriteText(string(data), replacements)), nil
}

// Released per-image metadata duplicated file attributes and relationships.
// Keep only extra generation inputs/results; items.json retains its full assets.
func generationDetails(raw json.RawMessage) (json.RawMessage, error) {
	var detail map[string]json.RawMessage
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil, err
	}
	for _, key := range []string{"schema", "image_path", "meta_path", "markdown", "alt_text", "mime_type", "size_bytes", "item_id", "item_type", "item_name", "story_id", "branch_id", "turn_id", "cover_path", "source_path", "backup_path", "cover_updated_at"} {
		delete(detail, key)
	}
	return json.Marshal(detail)
}

var remoteReference = regexp.MustCompile(`https?://[^\s<>"'()]+`)

func rewriteText(text string, replacements *strings.Replacer) string {
	var result strings.Builder
	previous := 0
	for _, match := range remoteReference.FindAllStringIndex(text, -1) {
		result.WriteString(replacements.Replace(text[previous:match[0]]))
		result.WriteString(text[match[0]:match[1]])
		previous = match[1]
	}
	result.WriteString(replacements.Replace(text[previous:]))
	return result.String()
}
