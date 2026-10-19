package interactive

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"denova/internal/book"
	"denova/internal/book/lore"
)

var ErrDefaultBackground = errors.New("default background must be an enabled associated local image")

// resolveStoryPresentationSettings pins new selections server-side. Unchanged
// selections retain their saved locator even after their Lore association changes.
func (s *Store) resolveStoryPresentationSettings(input, current *StoryPresentationSettings) (*StoryPresentationSettings, error) {
	settings := NormalizeStoryPresentationSettings(input)
	selected := settings.DefaultBackground
	if selected == nil {
		return settings, nil
	}
	if !selected.Focus.valid() {
		return nil, ErrDefaultBackground
	}
	if current != nil && current.DefaultBackground != nil && selected.ItemID == current.DefaultBackground.ItemID && selected.AssetID == current.DefaultBackground.AssetID {
		settings.DefaultBackground = NormalizeStoryPresentationSettings(current).DefaultBackground
		settings.DefaultBackground.Focus = selected.Focus
		return settings, nil
	}
	raw, _ := json.Marshal(map[string]any{"background": selected})
	stage, receipt := ResolvePresentationPatch(s.root, nil, raw, nil)
	if receipt.Ignored > 0 {
		slog.Warn("[interactive-presentation] default background rejected", "item_id", selected.ItemID, "asset_id", selected.AssetID, "reasons", receipt.Reasons)
		return nil, ErrDefaultBackground
	}
	settings.DefaultBackground = stage.Background
	settings.DefaultBackground.Focus = selected.Focus
	return settings, nil
}

// ResolvePresentationPatch reads current enabled associations once; replay uses the pinned
// snapshot. No Lore error may prevent submission of the narrative and state.
func ResolvePresentationPatch(workspace string, base *TurnPresentation, raw json.RawMessage, settings *StoryPresentationSettings) (*TurnPresentation, *PresentationReceipt) {
	var items []lore.Item
	var loadErr error
	loaded := false
	return ApplyPresentationPatch(base, raw, settings, func(itemID, assetID string) (PresentationMaterial, error) {
		if !loaded {
			items, loadErr = lore.NewStore(workspace).List()
			loaded = true
		}
		if loadErr != nil {
			slog.Warn("[interactive-presentation] failed to read Lore materials", "error", loadErr)
			return PresentationMaterial{}, fmt.Errorf("material catalog unavailable")
		}
		for _, item := range items {
			if item.ID != itemID {
				continue
			}
			for _, material := range item.ResolvedMaterials {
				if material.ID != assetID || !strings.HasPrefix(material.MIMEType, "image/") {
					continue
				}
				full, err := book.SafePath(workspace, material.Path)
				if err != nil {
					break
				}
				root, rootErr := filepath.EvalSymlinks(workspace)
				resolved, pathErr := filepath.EvalSymlinks(full)
				if rootErr != nil || pathErr != nil || resolved != filepath.Join(root, filepath.FromSlash(material.Path)) {
					break
				}
				info, err := os.Stat(full)
				if err != nil || !info.Mode().IsRegular() {
					break
				}
				selected := PresentationMaterial{ItemID: item.ID, AssetID: material.ID, Path: material.Path, Name: material.Name}
				// Sixteen character slots plus a background leave room for the
				// full current stage within the 64 KiB context budget.
				encoded, _ := json.Marshal(selected)
				if len(encoded) > 2048 {
					return PresentationMaterial{}, fmt.Errorf("material identity exceeds 2048 bytes")
				}
				return selected, nil
			}
			break
		}
		return PresentationMaterial{}, fmt.Errorf("enabled associated image or file unavailable")
	})
}
