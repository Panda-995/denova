package lore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func saveTestIndex(t *testing.T, store *Store, guide IndexGuide) {
	t.Helper()
	current, err := store.IndexGuide()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateIndexGuide(IndexGuideUpdate{Guide: guide, BaseRevision: current.Revision}); err != nil {
		t.Fatal(err)
	}
}

func TestLoreIndexPublishesNamesWithoutToolHopOrInternalIDs(t *testing.T) {
	store := NewStore(t.TempDir())
	saveTestIndex(t, store, IndexGuide{Groups: []IndexGroup{{ID: "harbor", Name: "Harbor", DefaultDetail: IndexDetailName}}})
	item, err := store.Create(ItemInput{ID: "internal-long-identity-12345", Name: "Harbor keeper", Content: "PRIVATE_BODY", IndexMemberships: []IndexMembership{{"harbor", IndexDetailInherit}}})
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := store.ProgressiveContextMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown, "Harbor keeper") || strings.Contains(markdown, item.ID) || strings.Contains(markdown, "PRIVATE_BODY") {
		t.Fatalf("name-only sections must publish names directly, without IDs or bodies: %s", markdown)
	}
}

func TestLoreIndexAutomaticGroupsFollowItemConfiguration(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, input := range []ItemInput{
		{Name: "World law", Type: "world", LoadMode: LoadModeResident, Content: "RESIDENT_CANON"},
		{Name: "Captain", Type: "character", LoadMode: LoadModeAuto, Content: "CAPTAIN_BODY"},
		{Name: "Docks", Type: "location", LoadMode: LoadModeManual, Content: "DOCKS_BODY"},
	} {
		if _, err := store.Create(input); err != nil {
			t.Fatal(err)
		}
	}
	markdown, err := store.ProgressiveContextMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Resident", "RESIDENT_CANON", "## Character · On demand", "Captain", "## Location · Manual", "Docks"} {
		if !strings.Contains(markdown, want) {
			t.Errorf("automatic injection missing %q", want)
		}
	}
	for _, hidden := range []string{"CAPTAIN_BODY", "DOCKS_BODY", "ID:"} {
		if strings.Contains(markdown, hidden) {
			t.Errorf("automatic index leaked %q", hidden)
		}
	}
}

func TestLoreIndexDirectLoadingAndCacheStability(t *testing.T) {
	store := NewStore(t.TempDir())
	guide := IndexGuide{IntroMarkdown: "Explore before inventing.", Groups: []IndexGroup{
		{ID: "rules", Name: "World rules", Purpose: "Constraints for every scene", BodyMarkdown: "GROUP_GUIDE", DefaultDetail: IndexDetailBrief},
		{ID: "cast", Name: "Recurring cast", Purpose: "People available for reuse", DefaultDetail: IndexDetailName},
		{ID: "full", Name: "Essentials", DefaultDetail: IndexDetailFull},
	}}
	saveTestIndex(t, store, guide)
	disabled := false
	var cast Item
	for _, input := range []ItemInput{
		{Name: "Resident override", LoadMode: LoadModeResident, BriefDescription: "Resident summary", Content: "RESIDENT_BODY", IndexMemberships: []IndexMembership{{"cast", IndexDetailInherit}}},
		{Name: "Shared", LoadMode: LoadModeAuto, BriefDescription: "Shared summary", Content: "SHARED_BODY", IndexMemberships: []IndexMembership{{"rules", IndexDetailInherit}, {"full", IndexDetailInherit}}},
		{Name: "Brief", LoadMode: LoadModeAuto, BriefDescription: "BRIEF_SUMMARY", Content: "BRIEF_BODY", IndexMemberships: []IndexMembership{{"rules", IndexDetailInherit}}},
		{Name: "Fresh discovery", Content: "FRESH_BODY"},
		{Name: "Disabled", Enabled: &disabled, LoadMode: LoadModeResident, Content: "DISABLED_BODY", IndexMemberships: []IndexMembership{{"full", IndexDetailInherit}}},
	} {
		item, err := store.Create(input)
		if err != nil {
			t.Fatal(err)
		}
		if input.Name == "Resident override" {
			cast = item
		}
	}
	initial, err := store.ProgressiveContextMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Lore Index", "Explore before inventing.", "GROUP_GUIDE", "Recurring cast", "People available for reuse", "BRIEF_SUMMARY", "Fresh discovery", "Resident override", "SHARED_BODY"} {
		if !strings.Contains(initial, want) {
			t.Errorf("initial context missing %q", want)
		}
	}
	for _, hidden := range []string{"RESIDENT_BODY", "BRIEF_BODY", "FRESH_BODY", "DISABLED_BODY", cast.ID, `"index_memberships"`} {
		if strings.Contains(initial, hidden) {
			t.Errorf("initial context leaked %q", hidden)
		}
	}
	if strings.Count(initial, "SHARED_BODY") != 1 {
		t.Fatal("shared full body must appear once")
	}
	if _, err := store.ApplyOperations("Update unloaded body", []Operation{{Op: "update", ID: cast.ID, Item: ItemInput{Content: "NEW_BODY"}}}); err != nil {
		t.Fatal(err)
	}
	after, _ := store.ProgressiveContextMarkdown()
	if initial != after {
		t.Fatal("unloaded item bodies must not rotate the index prefix")
	}
	page, err := store.Query(QueryOptions{IndexOptions: IndexOptions{GroupNames: []string{"Recurring cast"}}})
	if err != nil || len(page.Items) != 1 || page.Items[0].Content != "NEW_BODY" || page.Items[0].Name != cast.Name {
		t.Fatalf("full group query: %#v, %v", page, err)
	}
	guide.Groups[0].DefaultDetail = IndexDetailName
	saveTestIndex(t, store, guide)
	after, _ = store.ProgressiveContextMarkdown()
	if strings.Contains(after, "BRIEF_SUMMARY") || strings.Count(after, "SHARED_BODY") != 1 {
		t.Fatalf("incorrect inherited detail: %s", after)
	}
	if guide.IncludesFullBody(cast) {
		t.Fatal("authored name-only membership must control resident items too")
	}
}

