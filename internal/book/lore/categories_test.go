package lore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReleasedCategoriesMigrateWithBackupAndRestore(t *testing.T) {
	store := NewStore(t.TempDir())
	legacy := []byte(`{"version":2,"items":[{"id":"hero","type":"character","name":"Hero","content":"Identity","tags":["主角"]},{"id":"guild","type":"faction","name":"Guild","content":"Organization"},{"id":"rule","type":"rule","name":"Rule","content":"Canon"},{"id":"template","type":"other","name":"Template","content":"Keep this text","tags":["模板"],"enabled":false}]}`)
	if err := os.MkdirAll(filepath.Dir(store.itemsPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.itemsPath(), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.Ensure(); err != nil {
			t.Fatal(err)
		}
	}
	collection, err := store.loadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(collection.Categories, DefaultCategories()) {
		t.Fatalf("unexpected categories: %+v", collection.Categories)
	}
	want := []string{"character", "faction", "world", "world"}
	for i, item := range collection.Items {
		if item.Type != want[i] {
			t.Fatalf("item %s category = %s", item.ID, item.Type)
		}
	}
	if collection.Items[3].Enabled || collection.Items[3].Content != "Keep this text" || !reflect.DeepEqual(collection.Items[3].Tags, []string{"模板"}) {
		t.Fatal("migration changed user content")
	}
	backups, err := filepath.Glob(filepath.Join(store.workspace, "setting", "lore", "backups", "categories-migration-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one retry-safe backup: %v %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(backup, legacy) {
		t.Fatal("backup does not preserve the released bytes", err)
	}
	// Restoring a released version follows the same conversion and preserves it.
	if err := os.WriteFile(store.itemsPath(), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(store.itemsPath())
	var persisted Collection
	if err := json.Unmarshal(raw, &persisted); err != nil || persisted.Version != loreItemsVersion {
		t.Fatalf("migration was not persisted: %s", raw)
	}
}

func TestCustomCategoryRenameMoveDeletePreservesLore(t *testing.T) {
	store := NewStore(t.TempDir())
	categories, err := store.MutateCategory(CategoryMutation{Op: "create", Name: "Abilities"})
	if err != nil {
		t.Fatal(err)
	}
	id := categories[len(categories)-1].ID
	item, err := store.Create(ItemInput{ID: "skill", Type: id, Name: "Flight", Content: "Stable ability", Tags: []string{"air"}, LoadMode: LoadModeManual})
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := store.AllRevision()
	if _, err := store.MutateCategory(CategoryMutation{Op: "rename", ID: id, Name: "Techniques"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyTypeChanges(revision, []TypeChange{{ID: item.ID, Type: "world"}}); err != ErrRevisionConflict {
		t.Fatalf("stale category preview accepted: %v", err)
	}
	categories, err = store.MutateCategory(CategoryMutation{Op: "move", ID: id, Index: 1})
	if err != nil || categories[1].ID != id {
		t.Fatalf("move failed: %+v %v", categories, err)
	}
	catalog, err := store.NameCatalogMarkdown(NameCatalogOptions{})
	if err != nil || !strings.Contains(catalog, id+": Techniques") || !strings.Contains(catalog, "Flight") {
		t.Fatalf("custom category is undiscoverable: %s %v", catalog, err)
	}
	if _, err := store.MutateCategory(CategoryMutation{Op: "delete", ID: "character", DestinationID: "world"}); err == nil {
		t.Fatal("removed functional character category")
	}
	if _, err := store.MutateCategory(CategoryMutation{Op: "delete", ID: id, DestinationID: "missing"}); err == nil {
		t.Fatal("accepted missing destination")
	}
	if _, err := store.MutateCategory(CategoryMutation{Op: "delete", ID: id, DestinationID: "world"}); err != nil {
		t.Fatal(err)
	}
	saved, err := NewStore(store.workspace).ReadAny(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Type != "world" || saved.Content != item.Content || saved.Name != item.Name || saved.LoadMode != item.LoadMode || !reflect.DeepEqual(saved.Tags, item.Tags) {
		t.Fatalf("category removal changed content: %+v", saved)
	}
	backups, _ := filepath.Glob(filepath.Join(store.workspace, "setting", "lore", "backups", "categories-removal-*.json"))
	if len(backups) != 1 {
		t.Fatal("category removal has no recovery copy")
	}
}

func TestCreateAndReclassifyAfterDefaultCategoryRemoval(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.MutateCategory(CategoryMutation{Op: "delete", ID: "world", DestinationID: "item"}); err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(ItemInput{Name: "New fact", Content: "Keep body"})
	if err != nil || created.Type != "location" {
		t.Fatalf("missing default category not handled: %+v %v", created, err)
	}
	revision, _ := store.AllRevision()
	applied, err := store.ApplyTypeChanges(revision, []TypeChange{{ID: created.ID, Type: "item"}})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := store.AllRevision()
	if applied.Revision != current {
		t.Fatal("applied revision does not match current collection")
	}
	if _, err := store.MutateCategory(CategoryMutation{Op: "create", Name: "角色"}); err == nil {
		t.Fatal("accepted duplicate localized name")
	}
	if _, err := store.Create(ItemInput{Name: "Stale type", Type: "world"}); err == nil {
		t.Fatal("accepted deleted category")
	}
}
