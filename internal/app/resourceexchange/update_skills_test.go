package resourceexchange

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"denova/internal/agents/skills"
	"denova/internal/revisionfile"
)

func TestUpdateDependenciesAndSkillReplacementGuards(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	makePreview := func(text string) Preview {
		t.Helper()
		manifest := Manifest{Format: "denova.resource-pack", SchemaVersion: 1, Package: PackageInfo{ID: "review", Name: "Review"}, Resources: []Resource{
			{ID: "base", Kind: "skill", Path: "base"}, {ID: "dependent", Kind: "skill", Path: "dependent", Requires: []string{"base"}}, {ID: "independent", Kind: "skill", Path: "independent"},
		}}
		files := map[string][]byte{"denova-pack.json": jsonBytes(t, manifest)}
		for _, name := range []string{"base", "dependent", "independent"} {
			files[name+"/SKILL.md"] = []byte(skills.DefaultContent(name, text))
		}
		preview, err := s.previewFiles(ctx, Source{Kind: "github", URL: "https://github.com/author/review", Ref: "main"}, files)
		if err != nil {
			t.Fatal(err)
		}
		return preview
	}
	p := makePreview("Before")
	request := PlanRequest{PreviewID: p.ID, CandidateID: p.Candidates[0].ID, Resources: []string{"dependent", "independent"}, UpdateMode: "auto_apply"}
	plan, err := s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := s.Apply(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	baseFile := filepath.Join(s.root, "skills", "base", "SKILL.md")
	if err := os.WriteFile(baseFile, []byte(skills.DefaultContent("base", "Mine")), 0600); err != nil {
		t.Fatal(err)
	}
	p = makePreview("After")
	request.PreviewID, request.CandidateID, request.InstallationID = p.ID, p.Candidates[0].ID, installed.ID
	plan, err = s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for resource, state := range map[string]string{"base": "conflict", "dependent": "blocked", "independent": "update"} {
		if !slices.ContainsFunc(plan.Updates, func(item UpdateItem) bool { return item.ResourceID == resource && item.State == state }) {
			t.Fatalf("missing %s=%s: %+v", resource, state, plan.Updates)
		}
	}
	// Automatic updates use the same partial-update rules and never overwrite conflicts.
	item, snapshot, err := s.loadInstallation(ctx, installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	item.CheckedAt, item.RemoteState, item.PendingPreviewID = time.Now(), "update_available", p.ID
	record, _ := resolveTarget(s.root, s.registry, installationTarget(item.ID))
	if _, err := revisionfile.ReplaceIfRevision(ctx, record, snapshot.Revision, jsonBytes(t, item), revisionfile.Options{}); err != nil {
		t.Fatal(err)
	}
	s.UpdateDue(ctx, time.Now(), s.Apply)
	for name, content := range map[string]string{"base": "Mine", "dependent": "Before", "independent": "After"} {
		raw, err := os.ReadFile(filepath.Join(s.root, "skills", name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != skills.DefaultContent(name, content) {
			t.Fatalf("%s lost its content: %s", name, raw)
		}
	}
	// Review a fresh immutable preview after the automatic plan released its bytes.
	p = makePreview("After")
	request.PreviewID, request.CandidateID = p.ID, p.Candidates[0].ID
	request.Resolutions = map[string]map[string]string{"base": {"": "remote"}}
	plan, err = s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(s.root, "skills", "base", "my-script.txt")
	if err := os.WriteFile(extra, []byte("new local work"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, plan.ID); !errors.Is(err, ErrLocalModified) {
		t.Fatalf("missed file added after review: %v", err)
	}
	plan, err = s.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Fatal("explicit Skill replacement mixed local and upstream files")
	}
	restore, err := s.PlanRestore(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, restore.ID); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(extra); err != nil || string(raw) != "new local work" {
		t.Fatal("backup lost replaced local file", err)
	}
}