func TestLoreIndexAutomaticPresetAndMembershipPrecedence(t *testing.T) {
	store := NewStore(t.TempDir())
	guide := IndexGuide{AutomaticDetails: map[string]string{"auto:character": IndexDetailBrief}, Groups: []IndexGroup{{ID: "names", Name: "Cast", DefaultDetail: IndexDetailName}}}
	saveTestIndex(t, store, guide)
	item, err := store.Create(ItemInput{Name: "Captain", Type: "character", LoadMode: LoadModeAuto, BriefDescription: "CAPTAIN_BRIEF", Content: "CAPTAIN_BODY"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewIndexGuide(guide)
	if err != nil || len(preview.AutomaticGroups) != 1 || !strings.Contains(preview.Markdown, "CAPTAIN_BRIEF") || strings.Contains(preview.Markdown, item.ID) {
		t.Fatalf("incorrect automatic preview: %#v %v", preview, err)
	}
	query, err := store.Query(QueryOptions{IndexOptions: IndexOptions{GroupNames: []string{"Character · On demand"}}})
	if err != nil || len(query.Items) != 1 || query.Items[0].BriefDescription != "CAPTAIN_BRIEF" {
		t.Fatalf("automatic group query: %#v %v", query, err)
	}
	_, err = store.ApplyOperations("Assign to authored group", []Operation{{Op: "update", ID: item.ID, Item: ItemInput{IndexMemberships: []IndexMembership{{"names", IndexDetailInherit}}}}})
	if err != nil {
		t.Fatal(err)
	}
	preview, _ = store.PreviewIndexGuide(guide)
	if len(preview.AutomaticGroups) != 0 || strings.Contains(preview.Markdown, "CAPTAIN_BRIEF") {
		t.Fatal("assigned entries must leave automatic groups")
	}
	_, err = store.ApplyOperations("Restore automatic group", []Operation{{Op: "update", ID: item.ID, Item: ItemInput{IndexMemberships: []IndexMembership{}}}})
	if err != nil {
		t.Fatal(err)
	}
	preview, _ = store.PreviewIndexGuide(guide)
	if len(preview.AutomaticGroups) != 1 || !strings.Contains(preview.Markdown, "CAPTAIN_BRIEF") {
		t.Fatal("unlink must restore the automatic preset")
	}
	if _, err := store.Create(ItemInput{Name: "New crew", Type: "character", LoadMode: LoadModeAuto, BriefDescription: "NEW_CREW_BRIEF"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyOperations("Move classification", []Operation{{Op: "update", ID: item.ID, Item: ItemInput{Type: "location", LoadMode: LoadModeManual}}}); err != nil {
		t.Fatal(err)
	}
	preview, _ = store.PreviewIndexGuide(guide)
	if len(preview.AutomaticGroups) != 2 || !strings.Contains(preview.Markdown, "NEW_CREW_BRIEF") || strings.Contains(preview.Markdown, "CAPTAIN_BRIEF") || !strings.Contains(preview.Markdown, "Location · Manual") {
		t.Fatalf("automatic groups did not follow new entries or item changes: %#v", preview)
	}
}

func TestLoreIndexMembershipLifecycleAndConflict(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	guide := IndexGuide{Groups: []IndexGroup{{ID: "harbor", Name: "Harbor", DefaultDetail: IndexDetailBrief}}}
	saveTestIndex(t, store, guide)
	item, err := store.Create(ItemInput{Name: "Captain", Content: "Unique tide keyword", IndexMemberships: []IndexMembership{{"harbor", IndexDetailFull}}})
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := store.IndexGuide()
	if _, err := store.ApplyOperations("Update text", []Operation{{Op: "update", ID: item.ID, Item: ItemInput{Content: "Updated tide keyword"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateIndexGuide(IndexGuideUpdate{Guide: guide, BaseRevision: stale.Revision}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update: %v", err)
	}
	item, _ = store.Get(item.ID)
	if len(item.IndexMemberships) != 1 {
		t.Fatal("omitted membership update cleared associations")
	}
	guide.Groups[0].Name = "Port"
	saveTestIndex(t, store, guide)
	page, err := store.Query(QueryOptions{IndexOptions: IndexOptions{GroupNames: []string{"port"}, Keywords: []string{"tide"}}})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != item.ID {
		t.Fatalf("renamed group lost identity: %v %v", page, err)
	}
	if _, err := store.Query(QueryOptions{IndexOptions: IndexOptions{GroupNames: []string{"Harbor"}}}); err == nil {
		t.Fatal("unknown group must not return the whole library")
	}
	saveTestIndex(t, store, IndexGuide{})
	item, err = store.Get(item.ID)
	if err != nil || len(item.IndexMemberships) != 0 || item.Content != "Updated tide keyword" {
		t.Fatalf("section removal damaged item: %#v %v", item, err)
	}
	backups, _ := filepath.Glob(filepath.Join(workspace, "setting/lore/backups/categories-index-removal-*.json"))
	if len(backups) != 1 {
		t.Fatalf("expected recovery snapshot: %v", backups)
	}
	index, _ := store.ProgressiveContextMarkdown()
	if !strings.Contains(index, "Captain") {
		t.Fatal("unlinked item missing from automatic directory")
	}
	if err := store.Delete(item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(workspace).ProgressiveContextMarkdown(); err != nil {
		t.Fatal(err)
	}
}

func TestLoreIndexContextOverflowDoesNotLoseStoredContent(t *testing.T) {
	store := NewStore(t.TempDir())
	guide := IndexGuide{Groups: []IndexGroup{{ID: "large", Name: "Large", DefaultDetail: IndexDetailFull}}}
	saveTestIndex(t, store, guide)
	item, err := store.Create(ItemInput{Name: "Large entry", BriefDescription: "Large body", Content: strings.Repeat("x", IndexContextMaxBytes), IndexMemberships: []IndexMembership{{"large", IndexDetailInherit}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ProgressiveContextMarkdown(); err == nil {
		t.Fatal("oversized always-loaded context must fail explicitly")
	}
	preview, err := store.PreviewIndexGuide(guide)
	if err != nil || !preview.OverBudget || preview.Markdown != "" {
		t.Fatalf("oversized preview must remain editable without partial Markdown: %#v %v", preview, err)
	}
	guide.Groups[0].DefaultDetail = IndexDetailName
	saveTestIndex(t, store, guide)
	if _, err := store.ProgressiveContextMarkdown(); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(item.ID)
	if err != nil || stored.Content != item.Content {
		t.Fatal("overflow handling modified stored content")
	}
}
