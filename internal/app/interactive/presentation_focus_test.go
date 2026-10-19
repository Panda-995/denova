package interactiveapp

import (
	"reflect"
	"testing"

	"denova/internal/interactive"
)

func TestBackgroundFocusPersistsInOpeningAndTurnJournal(t *testing.T) {
	workspace := t.TempDir()
	asset := presentationLoreFixture(t, workspace, "station")
	store := interactive.NewStore(workspace)
	background := &interactive.PresentationMaterial{ItemID: "station", AssetID: asset.ID, Focus: &interactive.ImageFocus{X: 0.2, Y: 0.8}}
	settings := &interactive.StoryPresentationSettings{Background: true, Characters: true, DefaultBackground: background}
	story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "Focus", PresentationSettings: settings})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(story.PresentationSettings.DefaultBackground.Focus, background.Focus) {
		t.Fatal("opening focus lost")
	}
	settings.DefaultBackground.Focus = &interactive.ImageFocus{X: 1, Y: 0}
	updated, err := store.UpdateStory(story.ID, interactive.UpdateStoryRequest{PresentationSettings: settings})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.PresentationSettings.DefaultBackground.Focus, background.Focus) {
		t.Fatal("same image focus edit lost")
	}
	turn, _, err := store.AppendTurnWithState(story.ID, interactive.AppendTurnWithStateRequest{BranchID: "main", Narrative: "Original", TurnResult: &interactive.TurnResult{Choices: []string{"One", "Two", "Three", "Four", "Five"}, Presentation: &interactive.TurnPresentation{Background: updated.PresentationSettings.DefaultBackground}}})
	if err != nil {
		t.Fatal(err)
	}
	background.Focus = &interactive.ImageFocus{X: 0.3, Y: 0.7}
	if err := store.UpdateTurnBackground(story.ID, interactive.UpdateTurnBackgroundRequest{BranchID: "main", TurnID: turn.ID, Background: background}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = interactive.NewStore(workspace)
	defer store.Close()
	snapshot, err := store.Snapshot(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	got := snapshot.CurrentTurn.TurnResult.Presentation.Background
	if !reflect.DeepEqual(got.Focus, background.Focus) || got.Path != asset.Path || snapshot.CurrentTurn.Narrative != "Original" {
		t.Fatalf("focus recovery changed scene: %+v", got)
	}
	background.Focus = &interactive.ImageFocus{X: -1, Y: 0.5}
	if err := store.UpdateTurnBackground(story.ID, interactive.UpdateTurnBackgroundRequest{BranchID: "main", TurnID: turn.ID, Background: background}); err == nil {
		t.Fatal("invalid focus accepted")
	}
	background.Focus = nil
	if err := store.UpdateTurnBackground(story.ID, interactive.UpdateTurnBackgroundRequest{BranchID: "main", TurnID: turn.ID, Background: background}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.Snapshot(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CurrentTurn.TurnResult.Presentation.Background.Focus != nil {
		t.Fatal("reset did not restore center")
	}
}
