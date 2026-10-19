package resourceexchange

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"denova/config"
	"denova/internal/book/lore"
	toml "github.com/pelletier/go-toml/v2"
)

// PackageGameDefaults is a declarative recommendation. IDs reference manifest
// resources, not host libraries. Importing it never activates any resources.
type PackageGameDefaults struct {
	NarrativeStyleID   string                    `json:"narrative_style_id,omitempty"`
	ImagePresetID      string                    `json:"image_preset_id,omitempty"`
	PlanningTemplateID string                    `json:"planning_template_id,omitempty"`
	ActorStateID       string                    `json:"actor_state_id,omitempty"`
	RuleSystemID       string                    `json:"rule_system_id,omitempty"`
	EventPackageIDs    []string                  `json:"event_package_ids,omitempty"`
	DefaultBackground  *PackageDefaultBackground `json:"default_background,omitempty"`
}

type PackageDefaultBackground struct {
	ResourceID string `json:"resource_id"`
	ItemID     string `json:"item_id"`
	AssetPath  string `json:"asset_path"`
}

func validatePackageGameDefaults(dir string, candidate PackagePreview) error {
	d := candidate.GameDefaults
	if d == nil {
		return nil
	}
	check := func(id, kind string) error {
		if id != "" && !slices.ContainsFunc(candidate.Resources, func(r PreviewResource) bool { return r.ID == id && r.Kind == kind }) {
			return fmt.Errorf("game default %q must reference a %s resource", id, kind)
		}
		return nil
	}
	for _, ref := range []struct{ id, kind string }{
		{d.NarrativeStyleID, "preset.narrative"}, {d.ImagePresetID, "preset.image"},
		{d.PlanningTemplateID, "preset.game_planning"}, {d.ActorStateID, "preset.actor_state"}, {d.RuleSystemID, "preset.rules"},
	} {
		if err := check(ref.id, ref.kind); err != nil {
			return err
		}
	}
	for i, id := range d.EventPackageIDs {
		if id == "" || slices.Contains(d.EventPackageIDs[:i], id) {
			return fmt.Errorf("invalid default event package")
		}
		if err := check(id, "preset.events"); err != nil {
			return err
		}
	}
	if bg := d.DefaultBackground; bg != nil {
		if bg.ResourceID == "" || bg.ItemID == "" || bg.AssetPath == "" {
			return fmt.Errorf("default background requires a collection, item and asset")
		}
		if err := check(bg.ResourceID, "lore.collection"); err != nil {
			return err
		}
		resource := candidate.Resources[slices.IndexFunc(candidate.Resources, func(r PreviewResource) bool { return r.ID == bg.ResourceID })]
		raw, err := os.ReadFile(filepath.Join(dir, "files", filepath.FromSlash(resource.Path)))
		if err != nil {
			return err
		}
		collection, items, err := readLoreCollection(raw)
		if err != nil {
			return err
		}
		at := slices.IndexFunc(items, func(item lore.Item) bool { return item.ID == bg.ItemID && item.Enabled })
		if at < 0 {
			return fmt.Errorf("default background item is missing or disabled")
		}
		var payload struct {
			Materials *portableMaterials `json:"materials"`
		}
		if err := json.Unmarshal(collection.Items[at], &payload); err != nil {
			return err
		}
		if payload.Materials == nil || !slices.ContainsFunc(payload.Materials.Entries, func(m portableMaterial) bool { return m.AssetPath == bg.AssetPath }) {
			return fmt.Errorf("default background is not associated with its Lore item")
		}
		if _, err := resourceAsset(dir, resource, bg.AssetPath); err != nil {
			return err
		}
	}
	return nil
}

// Only imported resources become available recommendations. Missing selections
// never pull in resources, grant permissions, or silently change other choices.
func resolvePackageGameDefaults(candidate PackagePreview, bindings []Binding, assets map[FileTarget]lore.Asset) (*config.GameCreationDefaults, error) {
	d := candidate.GameDefaults
	if d == nil {
		return nil, nil
	}
	result := &config.GameCreationDefaults{}
	resolve := func(id string) *string {
		for _, binding := range bindings {
			if id != "" && binding.ResourceID == id {
				value := binding.Local.ID
				return &value
			}
		}
		return nil
	}
	result.NarrativeStyleID = resolve(d.NarrativeStyleID)
	result.ImagePresetID = resolve(d.ImagePresetID)
	result.PlanningTemplateID = resolve(d.PlanningTemplateID)
	result.ActorStateID = resolve(d.ActorStateID)
	result.RuleSystemID = resolve(d.RuleSystemID)
	if len(d.EventPackageIDs) > 0 {
		ids := []string{}
		for _, id := range d.EventPackageIDs {
			local := resolve(id)
			if local == nil {
				ids = nil
				break
			}
			ids = append(ids, *local)
		}
		if ids != nil {
			result.EventPackageIDs = &ids
		}
	}
	if bg := d.DefaultBackground; bg != nil {
		for _, binding := range bindings {
			if binding.ResourceID != bg.ResourceID {
				continue
			}
			resource := candidate.Resources[slices.IndexFunc(candidate.Resources, func(r PreviewResource) bool { return r.ID == bg.ResourceID })]
			asset := assets[FileTarget{ProjectID: binding.Local.ProjectID, Path: path.Join(resource.Root, bg.AssetPath)}]
			if asset.ID == "" {
				continue
			}
			if !strings.HasPrefix(asset.MIMEType, "image/") {
				return nil, fmt.Errorf("default background must be an image")
			}
			member := binding.Members[bg.ItemID]
			if member.ID == "" {
				return nil, fmt.Errorf("default background item mapping is missing")
			}
			result.DefaultBackground = &config.GameDefaultBackground{Mode: "image", ItemID: member.ID, AssetID: asset.ID}
		}
	}
	return result, result.Validate()
}

// Adoption joins the existing import transaction, including its revision check
// and before-image. It never becomes a package-owned file or update baseline.
func (s *Service) stageGameDefaults(ctx context.Context, request PlanRequest, plan *Plan, staged map[FileTarget][]byte, expected map[FileTarget]string) error {
	if len(request.GameDefaultsFields) == 0 {
		return nil
	}
	if request.automatic || request.ProjectID == "" {
		return fmt.Errorf("adopting defaults requires an explicit Project selection")
	}
	if plan.Installation.ProjectID != "" && plan.Installation.ProjectID != request.ProjectID {
		return fmt.Errorf("game defaults must belong to the installation Project")
	}
	if len(plan.GameDefaultsUnavailable) > 0 {
		return nil
	}
	selected, err := plan.Installation.GameDefaults.Select(request.GameDefaultsFields)
	if err != nil {
		return err
	}
	record, _, err := s.registry.Resolve(request.ProjectID, true)
	if err != nil {
		return err
	}
	target := FileTarget{Path: path.Join("stores", record.StoreDirName, "config.toml")}
	snapshot, err := s.snapshot(ctx, target)
	if err != nil {
		return err
	}
	var current config.Settings
	if err := toml.Unmarshal(snapshot.Content, &current); err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]any{"game_creation_defaults": selected})
	if err != nil {
		return err
	}
	next, err := config.ApplySettingsMergePatch(current, patch)
	if err != nil {
		return err
	}
	content, err := toml.Marshal(next)
	if err != nil {
		return err
	}
	staged[target], expected[target] = content, snapshot.Revision
	plan.GameDefaultsBefore, plan.GameDefaultsApplied = current.GameCreationDefaults, selected
	plan.Installation.ProjectID = request.ProjectID
	return nil
}
