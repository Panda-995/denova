package config

import (
	"fmt"
	"slices"

	"denova/internal/portablepath"
)

// GameCreationDefaults belongs to one Project. It seeds new Stories only;
// changing it must never rewrite an existing Story or a user's global settings.
// A missing field inherits the existing creation behavior. An empty resource ID
// or event list explicitly disables that module instead of inheriting it.
type GameCreationDefaults struct {
	NarrativeStyleID   *string                `json:"narrative_style_id,omitempty" toml:"narrative_style_id,omitempty"`
	ImagePresetID      *string                `json:"image_preset_id,omitempty" toml:"image_preset_id,omitempty"`
	PlanningTemplateID *string                `json:"planning_template_id,omitempty" toml:"planning_template_id,omitempty"`
	ActorStateID       *string                `json:"actor_state_id,omitempty" toml:"actor_state_id,omitempty"`
	RuleSystemID       *string                `json:"rule_system_id,omitempty" toml:"rule_system_id,omitempty"`
	EventPackageIDs    *[]string              `json:"event_package_ids,omitempty" toml:"event_package_ids,omitempty"`
	DefaultBackground  *GameDefaultBackground `json:"default_background,omitempty" toml:"default_background,omitempty"`
}

// GameDefaultBackground stores a concrete Lore association, never a host path
// or a mutable cover selection. Mode "none" is an explicit empty background.
type GameDefaultBackground struct {
	Mode    string `json:"mode" toml:"mode"`
	ItemID  string `json:"item_id,omitempty" toml:"item_id,omitempty"`
	AssetID string `json:"asset_id,omitempty" toml:"asset_id,omitempty"`
}

func (d *GameCreationDefaults) Validate() error {
	if d == nil {
		return nil
	}
	for _, id := range []*string{d.NarrativeStyleID, d.ImagePresetID, d.PlanningTemplateID, d.ActorStateID, d.RuleSystemID} {
		if id != nil && *id != "" && portablepath.ValidateComponent(*id) != nil {
			return fmt.Errorf("invalid game default resource ID")
		}
	}
	if d.PlanningTemplateID != nil && *d.PlanningTemplateID == "" {
		return fmt.Errorf("game planning default requires a resource ID")
	}
	if d.EventPackageIDs != nil {
		if len(*d.EventPackageIDs) > 256 {
			return fmt.Errorf("too many default event packages")
		}
		for i, id := range *d.EventPackageIDs {
			if portablepath.ValidateComponent(id) != nil || slices.Contains((*d.EventPackageIDs)[:i], id) {
				return fmt.Errorf("invalid or duplicate default event package ID")
			}
		}
	}
	if bg := d.DefaultBackground; bg != nil {
		switch bg.Mode {
		case "none":
			if bg.ItemID != "" || bg.AssetID != "" {
				return fmt.Errorf("empty default background cannot reference an image")
			}
		case "image":
			if portablepath.ValidateComponent(bg.ItemID) != nil || portablepath.ValidateComponent(bg.AssetID) != nil {
				return fmt.Errorf("default background requires a Lore item and image")
			}
		default:
			return fmt.Errorf("invalid default background mode")
		}
	}
	return nil
}

// Select limits an adoption to the fields the user reviewed. Existing defaults
// outside that selection are preserved by the normal settings merge patch.
func (d *GameCreationDefaults) Select(fields []string) (*GameCreationDefaults, error) {
	selected := &GameCreationDefaults{}
	if d == nil && len(fields) > 0 {
		return nil, fmt.Errorf("package has no available game defaults")
	}
	for _, field := range fields {
		available := false
		switch field {
		case "narrative_style_id":
			selected.NarrativeStyleID, available = d.NarrativeStyleID, d.NarrativeStyleID != nil
		case "image_preset_id":
			selected.ImagePresetID, available = d.ImagePresetID, d.ImagePresetID != nil
		case "planning_template_id":
			selected.PlanningTemplateID, available = d.PlanningTemplateID, d.PlanningTemplateID != nil
		case "actor_state_id":
			selected.ActorStateID, available = d.ActorStateID, d.ActorStateID != nil
		case "rule_system_id":
			selected.RuleSystemID, available = d.RuleSystemID, d.RuleSystemID != nil
		case "event_package_ids":
			selected.EventPackageIDs, available = d.EventPackageIDs, d.EventPackageIDs != nil
		case "default_background":
			selected.DefaultBackground, available = d.DefaultBackground, d.DefaultBackground != nil
		}
		if !available {
			return nil, fmt.Errorf("game default %q is unavailable", field)
		}
	}
	return selected, nil
}
