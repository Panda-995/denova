package interactiveapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"denova/internal/book/lore"
	"denova/internal/interactive"
)

func TestDefaultBackgroundSelectionPersistsAndSeedsOpening(t *testing.T) {
	workspace := t.TempDir()
	material := presentationLoreFixture(t, workspace, "station")
	store := interactive.NewStore(workspace)
	settings := &interactive.StoryPresentationSettings{Background: false, Characters: false, DefaultBackground: &interactive.PresentationMaterial{
		ItemID: "station", AssetID: material.ID, Path: "/untrusted/client/path.png", Name: "Client name",
	}}
	story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "Opening background", PlanningMode: interactive.StoryPlanningModeDisabled, PresentationSettings: settings})
	if err != nil {
		t.Fatal(err)
	}
	want := &interactive.PresentationMaterial{ItemID: "station", AssetID: material.ID, Path: material.Path, Name: material.Name}
	if !reflect.DeepEqual(story.PresentationSettings.DefaultBackground, want) {
		t.Fatalf("selection did not use the authoritative material: %#v", story.PresentationSettings)
	}
	c := NewConversation(store, t.TempDir(), workspace, story.ID, "main", "Enter", 800, nil)
	bindInteractiveCycleForTest(t, c)
	args := strings.TrimSuffix(gameStateArgs, "}") + `,"choices":["Enter","Observe","Listen","Inspect","Wait"],"presentation":{"background":null}}`
	receipt, err := c.SubmitTurnResult(t.Context(), interactive.DecodeInteractiveTurnSubmissionInput(args))
	if err != nil || !receipt.Ready || receipt.Presentation.Ignored != 1 {
		t.Fatalf("disabled dynamic selection: receipt=%#v err=%v", receipt, err)
	}
	if err := commitInteractiveAssistantForTest(t, c, "The station opens.", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = interactive.NewStore(workspace)
	defer store.Close()
	ctx, err := store.StoryContext(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ctx.Meta.PresentationSettings.DefaultBackground, want) || !reflect.DeepEqual(ctx.Snapshot.CurrentTurn.TurnResult.Presentation.Background, want) {
		t.Fatal("journal lost the configured or opening background")
	}
	// Association removal must not break unrelated toggles or change pinned history.
	if _, err := lore.NewStore(workspace).MutateMaterial("station", lore.MaterialMutation{Op: "remove", AssetID: material.ID}); err != nil {
		t.Fatal(err)
	}
	settings.Background = true
	updated, err := store.UpdateStory(story.ID, interactive.UpdateStoryRequest{PresentationSettings: settings})
	if err != nil || !reflect.DeepEqual(updated.PresentationSettings.DefaultBackground, want) {
		t.Fatalf("unchanged selection was not retained: %#v %v", updated, err)
	}
	settings.DefaultBackground.AssetID = "missing"
	if _, err := store.UpdateStory(story.ID, interactive.UpdateStoryRequest{PresentationSettings: settings}); !errors.Is(err, interactive.ErrDefaultBackground) {
		t.Fatalf("invalid selection accepted: %v", err)
	}
	settings.DefaultBackground = nil
	updated, err = store.UpdateStory(story.ID, interactive.UpdateStoryRequest{PresentationSettings: settings})
	if err != nil || updated.PresentationSettings.DefaultBackground != nil {
		t.Fatalf("could not clear default: %#v %v", updated, err)
	}
	snapshot, err := store.Snapshot(story.ID, "main")
	if err != nil || !reflect.DeepEqual(snapshot.CurrentTurn.TurnResult.Presentation.Background, want) {
		t.Fatal("changing the default rewrote committed history")
	}
}

