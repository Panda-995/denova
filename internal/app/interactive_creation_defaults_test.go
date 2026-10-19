package app

import (
	"reflect"
	"testing"

	"denova/config"
	appsettings "denova/internal/app/settings"
	"denova/internal/interactive"
	"denova/internal/interactive/teller"
)

func TestBookGameDefaultsSeedOnlyNewStories(t *testing.T) {
	a := newExecutionProfileTestApp(t)
	style, err := teller.NewLibrary(a.cfg.DataDir()).Get("classic")
	if err != nil {
		t.Fatal(err)
	}
	style.ID, style.Name = "cinematic", "Cinematic"
	if _, err := teller.NewLibrary(a.cfg.DataDir()).Create(style); err != nil {
		t.Fatal(err)
	}
	target := appsettings.Project(a.ProjectID())
	patch := func(raw string) {
		t.Helper()
		if _, err := a.SettingsService().Patch(target, config.SettingsLayerWorkspace, []byte(raw), ""); err != nil {
			t.Fatal(err)
		}
	}
	globalBefore, err := a.SettingsService().Snapshot(appsettings.Global())
	if err != nil {
		t.Fatal(err)
	}
	patch(`{"game_creation_defaults":{"narrative_style_id":"cinematic","image_preset_id":"","event_package_ids":[],"default_background":{"mode":"none"}}}`)
	first, err := a.CreateInteractiveStory(interactive.CreateStoryRequest{Title: "Uses book defaults"})
	if err != nil {
		t.Fatal(err)
	}
	if first.StoryTellerID != "cinematic" || first.ModuleRefs.NarrativeStyleID != "cinematic" || !first.ModuleRefs.ImagePresetDisabled || !first.ModuleRefs.EventPackagesDisabled {
		t.Fatalf("defaults not captured in story: %+v", first)
	}
	patch(`{"game_creation_defaults":{"narrative_style_id":"classic"}}`)
	explicit, err := a.CreateInteractiveStory(interactive.CreateStoryRequest{Title: "Explicit choice", StoryTellerID: "cinematic"})
	if err != nil || explicit.StoryTellerID != "cinematic" {
		t.Fatalf("explicit choice lost: %+v %v", explicit, err)
	}
	second, err := a.CreateInteractiveStory(interactive.CreateStoryRequest{Title: "New defaults"})
	if err != nil || second.StoryTellerID != "classic" {
		t.Fatalf("new defaults not read: %+v %v", second, err)
	}
	index, err := a.InteractiveStories()
	if err != nil {
		t.Fatal(err)
	}
	for _, story := range index.Stories {
		if story.ID == first.ID && !reflect.DeepEqual(story.ModuleRefs, first.ModuleRefs) {
			t.Fatal("existing story changed with book defaults")
		}
	}
	globalAfter, err := a.SettingsService().Snapshot(appsettings.Global())
	if err != nil || !reflect.DeepEqual(globalBefore.User, globalAfter.User) {
		t.Fatalf("global preferences changed: %v", err)
	}
}

func TestBookGameDefaultsDoNotRestrictStoryChoices(t *testing.T) {
	a := newExecutionProfileTestApp(t)
	if _, err := a.SettingsService().Patch(appsettings.Project(a.ProjectID()), config.SettingsLayerWorkspace, []byte(`{"game_creation_defaults":{"narrative_style_id":"missing","default_background":{"mode":"image","item_id":"missing","asset_id":"missing"}}}`), ""); err != nil {
		t.Fatal(err)
	}
	inherited, err := a.CreateInteractiveStory(interactive.CreateStoryRequest{PresentationSettings: &interactive.StoryPresentationSettings{Background: true, Characters: true}})
	if err != nil {
		t.Fatalf("unavailable recommendation blocked story creation: %v", err)
	}
	if inherited.ModuleRefs.NarrativeStyleID != "missing" {
		t.Fatalf("recommended resource choice was lost: %+v", inherited.ModuleRefs)
	}
	refs := interactive.DefaultStoryDirectorModuleRefs()
	refs.NarrativeStyleID = ""
	refs.NarrativeStyleDisabled = true
	story, err := a.CreateInteractiveStory(interactive.CreateStoryRequest{ModuleRefs: &refs, PresentationSettings: &interactive.StoryPresentationSettings{Background: true, Characters: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !story.ModuleRefs.NarrativeStyleDisabled || story.PresentationSettings.DefaultBackground != nil {
		t.Fatalf("explicit clear was overridden: %+v", story)
	}
	if _, err := a.CreateInteractiveStory(interactive.CreateStoryRequest{Preview: true}); err != nil {
		t.Fatalf("extension preview inherited book defaults: %v", err)
	}
}
