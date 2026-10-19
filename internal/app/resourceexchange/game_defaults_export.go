package resourceexchange

import (
	"context"
	"fmt"
	"path"

	"denova/config"
	"denova/internal/book/lore"
	"github.com/google/uuid"
)

// The same path is used by Lore export and default-background references.
func exportedMaterialPath(projectID, localPath string) string {
	return "assets/" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(projectID+":"+localPath)).String() + path.Ext(localPath)
}

func (s *Service) exportGameDefaults(ctx context.Context, defaults *config.GameCreationDefaults, exported map[LocalRef]string) (*PackageGameDefaults, error) {
	if defaults == nil {
		return nil, nil
	}
	ref := func(kind string, id *string) string {
		if id == nil || *id == "" {
			return ""
		}
		return exported[LocalRef{Kind: kind, Scope: "global", ID: *id}]
	}
	result := &PackageGameDefaults{
		NarrativeStyleID: ref("preset.narrative", defaults.NarrativeStyleID), ImagePresetID: ref("preset.image", defaults.ImagePresetID),
		PlanningTemplateID: ref("preset.game_planning", defaults.PlanningTemplateID), ActorStateID: ref("preset.actor_state", defaults.ActorStateID), RuleSystemID: ref("preset.rules", defaults.RuleSystemID),
	}
	if defaults.EventPackageIDs != nil {
		for _, id := range *defaults.EventPackageIDs {
			mapped := ref("preset.events", &id)
			if mapped == "" {
				result.EventPackageIDs = nil
				break
			}
			result.EventPackageIDs = append(result.EventPackageIDs, mapped)
		}
	}
	if bg := defaults.DefaultBackground; bg != nil && bg.Mode == "image" {
		for local, resourceID := range exported {
			if local.Kind != "lore.collection" {
				continue
			}
			sourceID := bg.ItemID
			if local.ID != "all" {
				ids, err := s.collectionSourceIDs(ctx, local)
				if err != nil {
					return nil, err
				}
				var found bool
				sourceID, found = ids[bg.ItemID]
				if !found {
					continue
				}
			}
			_, layout, err := s.registry.Resolve(local.ProjectID, true)
			if err != nil {
				return nil, err
			}
			items, err := lore.NewStore(layout.ContentRoot).ListAll()
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				if item.ID != bg.ItemID || !item.Enabled {
					continue
				}
				for _, asset := range item.ResolvedMaterials {
					if asset.ID == bg.AssetID && asset.Path != "" {
						result.DefaultBackground = &PackageDefaultBackground{ResourceID: resourceID, ItemID: sourceID, AssetPath: exportedMaterialPath(local.ProjectID, asset.Path)}
					}
				}
				if result.DefaultBackground == nil {
					return nil, fmt.Errorf("default background is no longer available for export")
				}
			}
		}
	}
	return result, nil
}
