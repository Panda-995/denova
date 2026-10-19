package resourceexchange

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"denova/internal/book/lore"
	"denova/internal/project"
)

func TestCollectionUpdatesPreserveAndResolveIndividualEdits(t *testing.T) {
	for _, kind := range []string{"lore.collection", "game.openings"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s := testService(t)
			dir := filepath.Join(s.root, "projects", "story")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			project, err := s.registry.Add(dir, project.TypeBook, "Story")
			if err != nil {
				t.Fatal(err)
			}
			remote := map[string]string{"a": "base", "b": "base", "c": "base", "d": "base", "e": "base", "f": "base"}
			preview := func() Preview {
				t.Helper()
				items := []map[string]any{}
				ids := []string{}
				for id := range remote {
					ids = append(ids, id)
				}
				slices.Sort(ids)
				for _, id := range ids {
					item := map[string]any{"id": id, "content": remote[id]}
					if kind == "lore.collection" {
						item["name"], item["type"], item["brief_description"] = id, "world", "Description"
					} else {
						item["title"] = id
					}
					items = append(items, item)
				}
				manifest := Manifest{Format: "denova.resource-pack", SchemaVersion: 1, Package: PackageInfo{ID: "review", Name: "Review"}, Resources: []Resource{{ID: "collection", Kind: kind, Path: "collection.json"}}}
				p, err := s.previewFiles(ctx, Source{Kind: "file", Filename: "review.zip"}, map[string][]byte{"denova-pack.json": jsonBytes(t, manifest), "collection.json": jsonBytes(t, map[string]any{"version": 1, "items": items})})
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
			p := preview()
			request := PlanRequest{PreviewID: p.ID, CandidateID: p.Candidates[0].ID, Resources: []string{"collection"}, ProjectID: project.ID}
			plan, err := s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			installed, err := s.Apply(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			binding := installed.Bindings[0]
			file := filepath.Join(dir, filepath.FromSlash(collectionPath(kind)))
			read := func() map[string]any {
				t.Helper()
				raw, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var body map[string]any
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Fatal(err)
				}
				return body
			}
			key := "items"
			if kind == "game.openings" {
				key = "presets"
			}
			body := read()
			items := []any{}
			for _, raw := range body[key].([]any) {
				item := raw.(map[string]any)
				id := item["id"].(string)
				if id == binding.Members["e"].ID {
					continue
				}
				if id == binding.Members["a"].ID || id == binding.Members["c"].ID {
					item["content"] = "mine"
				}
				if id == binding.Members["d"].ID {
					item["content"] = "converged"
				}
				items = append(items, item)
			}
			body[key] = items
			if err := os.WriteFile(file, jsonBytes(t, body), 0600); err != nil {
				t.Fatal(err)
			}
			remote["b"], remote["c"], remote["d"], remote["e"], remote["g"] = "next", "upstream", "converged", "restore", "new"
			delete(remote, "f")
			p = preview()
			request.PreviewID, request.CandidateID, request.InstallationID = p.ID, p.Candidates[0].ID, installed.ID
			plan, err = s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			states := map[string]string{}
			for _, item := range plan.Updates {
				states[item.MemberID] = item.State
			}
			for id, expected := range map[string]string{"a": "keep", "b": "update", "c": "conflict", "d": "unchanged", "e": "conflict", "f": "upstream_removed", "g": "create"} {
				if states[id] != expected {
					t.Fatalf("%s: got %s want %s", id, states[id], expected)
				}
			}
			installed, err = s.Apply(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertContent := func(id, content string, exists bool) {
				t.Helper()
				localID := installed.Bindings[0].Members[id].ID
				for _, raw := range read()[key].([]any) {
					item := raw.(map[string]any)
					if item["id"] == localID {
						if !exists || item["content"] != content {
							t.Fatalf("%s unexpectedly contains %+v", id, item)
						}
						return
					}
				}
				if exists {
					t.Fatalf("%s missing", id)
				}
			}
			assertContent("a", "mine", true)
			assertContent("b", "next", true)
			assertContent("c", "mine", true)
			assertContent("d", "converged", true)
			assertContent("e", "", false)
			assertContent("f", "base", true)
			assertContent("g", "new", true)
			if installed.RemoteState != "update_available" {
				t.Fatal("unresolved conflicts lost")
			}
			request.Resolutions = map[string]map[string]string{"collection": {"c": "keep", "e": "remote"}}
			plan, err = s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			installed, err = s.Apply(ctx, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertContent("c", "mine", true)
			assertContent("e", "restore", true)
			request.Resolutions = nil
			plan, err = s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if slices.ContainsFunc(plan.Updates, func(item UpdateItem) bool { return item.Conflict }) {
				t.Fatal("same upstream conflict repeated", plan.Updates)
			}
			remote["c"] = "another upstream edit"
			p = preview()
			request.PreviewID, request.CandidateID = p.ID, p.Candidates[0].ID
			plan, err = s.Plan(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(plan.Updates, func(item UpdateItem) bool { return item.MemberID == "c" && item.State == "conflict" }) {
				t.Fatal("keeping local content lost its applied baseline")
			}
			if kind == "lore.collection" {
				if _, err := lore.NewStore(dir).ListAll(); err != nil {
					t.Fatal("invalid merged lore", err)
				}
			}
		})
	}
}
