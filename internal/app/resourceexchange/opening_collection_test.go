package resourceexchange

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/platform"
	"denova/internal/project"
)

func TestOpeningCollectionLifecycle(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	dir := filepath.Join(s.root, "projects", "openings")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	record, err := s.registry.Add(dir, project.TypeBook, "Openings")
	if err != nil {
		t.Fatal(err)
	}
	write := func(value openings) {
		t.Helper()
		if err := writeFiles(dir, map[string][]byte{openingPath: jsonBytes(t, value)}); err != nil {
			t.Fatal(err)
		}
	}
	read := func() openings {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dir, openingPath))
		if err != nil {
			t.Fatal(err)
		}
		var value openings
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	write(openings{Version: 1, Presets: []opening{{ID: "personal", Title: "Personal", Content: "My opening"}}})
	items := make([]opening, 300)
	for i := range items {
		items[i] = opening{ID: fmt.Sprintf("start-%d", i), Title: fmt.Sprintf("Start %d", i), Content: "An independent beginning."}
	}
	manifest := Manifest{Format: "denova.resource-pack", SchemaVersion: 1, Package: PackageInfo{ID: "openings", Name: "Openings"}, Resources: []Resource{{ID: "openings", Kind: "game.openings", Path: "openings.json"}}}
	preview := func() Preview {
		t.Helper()
		p, err := s.previewFiles(ctx, Source{Kind: "file", Filename: "openings.zip"}, map[string][]byte{"denova-pack.json": jsonBytes(t, manifest), "openings.json": jsonBytes(t, map[string]any{"version": 1, "items": items})})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := preview()
	if len(p.Candidates[0].Resources) != 1 || p.Candidates[0].Resources[0].ItemCount != 300 {
		t.Fatalf("openings split into resources: %+v", p)
	}
	details, err := s.PreviewFiles(ctx, p.ID, p.Candidates[0].ID, "openings", "openings.json", "start-299")
	if err != nil || len(details.Items) != 300 || details.Items[299].Name != "Start 299" || details.Truncated {
		t.Fatal("preview", err, details)
	}
	var selected opening
	if err := json.Unmarshal([]byte(details.Content), &selected); err != nil || selected.ID != "start-299" {
		t.Fatal("selected preview", err)
	}
	request := PlanRequest{PreviewID: p.ID, CandidateID: p.Candidates[0].ID, Resources: []string{"openings"}, ProjectID: record.ID}
	plan, err := s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := s.Apply(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed.Bindings) != 1 || len(read().Presets) != 301 {
		t.Fatal("lost collection members")
	}
	local := installed.Bindings[0].Local
	candidates, err := s.ExportResources(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, c := range candidates {
		if c.Local.Kind == "game.openings" {
			found++
			if c.Local.ID == "all" && c.ItemCount != 301 {
				t.Fatal("project collection count", c)
			}
		}
	}
	if found != 2 {
		t.Fatal("expected project and installed collection choices", found)
	}
	exported, err := s.Export(ctx, ExportRequest{Package: installed.Package, InstallationID: installed.ID, Resources: []LocalRef{local}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := platform.ArchiveFiles(exported)
	if err != nil || len(files) != 2 {
		t.Fatal("expected manifest and one collection", len(files), err)
	}
	var exportedManifest Manifest
	if err := json.Unmarshal(files["denova-pack.json"], &exportedManifest); err != nil {
		t.Fatal(err)
	}
	var collection struct {
		Items []opening `json:"items"`
	}
	if err := json.Unmarshal(files[exportedManifest.Resources[0].Path], &collection); err != nil || len(collection.Items) != 300 || collection.Items[0] != items[0] {
		t.Fatal("roundtrip lost source identities", err)
	}
	if _, err := s.Export(ctx, ExportRequest{Package: installed.Package, Resources: []LocalRef{local, {Kind: "game.openings", Scope: "project", ProjectID: record.ID, ID: "all"}}}); err == nil {
		t.Fatal("accepted overlapping collection scopes")
	}
	current := read()
	firstID := current.Presets[1].ID
	current.Presets[0].Content = "Unrelated local edit"
	write(current)
	states, err := s.Installations(ctx)
	if err != nil || states[0].LocalState != "unchanged" {
		t.Fatal("unrelated edit marks collection modified", err, states)
	}
	items = items[:299]
	items[0].Content = "Updated upstream"
	p = preview()
	request.PreviewID = p.ID
	request.InstallationID = installed.ID
	updated, err := s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, updated.ID); err != nil {
		t.Fatal(err)
	}
	current = read()
	if len(current.Presets) != 301 || current.Presets[1].ID != firstID || current.Presets[1].Content != "Updated upstream" || current.Presets[0].Content != "Unrelated local edit" {
		t.Fatal("update changed identities or removed local entries")
	}
	current.Presets[1].Content = "My revision"
	write(current)
	states, err = s.Installations(ctx)
	if err != nil || states[0].LocalState != "modified" {
		t.Fatal("missed local edit", err, states)
	}
	items[0].Content = "New upstream revision"
	p = preview()
	request.PreviewID = p.ID
	protected, err := s.Plan(ctx, request)
	if err != nil || protected.Updates[0].State != "conflict" {
		t.Fatal("local edit not protected", err)
	}
	request.Resolutions = map[string]map[string]string{"openings": {"start-0": "remote"}}
	replaced, err := s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, replaced.ID); err != nil {
		t.Fatal(err)
	}
	restore, err := s.PlanRestore(ctx, replaced.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, restore.ID); err != nil {
		t.Fatal(err)
	}
	if read().Presets[1].Content != "My revision" {
		t.Fatal("rollback lost edit")
	}
}

func TestOpeningCollectionAdmissionAndCharacterConversion(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"items":[]}`,
		`{"version":2,"items":[{"id":"a","title":"A","content":"Text"}]}`,
		`{"version":1,"items":[{"id":"a","title":"A","content":"Text"},{"id":"a","title":"B","content":"Other"}]}`,
		`{"version":1,"items":[{"id":" a ","title":"A","content":"Text"}]}`,
		`{"version":1,"items":[{"id":"a","title":" ","content":"Text"}]}`,
		`{"version":1,"items":[{"id":"a","title":"A","content":" "}]}`,
		`{"version":1,"items":[{"id":"a","title":"A","content":"Text","workspace":"/host"}]}`,
	} {
		if err := validatePayload("game.openings", []byte(raw)); err == nil {
			t.Fatalf("accepted invalid collection %s", raw)
		}
	}
	// Admission protects the snapshot used for merging, even if a separate live
	// read would see an older version. Collection IDs are JSON identities only.
	old := opening{ID: "local", Title: "入门", Content: "Before"}
	binding := Binding{Members: map[string]CollectionMember{"开场-甲": {ID: old.ID, Digest: openingDigest(old)}}}
	incoming := []byte(`{"version":1,"items":[{"id":"开场-甲","title":"入门","content":"Upstream"}]}`)
	old.Content = "User edit"
	current := jsonBytes(t, openings{Version: 1, Presets: []opening{old}})
	review := &updateReview{}
	if _, err := stageOpeningCollection(&binding, incoming, current, review); err != nil || review.items[0].State != "conflict" {
		t.Fatal("snapshot edit not protected", err)
	}
	s := testService(t)
	card := []byte(`{"spec":"chara_card_v2","data":{"name":"Guide","description":"A guide","first_mes":"First beginning","alternate_greetings":["Second beginning","Third beginning"]}}`)
	p, err := s.Preview(context.Background(), Source{Kind: "file", Filename: "guide.json"}, card)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, r := range p.Candidates[0].Resources {
		if r.Kind == "game.openings" {
			found++
			if r.ItemCount != 3 {
				t.Fatal("character openings lost", r)
			}
			files, err := s.PreviewFiles(context.Background(), p.ID, p.Candidates[0].ID, r.ID, "", "")
			if err != nil || len(files.Files) != 1 || len(files.Items) != 3 {
				t.Fatal("character openings split", err, files)
			}
		}
	}
	if found != 1 {
		t.Fatal("expected one character opening collection", found)
	}
}
