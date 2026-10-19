package app

import (
	"path/filepath"
	"slices"

	"denova/config"
	"denova/internal/interactive"
)

// Creation choices outrank book defaults, then the book's last selected Story.
// Resume, updates and extension preview Stories never consult these defaults.
func (s *InteractiveAppService) withGameCreationDefaults(req interactive.CreateStoryRequest) (interactive.CreateStoryRequest, error) {
	cfg := s.cfg()
	if req.Preview || cfg == nil || cfg.ProjectStoreDir == "" {
		return req, nil
	}
	settings, err := config.ReadSettingsFile(filepath.Join(cfg.ProjectStoreDir, "config.toml"))
	if err != nil {
		return req, err
	}
	d := settings.GameCreationDefaults
	if (d == nil || d.NarrativeStyleID == nil) && req.StoryTellerID == "" && (req.ModuleRefs == nil || req.ModuleRefs.NarrativeStyleID == "" && !req.ModuleRefs.NarrativeStyleDisabled) {
		index, err := s.store().Index()
		if err != nil {
			return req, err
		}
		for _, story := range index.Stories {
			if story.ID != index.CurrentStoryID || story.Preview {
				continue
			}
			refs := interactive.StoryDirectorModuleRefs{}
			if req.ModuleRefs != nil {
				refs = *req.ModuleRefs
			}
			refs.NarrativeStyleID = story.StoryTellerID
			if story.ModuleRefs != nil {
				refs.NarrativeStyleDisabled = story.ModuleRefs.NarrativeStyleDisabled
				if story.ModuleRefs.NarrativeStyleID != "" {
					refs.NarrativeStyleID = story.ModuleRefs.NarrativeStyleID
				}
			}
			req.ModuleRefs = &refs
			break
		}
	}
	if d == nil {
		return req, nil
	}
	return applyBookGameDefaults(req, d), nil
}

// Book defaults only fill missing choices. Resource resolution belongs to the
// normal Story flow and must not impose the recommended resource combination.
func applyBookGameDefaults(req interactive.CreateStoryRequest, d *config.GameCreationDefaults) interactive.CreateStoryRequest {
	refs := interactive.StoryDirectorModuleRefs{}
	if req.ModuleRefs != nil {
		refs = *req.ModuleRefs
		refs.EventPackageIDs = slices.Clone(refs.EventPackageIDs)
	}
	builtin := interactive.DefaultStoryDirectorModuleRefs()
	// The explicit narrator input is an existing API alias and outranks book defaults.
	if req.StoryTellerID != "" && refs.NarrativeStyleID == "" && !refs.NarrativeStyleDisabled {
		refs.NarrativeStyleID = req.StoryTellerID
	}
	for _, selection := range []struct {
		id       *string
		disabled *bool
		value    *string
		fallback string
	}{
		{&refs.NarrativeStyleID, &refs.NarrativeStyleDisabled, d.NarrativeStyleID, builtin.NarrativeStyleID},
		{&refs.ImagePresetID, &refs.ImagePresetDisabled, d.ImagePresetID, builtin.ImagePresetID},
		{&refs.ActorStateID, &refs.ActorStateDisabled, d.ActorStateID, builtin.ActorStateID},
		{&refs.RuleSystemID, &refs.RuleSystemDisabled, d.RuleSystemID, builtin.RuleSystemID},
	} {
		if *selection.id != "" || *selection.disabled {
			continue
		}
		if selection.value == nil {
			*selection.id = selection.fallback
		} else {
			*selection.id = *selection.value
			*selection.disabled = *selection.value == ""
		}
	}
	if refs.ActorStateDisabled && req.StateSchemaPolicy == nil {
		req.StateSchemaPolicy = &interactive.StoryStateSchemaPolicy{Mode: interactive.StoryStateSchemaModeGenerate}
	}
	if refs.EventPackageIDs == nil && !refs.EventPackagesDisabled {
		refs.EventPackageIDs = slices.Clone(builtin.EventPackageIDs)
		if d.EventPackageIDs != nil {
			refs.EventPackageIDs = slices.Clone(*d.EventPackageIDs)
			refs.EventPackagesDisabled = len(refs.EventPackageIDs) == 0
		}
	}
	if req.PlanningTemplateID == "" && d.PlanningTemplateID != nil {
		req.PlanningTemplateID = *d.PlanningTemplateID
	}
	if req.PresentationSettings == nil && d.DefaultBackground != nil {
		req.PresentationSettings = interactive.NormalizeStoryPresentationSettings(nil)
		if bg := d.DefaultBackground; bg.Mode == "image" {
			req.PresentationSettings.DefaultBackground = &interactive.PresentationMaterial{ItemID: bg.ItemID, AssetID: bg.AssetID}
		}
	}
	req.ModuleRefs = &refs
	return req
}
