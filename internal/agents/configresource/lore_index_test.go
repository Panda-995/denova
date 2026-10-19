package configresource

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/book/lore"
)

// Exercise the model-visible tools against the real store, including edits
// from another caller. Pure adapter/schema assertions cannot catch lost canon.
func TestLoreIndexConfigurationRoundTrip(t *testing.T) {
	cfg := &config.Config{NovaDir: t.TempDir(), Workspace: t.TempDir()}
	store := lore.NewStore(cfg.Workspace)
	read := configManagerToolByName(t, cfg, "config_read")
	apply := configManagerToolByName(t, cfg, "config_apply")
	call := func(name string, input map[string]any) string {
		t.Helper()
		tool := read
		if name == "apply" {
			tool = apply
		}
		output, err := runToolForTest(context.Background(), tool, mustJSON(t, input))
		if err != nil {
			t.Fatal(err)
		}
		return output
	}
	get := func(resource, id string) string {
		t.Helper()
		return call("read", map[string]any{"operation": ReadGet, "resource": resource, "ids": []string{id}, "scope": "workspace"})
	}
	guide := decodeConfigGetItem[loreIndexDocument](t, get("lore_index", "index"))
	guide.Guide = lore.IndexGuide{
		IntroMarkdown: "Read the harbor guide first.",
		Groups: []lore.IndexGroup{
			{ID: "harbor", Name: "Harbor", Purpose: "Port investigations", BodyMarkdown: "Keep the tide in mind.", DefaultDetail: "full"},
			{ID: "other", Name: "Other", DefaultDetail: "name"},
		},
		AutomaticDetails: map[string]string{"auto:character": "brief"},
	}
	guideInput := map[string]any{"operation": ApplyUpdate, "resource": "lore_index", "id": "index", "revision": guide.Revision, "value": guide.Guide}
	call("apply", guideInput)
	if _, err := runToolForTest(context.Background(), apply, mustJSON(t, guideInput)); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("stale guide should fail: %v", err)
	}
	if got := decodeConfigGetItem[loreIndexDocument](t, get("lore_index", "index")); !reflect.DeepEqual(got.Guide, guide.Guide) {
		t.Fatalf("guide round trip = %#v", got)
	}
	disabled := false
	for _, input := range []lore.ItemInput{
		{ID: "captain", Name: "Captain", Type: "character", Content: "CAPTAIN_BODY", BriefDescription: "A ferry pilot", Tags: []string{"port"}, Keywords: []string{"pilot"}, LoadMode: "auto", IndexMemberships: []lore.IndexMembership{{GroupID: "other", Detail: "name"}}},
		{ID: "archived", Name: "Archived", Enabled: &disabled, Content: "DISABLED_BODY", LoadMode: "resident"},
	} {
		if _, err := store.Create(input); err != nil {
			t.Fatal(err)
		}
	}
	before, err := store.Get("captain")
	if err != nil {
		t.Fatal(err)
	}
	catalog := call("read", map[string]any{"operation": ReadList, "resource": "lore_index_membership"})
	if !strings.Contains(catalog, `"id":"archived"`) || strings.Contains(catalog, "CAPTAIN_BODY") || strings.Contains(catalog, "DISABLED_BODY") {
		t.Fatalf("configuration catalog must include disabled metadata but no bodies: %s", catalog)
	}
	member := decodeConfigGetItem[loreIndexMembershipDocument](t, get("lore_index_membership", "captain"))
	memberships := append(member.IndexMemberships, lore.IndexMembership{GroupID: "harbor", Detail: "inherit"})
	memberInput := map[string]any{"operation": ApplyUpdate, "resource": "lore_index_membership", "id": "captain", "revision": member.Revision, "value": map[string]any{"index_memberships": memberships}}
	call("apply", memberInput)
	if _, err := runToolForTest(context.Background(), apply, mustJSON(t, memberInput)); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("stale membership should fail: %v", err)
	}
	after, err := store.Get("captain")
	if err != nil {
		t.Fatal(err)
	}
	want := before
	want.IndexMemberships, want.UpdatedAt = memberships, after.UpdatedAt
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("membership edit changed other item fields:\ngot %#v\nwant %#v", after, want)
	}
	// A failed subsequent item must neither roll back a successful association
	// nor change the disabled entry. A concurrent body edit also invalidates it.
	archived := decodeConfigGetItem[loreIndexMembershipDocument](t, get("lore_index_membership", "archived"))
	badInput := map[string]any{"operation": ApplyUpdate, "resource": "lore_index_membership", "id": "archived", "revision": archived.Revision, "value": map[string]any{"index_memberships": []lore.IndexMembership{{GroupID: "missing", Detail: "inherit"}}}}
	if _, err := runToolForTest(context.Background(), apply, mustJSON(t, badInput)); err == nil {
		t.Fatal("unknown group was accepted")
	}
	if _, err := store.ApplyOperations("Concurrent edit", []lore.Operation{{Op: "update", ID: "archived", Item: lore.ItemInput{Content: "NEW_DISABLED_BODY"}}}); err != nil {
		t.Fatal(err)
	}
	badInput["value"] = map[string]any{"index_memberships": memberships}
	if _, err := runToolForTest(context.Background(), apply, mustJSON(t, badInput)); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("concurrent item edit should conflict: %v", err)
	}
	archived = decodeConfigGetItem[loreIndexMembershipDocument](t, get("lore_index_membership", "archived"))
	badInput["revision"] = archived.Revision
	call("apply", badInput)
	preview, err := store.PreviewIndexGuide(guide.Guide)
	if err != nil || !strings.Contains(preview.Markdown, "CAPTAIN_BODY") || strings.Contains(preview.Markdown, "DISABLED_BODY") {
		t.Fatalf("actual context projection = %#v, %v", preview, err)
	}
	// Rename/reorder by stable ID, preserving associations and automatic detail.
	current := decodeConfigGetItem[loreIndexDocument](t, get("lore_index", "index"))
	current.Guide.Groups[0].Name = "Port cast"
	current.Guide.Groups[0], current.Guide.Groups[1] = current.Guide.Groups[1], current.Guide.Groups[0]
	guideInput["revision"], guideInput["value"] = current.Revision, current.Guide
	call("apply", guideInput)
	current = decodeConfigGetItem[loreIndexDocument](t, get("lore_index", "index"))
	current.Guide.Groups = current.Guide.Groups[:1]
	guideInput["revision"], guideInput["value"] = current.Revision, current.Guide
	preRemoval, err := os.ReadFile(lore.ItemsPath(cfg.Workspace))
	if err != nil {
		t.Fatal(err)
	}
	call("apply", guideInput)
	backups, err := filepath.Glob(filepath.Join(cfg.Workspace, "setting", "lore", "backups", "categories-index-removal-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("group removal backup = %v, %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || string(backup) != string(preRemoval) {
		t.Fatalf("backup did not preserve the full original collection: %v", err)
	}
	member = decodeConfigGetItem[loreIndexMembershipDocument](t, get("lore_index_membership", "captain"))
	if !reflect.DeepEqual(member.IndexMemberships, before.IndexMemberships) {
		t.Fatalf("group deletion removed unrelated associations: %#v", member)
	}
	memberInput["revision"] = member.Revision
	memberInput["value"] = map[string]any{"index_memberships": []lore.IndexMembership{}}
	call("apply", memberInput)
	current = decodeConfigGetItem[loreIndexDocument](t, get("lore_index", "index"))
	preview, err = store.PreviewIndexGuide(current.Guide)
	if err != nil || strings.Contains(preview.Markdown, "CAPTAIN_BODY") || !strings.Contains(preview.Markdown, "A ferry pilot") {
		t.Fatalf("clearing associations did not restore the automatic brief: %#v, %v", preview, err)
	}
}
