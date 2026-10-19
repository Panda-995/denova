package lore

import "testing"

func TestCreateLoreNeedsANameForGeneratedIdentity(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Create(ItemInput{Name: "✨---", Type: "character"}); err == nil {
		t.Fatal("symbol-only name must not fall back to a category ID")
	}
	if _, err := store.ApplyOperations("Create lore", []Operation{{Op: "create", Item: ItemInput{Name: "✨", Type: "world"}}}); err == nil {
		t.Fatal("batch creation must not fall back to a category ID")
	}
	item, err := store.Create(ItemInput{Name: "沈凝", Type: "character"})
	if err != nil || item.ID != "沈凝" {
		t.Fatalf("unexpected created identity: %+v %v", item, err)
	}
	renamed, err := store.Update(item.ID, ItemInput{Name: "沈宁", Type: item.Type})
	if err != nil || renamed.ID != item.ID {
		t.Fatalf("renaming changed identity: %+v %v", renamed, err)
	}
	items, err := store.ListAll()
	if err != nil || len(items) != 1 || items[0].ID != "沈凝" || items[0].Name != "沈宁" {
		t.Fatalf("invalid creation or rename corrupted persisted items: %+v %v", items, err)
	}
}