func presentationLoreFixture(t *testing.T, workspace string, id string) lore.Material {
	t.Helper()
	store := lore.NewStore(workspace)
	if _, err := store.Create(lore.ItemInput{ID: id, Name: id, Type: "character", BriefDescription: "An investigator", Content: "Full lore body must not enter the material catalog."}); err != nil {
		t.Fatal(err)
	}
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	item, err := store.UploadMaterial(t.Context(), id, "happy.png", imageData.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return item.ResolvedMaterials[0]
}

func TestManualBackgroundSeedsNextTurnAndAllowsDynamicReplacement(t *testing.T) {
	workspace := t.TempDir()
	material := presentationLoreFixture(t, workspace, "station")
	store := interactive.NewStore(workspace)
	defer store.Close()
	story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "Manual background", PlanningMode: interactive.StoryPlanningModeDisabled, PresentationSettings: &interactive.StoryPresentationSettings{Background: false, Characters: true}})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := store.AppendTurn(story.ID, interactive.AppendTurnRequest{BranchID: "main", Narrative: "Opening"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateTurnBackground(story.ID, interactive.UpdateTurnBackgroundRequest{
		BranchID: "main", TurnID: turn.ID, Background: &interactive.PresentationMaterial{ItemID: "station", AssetID: material.ID, Path: "/untrusted/path"},
	}); err != nil {
		t.Fatal(err)
	}
	want := &interactive.PresentationMaterial{ItemID: "station", AssetID: material.ID, Path: material.Path, Name: material.Name}
	for _, step := range []struct{ dynamic, clear bool }{{false, true}, {true, false}, {true, true}} {
		if _, err := store.UpdateStory(story.ID, interactive.UpdateStoryRequest{PresentationSettings: &interactive.StoryPresentationSettings{Background: step.dynamic, Characters: true}}); err != nil {
			t.Fatal(err)
		}
		c := NewConversation(store, t.TempDir(), workspace, story.ID, "main", "Continue", 800, nil)
		bindInteractiveCycleForTest(t, c)
		args := strings.TrimSuffix(gameStateArgs, "}") + `,"choices":["Enter","Observe","Listen","Inspect","Wait"]`
		if step.clear {
			args += `,"presentation":{"background":null}`
		}
		receipt, err := c.SubmitTurnResult(t.Context(), interactive.DecodeInteractiveTurnSubmissionInput(args+"}"))
		if err != nil || !receipt.Ready {
			t.Fatalf("submission failed: %+v %v", receipt, err)
		}
		if err := commitInteractiveAssistantForTest(t, c, "The scene continues.", ""); err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.Snapshot(story.ID, "main")
		if err != nil {
			t.Fatal(err)
		}
		if step.clear && step.dynamic {
			want = nil
		}
		if !reflect.DeepEqual(snapshot.CurrentTurn.TurnResult.Presentation.Background, want) {
			t.Fatal("manual background inheritance or dynamic replacement failed")
		}
	}
}

func TestPresentationCatalogAndResolverUseEnabledAssociatedImages(t *testing.T) {
	workspace := t.TempDir()
	material := presentationLoreFixture(t, workspace, "hero")
	raw := json.RawMessage(fmt.Sprintf(`{"characters":[{"item_id":"hero","asset_id":%q}]}`, material.ID))
	stage, receipt := interactive.ResolvePresentationPatch(workspace, nil, raw, nil)
	if receipt.Applied != 1 || stage.Characters[0].Path != material.Path {
		t.Fatalf("resolve=%#v %#v", stage, receipt)
	}
	source := buildPresentationContext(workspace, nil, stage, nil, "hero")
	if !strings.Contains(source.Content, material.ID) || !strings.Contains(source.Content, "An investigator") || strings.Contains(source.Content, "Full lore body") || source.Limit != 64*1024 {
		t.Fatalf("catalog=%s", source.Content)
	}
	if again := buildPresentationContext(workspace, nil, stage, nil, "hero"); again.Content != source.Content {
		t.Fatal("catalog order is unstable")
	}
	// Removing an association makes new selections invalid without changing a
	// committed stage's concrete locator.
	if _, err := lore.NewStore(workspace).MutateMaterial("hero", lore.MaterialMutation{Op: "remove", AssetID: material.ID}); err != nil {
		t.Fatal(err)
	}
	preserved, receipt := interactive.ResolvePresentationPatch(workspace, stage, raw, nil)
	if receipt.Ignored != 1 || !reflect.DeepEqual(preserved, stage) {
		t.Fatal("missing association erased an existing sprite")
	}
	if err := os.Remove(filepath.Join(workspace, filepath.FromSlash(material.Path))); err != nil {
		t.Fatal(err)
	}
	disabled := buildPresentationContext(workspace, &interactive.StoryPresentationSettings{}, stage, nil, "hero")
	if strings.Contains(disabled.Content, "image catalog") || !strings.Contains(disabled.Content, "Both layers are disabled") {
		t.Fatal("disabled layers injected a catalog")
	}
}

func TestPresentationCatalogSkipsOversizeCompleteItems(t *testing.T) {
	workspace := t.TempDir()
	material := presentationLoreFixture(t, workspace, "oversize")
	if _, err := lore.NewStore(workspace).MutateMaterial("oversize", lore.MaterialMutation{Op: "update", AssetID: material.ID, Description: strings.Repeat("x", presentationContextMaxBytes)}); err != nil {
		t.Fatal(err)
	}
	presentationLoreFixture(t, workspace, "small")
	source := buildPresentationContext(workspace, nil, nil, nil, "oversize")
	if len(source.Content) > presentationContextMaxBytes || !source.Truncated || !strings.Contains(source.Content, `"item_id":"small"`) || strings.Contains(source.Content, `"item_id":"oversize"`) || !strings.Contains(source.Content, "1 items (1 images) omitted") {
		t.Fatalf("bounded catalog=%s", source.Content)
	}
}

func TestPresentationSubmissionRecoversAcceptedStageAndIgnoresBadReferences(t *testing.T) {
	workspace := t.TempDir()
	material := presentationLoreFixture(t, workspace, "hero")
	store := interactive.NewStore(workspace)
	defer store.Close()
	story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "Stage recovery", PlanningMode: interactive.StoryPlanningModeDisabled})
	if err != nil {
		t.Fatal(err)
	}
	c := NewConversation(store, t.TempDir(), workspace, story.ID, "main", "Enter", 800, nil)
	bindInteractiveCycleForTest(t, c)
	args := strings.TrimSuffix(gameStateArgs, "}") + fmt.Sprintf(`,"presentation":{"background":{"item_id":"missing","asset_id":"bad"},"characters":[{"item_id":"hero","asset_id":%q}]}}`, material.ID)
	receipt, err := c.SubmitTurnResult(t.Context(), interactive.DecodeInteractiveTurnSubmissionInput(args))
	if err != nil || receipt.Ready || receipt.Presentation.Applied != 1 || receipt.Presentation.Ignored != 1 || !reflect.DeepEqual(receipt.RetryModules, []string{"choices"}) {
		t.Fatalf("partial receipt=%#v err=%v", receipt, err)
	}
	restored := NewConversation(store, t.TempDir(), workspace, story.ID, "main", "Enter", 800, nil)
	restored.BindAgentCycleIdentity(c.AgentCycleIdentitySnapshot())
	receipt, err = restored.SubmitTurnResult(t.Context(), interactive.DecodeInteractiveTurnSubmissionInput(`{"choices":["Enter","Observe","Listen","Inspect","Wait"],"presentation":{"characters":[{"item_id":"hero","asset_id":"typo"}]}}`))
	if err != nil || !receipt.Ready || len(receipt.RetryModules) != 0 || receipt.Presentation.Ignored != 1 {
		t.Fatalf("visual error blocked readiness: %#v %v", receipt, err)
	}
	if err := commitInteractiveAssistantForTest(t, restored, "The investigator arrives.", ""); err != nil {
		t.Fatal(err)
	}
	snapshot, err := interactive.NewStore(workspace).Snapshot(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CurrentTurn == nil || snapshot.CurrentTurn.TurnResult.Presentation.Characters[0].AssetID != material.ID {
		t.Fatal("recovered turn lost accepted sprite")
	}
}
