package resourceexchange

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/book/character"
	"denova/internal/book/lore"
	"denova/internal/project"
)

func TestCharacterPreviewKeepsCustomClassificationAndDeletedCategories(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	workspace := filepath.Join(s.root, "projects", "custom-card")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	book, err := s.registry.Add(workspace, project.TypeBook, "Custom card")
	if err != nil {
		t.Fatal(err)
	}
	store := lore.NewStore(workspace)
	categories, err := store.MutateCategory(lore.CategoryMutation{Op: "create", Name: "Abilities"})
	if err != nil {
		t.Fatal(err)
	}
	id := categories[len(categories)-1].ID
	categories, err = store.MutateCategory(lore.CategoryMutation{Op: "delete", ID: "location", DestinationID: "world"})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"spec":"chara_card_v2","data":{"name":"Explorer","description":"A careful explorer.","character_book":{"entries":[{"comment":"Ascension","content":"Flight skill.","enabled":true},{"comment":"Location: Harbor","content":"A quiet harbor.","enabled":true}]}}}`)
	called := 0
	preview, err := s.PreviewCharacter(ctx, Source{Kind: "file", Filename: "explorer.json"}, raw, character.ImportOptions{
		ClassificationMode: lore.ClassificationModeSemantic,
		ClassifyLore: func(inputs []lore.ClassificationInput) ([]lore.ClassificationSuggestion, error) {
			called++
			return []lore.ClassificationSuggestion{{ID: inputs[0].ID, Type: id, Confidence: lore.ClassificationConfidenceHigh}}, nil
		},
	}, categories)
	if err != nil {
		t.Fatal(err)
	}
	candidate := preview.Candidates[0]
	selected := []string{}
	for _, resource := range candidate.Resources {
		selected = append(selected, resource.ID)
	}
	plan, err := s.Plan(ctx, PlanRequest{PreviewID: preview.ID, CandidateID: candidate.ID, ProjectID: book.ID, Resources: selected})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListAll()
	if err != nil || len(items) != 3 || called != 1 {
		t.Fatalf("unexpected conversion: %+v, calls=%d, error=%v", items, called, err)
	}
	for _, item := range items {
		if item.Name == "Ascension" && item.Type != id {
			t.Fatalf("lost custom classification: %+v", item)
		}
		if item.Name == "Location: Harbor" && item.Type != "world" {
			t.Fatalf("used a deleted category: %+v", item)
		}
	}
	actual, err := store.Categories()
	if err != nil || lore.HasCategory(actual, "location") {
		t.Fatalf("import restored a deleted category: %+v %v", actual, err)
	}
}
