package app

import (
	"context"
	"log/slog"
	"slices"

	"denova/config"
	"denova/internal/interactive"
)

// Remember the normalized creation choices, not the Story's later tuning or
// generated content. A settings mutation preserves concurrent book-default edits.
func rememberGameCreation(ctx context.Context, projectStoreDir string, story interactive.StorySummary, customAgentID string) {
	if story.Preview || projectStoreDir == "" {
		return
	}
	refs := story.ModuleRefs
	events := slices.Clone(refs.EventPackageIDs)
	if len(events) == 0 {
		events = []string{}
	}
	resources := config.GameCreationDefaults{
		NarrativeStyleID:   &refs.NarrativeStyleID,
		ImagePresetID:      &refs.ImagePresetID,
		ActorStateID:       &refs.ActorStateID,
		RuleSystemID:       &refs.RuleSystemID,
		EventPackageIDs:    &events,
		PlanningTemplateID: &story.PlanningTemplateID,
		DefaultBackground:  &config.GameDefaultBackground{Mode: "none"},
	}
	presentation := interactive.NormalizeStoryPresentationSettings(story.PresentationSettings)
	if bg := presentation.DefaultBackground; bg != nil {
		resources.DefaultBackground = &config.GameDefaultBackground{Mode: "image", ItemID: bg.ItemID, AssetID: bg.AssetID}
	}
	preferences := &config.GameCreationPreferences{
		Resources: resources, CustomAgentID: customAgentID,
		Protagonist:      &config.GameCreationProtagonist{Mode: story.Protagonist.Mode},
		DisabledModules:  []string{},
		PlanningEnabled:  story.PlanningMode == "enabled",
		ReplyTargetChars: story.ReplyTargetChars, ChoiceCount: story.ChoiceCount,
		ImageMode: story.ImageSettings.Mode, ImageIntervalTurns: story.ImageSettings.IntervalTurns, ImagePresetID: story.ImageSettings.PresetID,
		DifficultyShift: story.CheckSettings.DifficultyShift, RollModifier: story.CheckSettings.RollModifier,
		RuleStateConsumptionMode: story.CheckSettings.RuleStateConsumptionMode, RuleVisibilityMode: story.CheckSettings.RuleVisibilityMode,
		StateSchemaMode: story.StateSchemaPolicy.Mode, Background: presentation.Background, Characters: presentation.Characters,
	}
	switch story.Protagonist.Mode {
	case interactive.StoryProtagonistModeLore:
		preferences.Protagonist.SourceLoreItemID = story.Protagonist.SourceLoreItemID
	case interactive.StoryProtagonistModeCustom:
		preferences.Protagonist.Name = story.Protagonist.Name
		preferences.Protagonist.Profile = story.Protagonist.Profile
	case interactive.StoryProtagonistModeDefault:
	}
	// Retain the chosen resource even while its module is switched off, so
	// turning it on in the next creation restores the user's selection.
	for _, module := range []struct {
		name     string
		disabled bool
	}{
		{"narrative_style_disabled", refs.NarrativeStyleDisabled},
		{"image_preset_disabled", refs.ImagePresetDisabled},
		{"actor_state_disabled", refs.ActorStateDisabled},
		{"rule_system_disabled", refs.RuleSystemDisabled},
		{"event_packages_disabled", refs.EventPackagesDisabled},
	} {
		if module.disabled {
			preferences.DisabledModules = append(preferences.DisabledModules, module.name)
		}
	}
	if bg := presentation.DefaultBackground; bg != nil && bg.Focus != nil {
		preferences.BackgroundFocus = &config.GameCreationImageFocus{X: bg.Focus.X, Y: bg.Focus.Y}
	}
	_, err := config.MutateSettingsFile(config.ProjectConfigPath(projectStoreDir), "", func(settings config.Settings) (config.Settings, error) {
		settings.GameLastCreation = preferences
		return settings, nil
	})
	if err != nil {
		// The Story is already durable. Reporting creation failure would invite a
		// duplicate Story on retry; retain the previous preference on write failure.
		slog.ErrorContext(ctx, "[story-setup] failed to remember creation preferences", "story_id", story.ID, "error", err)
		return
	}
	slog.InfoContext(ctx, "[story-setup] remembered creation preferences", "story_id", story.ID)
}
