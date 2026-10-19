package interactive

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestManualBackgroundPersistsWithoutChangingTurn(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	story, err := store.CreateStory(CreateStoryRequest{Title: "Manual background", PresentationSettings: &StoryPresentationSettings{Background: false, Characters: true}})
	if err != nil {
		t.Fatal(err)
	}
	ambient := &PresentationMaterial{ItemID: "room", AssetID: "rain", Path: "assets/rain.wav", Name: "Rain"}
	stage := &TurnPresentation{Ambient: ambient, Background: &PresentationMaterial{ItemID: "room", AssetID: "day", Path: "assets/day.png", Name: "Day"}, Characters: []PresentationMaterial{{ItemID: "hero", AssetID: "calm", Path: "assets/hero.png", Name: "Hero"}}}
	turn, _, err := store.AppendTurnWithState(story.ID, AppendTurnWithStateRequest{BranchID: "main", Narrative: "Original", TurnResult: &TurnResult{Choices: testTurnChoices(), Presentation: stage}})
	if err != nil {
		t.Fatal(err)
	}
	req := UpdateTurnPresentationRequest{BranchID: "main", TurnID: turn.ID, Presentation: []byte(`{"background":null}`)}
	if err := store.UpdateTurnPresentation(story.ID, req); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	indices, err := filepath.Glob(filepath.Join(root, "interactive", "story", "*.idx.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range indices {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	store = NewStore(root)
	defer store.Close()
	snapshot, err := store.Snapshot(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	want := *turn.TurnResult
	want.Presentation = stage.Clone()
	want.Presentation.Background = nil
	if snapshot.CurrentTurn.ID != turn.ID || snapshot.CurrentTurn.Narrative != turn.Narrative || !reflect.DeepEqual(snapshot.CurrentTurn.TurnResult, &want) {
		t.Fatalf("unexpected edited turn: %+v", snapshot.CurrentTurn)
	}
	page, err := store.ReadHistoryPage(story.ID, "main", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Turns) != 1 || !reflect.DeepEqual(page.Turns[0].TurnResult, &want) {
		t.Fatalf("history lost manual background: %+v", page)
	}
	scene, err := store.ReadSceneAtTurn(t.Context(), story.ID, "main", turn.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scene.Turn.TurnResult, &want) {
		t.Fatal("scene lost manual background")
	}
	req.Presentation = []byte(`{"background":{"item_id":"missing","asset_id":"missing"}}`)
	if err := store.UpdateTurnPresentation(story.ID, req); err == nil {
		t.Fatal("invalid material accepted")
	}
	if _, err := store.AppendTurn(story.ID, AppendTurnRequest{BranchID: "main", Narrative: "Next"}); err != nil {
		t.Fatal(err)
	}
	req.Presentation = []byte(`{"background":null}`)
	if err := store.UpdateTurnPresentation(story.ID, req); err == nil {
		t.Fatal("stale turn accepted")
	}
}
