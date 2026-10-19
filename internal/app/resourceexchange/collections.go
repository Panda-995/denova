package resourceexchange

import (
	"context"
	"encoding/json"
	"fmt"

	"denova/internal/book/lore"
)

// Collections are distribution units with stable source IDs. Receipts track
// members in the existing project libraries, not in a second content store.
type portableCollection[T any] struct {
	Version    int              `json:"version"`
	Items      []T              `json:"items"`
	Categories []lore.Category  `json:"categories,omitempty"`
	IndexGuide *lore.IndexGuide `json:"index_guide,omitempty"`
}

func collectionPath(kind string) string {
	switch kind {
	case "lore.collection":
		return lore.ItemsRelativePath
	case "game.openings":
		return openingPath
	default:
		return ""
	}
}

func collectionMembersState(digests map[string]string, binding Binding) string {
	for _, member := range binding.Members {
		digest, found := digests[member.ID]
		if !found {
			return "missing"
		}
		if digest != member.Digest {
			return "modified"
		}
	}
	return "unchanged"
}

func (s *Service) collectionLocalState(ctx context.Context, binding Binding) (string, error) {
	switch binding.Local.Kind {
	case "lore.collection":
		_, layout, err := s.registry.Resolve(binding.Local.ProjectID, true)
		if err != nil {
			return "", err
		}
		items, err := lore.NewStore(layout.ContentRoot).ListAll()
		if err != nil {
			return "", err
		}
		return loreMembersState(items, binding), nil
	case "game.openings":
		snapshot, err := s.snapshot(ctx, FileTarget{ProjectID: binding.Local.ProjectID, Path: openingPath})
		if err != nil {
			return "", err
		}
		if !snapshot.Exists {
			return "missing", nil
		}
		var current openings
		if err := json.Unmarshal(snapshot.Content, &current); err != nil {
			return "", err
		}
		return openingMembersState(current.Presets, binding), nil
	default:
		return "", fmt.Errorf("invalid collection kind %s", binding.Local.Kind)
	}
}

// A project export uses current local IDs; an acquired collection restores its
// source IDs so a round trip retains update identity and excludes unrelated work.
func (s *Service) collectionSourceIDs(ctx context.Context, ref LocalRef) (map[string]string, error) {
	if ref.ID == "all" {
		return nil, nil
	}
	installations, err := s.installations(ctx)
	if err != nil {
		return nil, err
	}
	for _, installation := range installations {
		for _, binding := range installation.Bindings {
			if binding.Local != ref {
				continue
			}
			ids := map[string]string{}
			for source, member := range binding.Members {
				ids[member.ID] = source
			}
			return ids, nil
		}
	}
	return nil, fmt.Errorf("collection not found")
}

// Collection payloads have already passed identity validation before staging.
func collectionSourceID(raw json.RawMessage) string {
	var value struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &value)
	return value.ID
}
