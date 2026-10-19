package resourceexchange

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/internal/agents/skills"
)

func TestFileUpdatePreservesRemoteTrackingAndLocalEdits(t *testing.T) {
	for _, source := range []Source{
		{Kind: "github", URL: "https://github.com/author/package", Ref: "main", Path: "examples/story", Commit: strings.Repeat("a", 40)},
		{Kind: "https_zip", URL: "https://example.com/story.zip"},
	} {
		t.Run(source.Kind, func(t *testing.T) {
			ctx := context.Background()
			s := testService(t)
			manifest := Manifest{Format: "denova.resource-pack", SchemaVersion: 1, Package: PackageInfo{ID: "story", Name: "Story"}, Resources: []Resource{{ID: "skill", Kind: "skill", Path: "skill"}}}
			files := map[string][]byte{"denova-pack.json": jsonBytes(t, manifest), "skill/SKILL.md": []byte(skills.DefaultContent("story", "First"))}
			first, err := s.previewFiles(ctx, source, files)
			if err != nil {
				t.Fatal(err)
			}
			request := PlanRequest{PreviewID: first.ID, CandidateID: first.Candidates[0].ID, Resources: []string{"skill"}, UpdateMode: "notify"}
			plan, err := s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			installed, err := s.Apply(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			local := filepath.Join(s.root, "skills", "story", "SKILL.md")
			mine := []byte(skills.DefaultContent("story", "My draft"))
			if err := os.WriteFile(local, mine, 0600); err != nil {
				t.Fatal(err)
			}
			files["skill/SKILL.md"] = []byte(skills.DefaultContent("story", "Second"))
			// Upload the entire repository ZIP, with its wrapper and package directory.
			archiveFiles := map[string][]byte{}
			for name, raw := range files {
				archiveFiles["package-main/examples/story/"+name] = raw
			}
			archive, err := archiveBytes(archiveFiles)
			if err != nil {
				t.Fatal(err)
			}
			next, err := s.Preview(ctx, Source{Kind: "file", Filename: "package-main.zip"}, archive)
			if err != nil {
				t.Fatal(err)
			}
			request.PreviewID, request.CandidateID, request.InstallationID = next.ID, next.Candidates[0].ID, installed.ID
			plan, err = s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Updates) != 1 || !plan.Updates[0].Conflict {
				t.Fatalf("local edit not reviewed: %+v", plan.Updates)
			}
			request.Resolutions = map[string]map[string]string{"skill": {"": "remote"}}
			plan, err = s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := s.Apply(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			expectedSource := source
			expectedSource.Commit = ""
			if updated.ID != installed.ID || updated.Source != expectedSource || updated.UpdateMode != "notify" || updated.Bindings[0].Local != installed.Bindings[0].Local {
				t.Fatalf("file update changed tracking or ownership: %+v", updated)
			}
			if raw, err := os.ReadFile(local); err != nil || string(raw) != string(files["skill/SKILL.md"]) {
				t.Fatalf("file update was not applied: %s %v", raw, err)
			}
			// A different remote source still requires an explicit change of ownership.
			otherSource := source
			otherSource.URL += "-other"
			other, err := s.previewFiles(ctx, otherSource, files)
			if err != nil {
				t.Fatal(err)
			}
			request.PreviewID, request.CandidateID = other.ID, other.Candidates[0].ID
			if _, err := s.Plan(ctx, request); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("accepted another remote source: %v", err)
			}
			restore, err := s.PlanRestore(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Apply(ctx, restore.ID); err != nil {
				t.Fatal(err)
			}
			if raw, err := os.ReadFile(local); err != nil || string(raw) != string(mine) {
				t.Fatalf("backup did not restore local draft: %s %v", raw, err)
			}
		})
	}
}
