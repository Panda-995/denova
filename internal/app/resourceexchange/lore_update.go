package resourceexchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"

	"denova/internal/book/lore"
	"denova/internal/revisionfile"
)

// Content equality excludes generated identities and timestamps, but includes
// material bytes and association text. Identical local/upstream edits converge.
func loreUpdateDigest(item lore.Item, materials portableMaterials, read func(string) ([]byte, error)) (string, error) {
	materials.Entries = slices.Clone(materials.Entries)
	item.ID, item.CreatedAt, item.UpdatedAt = "", "", ""
	item.Image, item.Materials, item.ResolvedMaterials = nil, nil, nil
	for i := range materials.Entries {
		m := &materials.Entries[i]
		m.Name, m.Description = strings.TrimSpace(m.Name), strings.TrimSpace(m.Description)
		if m.URL != "" {
			parsed, err := url.Parse(m.URL)
			if err != nil {
				return "", err
			}
			m.OriginalName = path.Base(parsed.Path)
			if m.OriginalName == "." || m.OriginalName == "/" || m.OriginalName == "" {
				m.OriginalName = parsed.Hostname()
			}
			m.SourceURL = ""
		} else if m.OriginalName == "" {
			m.OriginalName = path.Base(m.AssetPath)
		}
		if m.Name == "" {
			m.Name = m.OriginalName
		}
		if m.AssetPath == "" {
			continue
		}
		raw, err := read(m.AssetPath)
		if err != nil {
			return "", err
		}
		digest := revisionfile.Revision(raw)
		if materials.CoverPath == m.AssetPath {
			materials.CoverPath = digest
		}
		m.AssetPath = digest
	}
	if len(materials.Entries) == 0 {
		materials.Entries = nil
	}
	raw, err := json.Marshal(struct {
		Item      lore.Item
		Materials portableMaterials
	}{item, materials})
	if err != nil {
		return "", err
	}
	return revisionfile.Revision(raw), nil
}

func (s *Service) localLoreUpdateDigest(ctx context.Context, projectID string, item lore.Item) (string, error) {
	materials := portableMaterials{}
	for _, m := range item.ResolvedMaterials {
		materials.Entries = append(materials.Entries, portableMaterial{AssetPath: m.Path, URL: m.URL, SourceURL: m.Source.URL, OriginalName: m.OriginalName, Name: m.Name, Description: m.Description})
		if item.Materials != nil && item.Materials.CoverAssetID == m.ID {
			materials.CoverPath, materials.CoverURL = m.Path, m.URL
		}
	}
	return loreUpdateDigest(item, materials, func(name string) ([]byte, error) {
		snapshot, err := s.snapshot(ctx, FileTarget{ProjectID: projectID, Path: name})
		if err != nil {
			return nil, err
		}
		if !snapshot.Exists {
			return nil, fmt.Errorf("lore material missing: %s: %w", name, os.ErrNotExist)
		}
		return snapshot.Content, nil
	})
}
