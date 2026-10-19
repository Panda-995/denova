package resourceexchange

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/internal/agents/skills"
	"denova/internal/project"
)

func TestDefaultImportReusesSharedResourcesAcrossBooks(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	manifest := Manifest{Format: "denova.resource-pack", SchemaVersion: 1, Package: PackageInfo{ID: "shared", Name: "Shared"}, Resources: []Resource{{ID: "skill", Kind: "skill", Path: "skill"}, {ID: "image", Kind: "preset.image", Path: "image.json"}, {ID: "lore", Kind: "lore.collection", Path: "lore.json"}}}
	preview, err := s.previewFiles(ctx, Source{Kind: "file", Filename: "shared.zip"}, map[string][]byte{"denova-pack.json": jsonBytes(t, manifest), "skill/SKILL.md": []byte(skills.DefaultContent("shared", "Original")), "image.json": []byte(`{"name":"Shared image","prompt":"Draw"}`), "lore.json": []byte(`{"version":1,"items":[{"id":"hero","name":"Hero","type":"character","content":"A hero"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	var copies []Installation
	for _, name := range []string{"first", "second"} {
		workspace := filepath.Join(s.root, "projects", name)
		if err := os.MkdirAll(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		book, err := s.registry.Add(workspace, project.TypeBook, name)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := s.Plan(ctx, PlanRequest{PreviewID: preview.ID, CandidateID: preview.Candidates[0].ID, Resources: []string{"skill", "image", "lore"}, ProjectID: book.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(copies) > 0 {
			for _, item := range plan.Items {
				if item.Local.Scope != "project" && item.Action != "reference" {
					t.Fatalf("shared resource copied: %+v", item)
				}
			}
			for _, change := range plan.Changes {
				if change.Target.ProjectID == "" && !strings.HasPrefix(change.Target.Path, "resource-exchange/") {
					t.Fatalf("shared content staged: %+v", change.Target)
				}
			}
		}
		installed, err := s.Apply(ctx, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, installed)
		if name == "first" {
			if err := os.WriteFile(filepath.Join(s.root, "skills", "shared", "SKILL.md"), []byte(skills.DefaultContent("shared", "Local customization")), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 2; i++ {
		if copies[0].Bindings[i].Local != copies[1].Bindings[i].Local || copies[1].Bindings[i].Ownership != "reference" {
			t.Fatalf("shared binding: %+v", copies)
		}
	}
	if copies[0].Bindings[2].Local == copies[1].Bindings[2].Local {
		t.Fatal("project collection shared between books")
	}
	plan, err := s.Plan(ctx, PlanRequest{PreviewID: preview.ID, CandidateID: preview.Candidates[0].ID, InstallationID: copies[1].ID, Resources: []string{"skill", "image", "lore"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.root, "skills", "shared", "SKILL.md"))
	if err != nil || !strings.Contains(string(raw), "Local customization") {
		t.Fatalf("shared customization replaced: %s %v", raw, err)
	}
}

func TestRepeatedPackImportsKeepIndependentSkillsAndUpdates(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	preview := updateFixture(t, s, "First", true)
	var installed []Installation
	for i, name := range []string{"automatic", "automatic-2", "automatic-3"} {
		workspace := filepath.Join(s.root, "projects", name)
		if err := os.MkdirAll(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		book, err := s.registry.Add(workspace, project.TypeBook, name)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := s.Plan(ctx, PlanRequest{PreviewID: preview.ID, CandidateID: preview.Candidates[0].ID, ProjectID: book.ID, Resources: []string{"skill", "image"}, SharedResources: "copy"})
		if err != nil {
			t.Fatalf("import %d: %v", i+1, err)
		}
		item, err := s.Apply(ctx, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		if item.Bindings[0].Local.ID != name {
			t.Fatalf("import %d: %+v", i+1, item.Bindings)
		}
		if i > 0 && (item.ID == installed[0].ID || item.Bindings[1].Local.ID == installed[0].Bindings[1].Local.ID) {
			t.Fatal("copies share an identity")
		}
		installed = append(installed, item)
	}
	next := updateFixture(t, s, "Second", true)
	plan, err := s.Plan(ctx, PlanRequest{PreviewID: next.ID, CandidateID: next.Candidates[0].ID, InstallationID: installed[1].ID, Resources: []string{"skill", "image"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	for i, item := range installed {
		name := item.Bindings[0].Local.ID
		raw, err := os.ReadFile(filepath.Join(s.root, "skills", name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		want := "First"
		if i == 1 {
			want = "Second"
		}
		if !strings.Contains(string(raw), want) || !strings.Contains(string(raw), "name: "+name) {
			t.Fatalf("copy %d changed incorrectly: %s", i, raw)
		}
	}
}

func TestDuplicateSkillNamesAllocatePortableCopies(t *testing.T) {
	for _, name := range []string{"fixture", strings.Repeat("a", 64), strings.Repeat("界", 64)} {
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			preview, candidate := previewFixture(t, s)
			for _, existing := range []string{strings.ToUpper(name), name + "-2"} {
				if err := os.MkdirAll(filepath.Join(s.root, "skills", existing), 0700); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := s.Plan(context.Background(), PlanRequest{PreviewID: preview.ID, CandidateID: candidate.ID, Resources: []string{"skill"}, Names: map[string]string{"skill": name}, SharedResources: "copy"})
			if err != nil {
				t.Fatal(err)
			}
			copyName := plan.Items[1].Local.ID
			if strings.EqualFold(copyName, name) || strings.EqualFold(copyName, name+"-2") {
				t.Fatalf("existing name reused: %s", copyName)
			}
			if err := skills.ValidateName(copyName); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Apply(context.Background(), plan.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}
