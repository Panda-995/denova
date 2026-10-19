package resourceexchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/book/lore"
	"denova/internal/project"
	"denova/internal/revisionfile"
)

func gameDefaultsFixture(t *testing.T, service *Service) (Preview, string, string) {
	t.Helper()
	workspace := filepath.Join(service.root, "projects", "defaults")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	record, err := service.registry.Add(workspace, project.TypeBook, "Defaults")
	if err != nil {
		t.Fatal(err)
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Format: "denova.resource-pack", SchemaVersion: 1, Package: PackageInfo{ID: "defaults", Name: "Defaults"},
		Resources:    []Resource{{ID: "lore", Kind: "lore.collection", Path: "lore.json", Assets: []string{"assets/background.png"}}, {ID: "style", Kind: "preset.narrative", Path: "style.json"}},
		GameDefaults: &PackageGameDefaults{NarrativeStyleID: "style", DefaultBackground: &PackageDefaultBackground{ResourceID: "lore", ItemID: "scene", AssetPath: "assets/background.png"}},
	}
	preview, err := service.previewFiles(context.Background(), Source{Kind: "file", Filename: "defaults.zip"}, map[string][]byte{
		"denova-pack.json": jsonBytes(t, manifest), "assets/background.png": picture.Bytes(),
		"style.json": []byte(`{"name":"Package narrative","description":"Fixture","slots":[{"id":"system","name":"Narrative","target":"system","enabled":true,"content":"Tell the story."}]}`),
		"lore.json":  []byte(`{"version":1,"items":[{"id":"scene","enabled":true,"type":"world","name":"Moonlit Scene","content":"A setting","materials":{"entries":[{"asset_path":"assets/background.png","name":"Background"}]}}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, layout, err := service.registry.Resolve(record.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	return preview, record.ID, layout.ConfigPath()
}

func TestGameDefaultsImportAdoptionAndUpdateIsolation(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	preview, projectID, configPath := gameDefaultsFixture(t, s)
	before := []byte("theme = 'dark'\n[game_creation_defaults]\nnarrative_style_id = ''\n")
	if err := os.WriteFile(configPath, before, 0600); err != nil {
		t.Fatal(err)
	}
	request := PlanRequest{PreviewID: preview.ID, CandidateID: preview.Candidates[0].ID, Resources: []string{"lore", "style"}, ProjectID: projectID}
	plan, err := s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := s.Apply(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(configPath); !bytes.Equal(raw, before) {
		t.Fatal("ordinary import changed project settings")
	}
	bg := installed.GameDefaults.DefaultBackground
	if bg == nil || bg.ItemID != "moonlit_scene" || bg.AssetID == "" || *installed.GameDefaults.NarrativeStyleID == "style" {
		t.Fatalf("source references were not mapped: %+v", installed.GameDefaults)
	}
	_, layout, _ := s.registry.Resolve(projectID, true)
	items, err := lore.NewStore(layout.ContentRoot).ListAll()
	if err != nil || items[0].ID != bg.ItemID || items[0].ResolvedMaterials[0].ID != bg.AssetID {
		t.Fatalf("background mapping does not point at imported association: %+v %v", items, err)
	}
	request.InstallationID = installed.ID
	request.GameDefaultsFields = []string{"default_background"}
	plan, err = s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.GameDefaultsBefore.NarrativeStyleID == nil || plan.GameDefaultsApplied.NarrativeStyleID != nil {
		t.Fatal("adoption did not preserve the explicit narrative choice")
	}
	installed, err = s.Apply(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := config.ReadSettingsFile(configPath)
	if err != nil || saved.Theme != "dark" || *saved.GameCreationDefaults.NarrativeStyleID != "" || !reflect.DeepEqual(saved.GameCreationDefaults.DefaultBackground, installed.GameDefaults.DefaultBackground) {
		t.Fatalf("adoption corrupted settings: %+v %v", saved, err)
	}
	adopted, _ := os.ReadFile(configPath)
	request.GameDefaultsFields = nil
	update, err := s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, update.ID); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(configPath); !bytes.Equal(raw, adopted) {
		t.Fatal("package update reapplied its recommendations")
	}
	request.Resources = []string{"style"}
	partial, err := s.Plan(ctx, request)
	if err != nil || partial.Installation.GameDefaults.DefaultBackground == nil {
		t.Fatalf("partial update lost retained background recommendation: %v", err)
	}
	request.GameDefaultsFields = []string{"default_background"}
	if _, err := s.Plan(ctx, request); err == nil {
		t.Fatal("adopted a resource omitted from this update")
	}
	request.Resources = []string{"lore", "style"}
	request.ProjectID = "another-project"
	if _, err := s.Plan(ctx, request); err == nil || !strings.Contains(err.Error(), "installation Project") {
		t.Fatalf("accepted defaults from another Project: %v", err)
	}
	refs := []LocalRef{}
	for _, binding := range installed.Bindings {
		refs = append(refs, binding.Local)
	}
	raw, err := s.Export(ctx, ExportRequest{InstallationID: installed.ID, Package: installed.Package, Resources: refs})
	if err != nil {
		t.Fatal(err)
	}
	next := testService(t)
	roundtrip, err := next.Preview(ctx, Source{Kind: "file", Filename: "roundtrip.zip"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	d := roundtrip.Candidates[0].GameDefaults
	if d == nil || d.NarrativeStyleID != "style" || d.DefaultBackground == nil || d.DefaultBackground.ItemID != "scene" {
		t.Fatalf("export lost recommendation identity: %+v", d)
	}
}

func TestGameDefaultsAdoptionChecksSelectionAndConcurrentEdits(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	preview, projectID, configPath := gameDefaultsFixture(t, s)
	request := PlanRequest{PreviewID: preview.ID, CandidateID: preview.Candidates[0].ID, Resources: []string{"style"}, ProjectID: projectID, GameDefaultsFields: []string{"default_background"}}
	if _, err := s.Plan(ctx, request); err == nil {
		t.Fatal("adopted an unselected background resource")
	}
	request.GameDefaultsFields = []string{"narrative_style_id"}
	plan, err := s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[game_creation_defaults]\nnarrative_style_id = 'my-choice'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.Apply(ctx, plan.ID)
	var conflict *revisionfile.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("concurrent book edit not protected: %v", err)
	}
	request.automatic = true
	if _, err := s.Plan(ctx, request); err == nil {
		t.Fatal("automatic update adopted defaults")
	}
}

func TestGameDefaultsPreviewRejectsInvalidReferences(t *testing.T) {
	s := testService(t)
	preview, _, _ := gameDefaultsFixture(t, s)
	dir, _ := s.previewPath(preview.ID)
	candidate := preview.Candidates[0]
	for _, mutate := range []func(*PackageGameDefaults){
		func(d *PackageGameDefaults) { d.NarrativeStyleID = "lore" },
		func(d *PackageGameDefaults) { d.DefaultBackground.ItemID = "missing" },
		func(d *PackageGameDefaults) { d.DefaultBackground.AssetPath = "../outside.png" },
	} {
		raw := jsonBytes(t, preview.Candidates[0].GameDefaults)
		var d PackageGameDefaults
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		mutate(&d)
		candidate.GameDefaults = &d
		if err := validatePackageGameDefaults(dir, candidate); err == nil {
			t.Fatalf("invalid recommendation accepted: %+v", d)
		}
	}
}

func TestGameDefaultsAdoptionReviewsBackgroundConflicts(t *testing.T) {
	for _, resolution := range []string{"remote", "keep"} {
		t.Run(resolution, func(t *testing.T) {
			ctx := context.Background()
			s := testService(t)
			preview, projectID, configPath := gameDefaultsFixture(t, s)
			if err := os.WriteFile(configPath, []byte("theme = 'dark'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			dir, err := s.previewPath(preview.ID)
			if err != nil {
				t.Fatal(err)
			}
			files, err := readFiles(filepath.Join(dir, "files"))
			if err != nil {
				t.Fatal(err)
			}
			// An older package can have the same Lore material without a default.
			var manifest Manifest
			if err := json.Unmarshal(files["denova-pack.json"], &manifest); err != nil {
				t.Fatal(err)
			}
			defaults := manifest.GameDefaults
			manifest.GameDefaults = nil
			files["denova-pack.json"] = jsonBytes(t, manifest)
			baseline, err := s.previewFiles(ctx, preview.Source, files)
			if err != nil {
				t.Fatal(err)
			}
			request := PlanRequest{PreviewID: baseline.ID, CandidateID: baseline.Candidates[0].ID, Resources: []string{"lore", "style"}, ProjectID: projectID}
			plan, err := s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			installed, err := s.Apply(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, layout, err := s.registry.Resolve(projectID, true)
			if err != nil {
				t.Fatal(err)
			}
			store := lore.NewStore(layout.ContentRoot)
			if _, err := store.Update("moonlit_scene", lore.ItemInput{Name: "Moonlit Scene", Content: "My local setting"}); err != nil {
				t.Fatal(err)
			}
			beforeConfig, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeLore, err := os.ReadFile(filepath.Join(layout.ContentRoot, lore.ItemsRelativePath))
			if err != nil {
				t.Fatal(err)
			}
			manifest.GameDefaults = defaults
			files["denova-pack.json"] = jsonBytes(t, manifest)
			files["lore.json"] = bytes.ReplaceAll(files["lore.json"], []byte("A setting"), []byte("An upstream setting"))
			updated, err := s.previewFiles(ctx, preview.Source, files)
			if err != nil {
				t.Fatal(err)
			}
			request.PreviewID, request.InstallationID = updated.ID, installed.ID
			request.GameDefaultsFields = []string{"default_background", "narrative_style_id"}
			plan, err = s.Plan(ctx, request)
			if err != nil {
				t.Fatalf("background adoption prevented conflict review: %v", err)
			}
			if !reflect.DeepEqual(plan.GameDefaultsUnavailable, []string{"default_background"}) || plan.GameDefaultsApplied != nil {
				t.Fatalf("unavailable defaults were not deferred: %+v", plan)
			}
			if len(plan.Updates) == 0 || !plan.Updates[0].Conflict {
				t.Fatalf("background conflict is missing: %+v", plan.Updates)
			}
			if _, err := s.Apply(ctx, plan.ID); err == nil {
				t.Fatal("applied a plan that could not honor selected defaults")
			}
			if raw, _ := os.ReadFile(configPath); !bytes.Equal(raw, beforeConfig) {
				t.Fatal("conflict review changed book settings")
			}
			if raw, _ := os.ReadFile(filepath.Join(layout.ContentRoot, lore.ItemsRelativePath)); !bytes.Equal(raw, beforeLore) {
				t.Fatal("conflict review changed local Lore")
			}
			request.Resolutions = map[string]map[string]string{"lore": {"scene": resolution}}
			plan, err = s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if resolution == "keep" {
				if !reflect.DeepEqual(plan.GameDefaultsUnavailable, []string{"default_background"}) {
					t.Fatal("retained local content supplied an unmapped background")
				}
				request.GameDefaultsFields = nil
				plan, err = s.Plan(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
			} else if len(plan.GameDefaultsUnavailable) > 0 || plan.GameDefaultsApplied.DefaultBackground == nil || plan.GameDefaultsApplied.NarrativeStyleID == nil {
				t.Fatal("resolved conflict did not restore all selected defaults")
			}
			if _, err := s.Apply(ctx, plan.ID); err != nil {
				t.Fatal(err)
			}
			item, err := store.ReadAny("moonlit_scene")
			if err != nil {
				t.Fatal(err)
			}
			if resolution == "keep" {
				if item.Content != "My local setting" {
					t.Fatal("keeping local content overwrote the user's edit")
				}
				if raw, _ := os.ReadFile(configPath); !bytes.Equal(raw, beforeConfig) {
					t.Fatal("deselecting defaults changed book settings")
				}
			} else {
				saved, err := config.ReadSettingsFile(configPath)
				if err != nil || !reflect.DeepEqual(saved.GameCreationDefaults, plan.GameDefaultsApplied) || item.Content != "An upstream setting" || item.ResolvedMaterials[0].ID != saved.GameCreationDefaults.DefaultBackground.AssetID {
					t.Fatalf("adopted defaults do not match imported content: %+v %v", saved.GameCreationDefaults, err)
				}
			}
		})
	}
}
