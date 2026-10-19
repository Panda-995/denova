package resourceexchange

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/book/lore"
)

func TestLoreUpdateIncludesMaterialBytesAndPreservesUnchangedIdentity(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	p, projectID, _ := gameDefaultsFixture(t, s)
	request := PlanRequest{PreviewID: p.ID, CandidateID: p.Candidates[0].ID, ProjectID: projectID, Resources: []string{"lore"}}
	plan, err := s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := s.Apply(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, layout, _ := s.registry.Resolve(projectID, true)
	store := lore.NewStore(layout.ContentRoot)
	id := installed.Bindings[0].Members["scene"].ID
	before, err := store.ReadAny(id)
	if err != nil {
		t.Fatal(err)
	}
	request.InstallationID = installed.ID
	plan, err = s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Updates) != 1 || plan.Updates[0].State != "unchanged" {
		t.Fatalf("unchanged materials differed: %+v", plan.Updates)
	}
	if _, err := s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := store.ReadAny(id)
	if before.ResolvedMaterials[0].ID != after.ResolvedMaterials[0].ID {
		t.Fatal("unchanged update replaced asset identity")
	}
	_, previewDir, err := s.loadPreview(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	files, err := readFiles(filepath.Join(previewDir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	var changed bytes.Buffer
	if err := png.Encode(&changed, picture); err != nil {
		t.Fatal(err)
	}
	files["assets/background.png"] = changed.Bytes()
	next, err := s.previewFiles(ctx, p.Source, files)
	if err != nil {
		t.Fatal(err)
	}
	request.PreviewID, request.CandidateID = next.ID, next.Candidates[0].ID
	plan, err = s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Updates[0].State != "update" {
		t.Fatalf("material-only upstream update missed: %+v", plan.Updates)
	}
	// Editing local media must conflict even when the Lore JSON is unchanged.
	if err := os.WriteFile(filepath.Join(layout.ContentRoot, before.ResolvedMaterials[0].Path), []byte("local material"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err = s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Updates[0].State != "conflict" {
		t.Fatalf("local media edit lost: %+v", plan.Updates)
	}
	request.Resolutions = map[string]map[string]string{"lore": {"scene": "remote"}}
	plan, err = s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	states, err := s.Installations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if states[0].LocalState != "unchanged" {
		t.Fatal("replaced media retained an obsolete local baseline", states[0].LocalState)
	}
	after, _ = store.ReadAny(id)
	content, err := os.ReadFile(filepath.Join(layout.ContentRoot, after.ResolvedMaterials[0].Path))
	if err != nil || !bytes.Equal(content, changed.Bytes()) {
		t.Fatal("replacement lost material bytes", err)
	}
}
