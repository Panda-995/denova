package platform

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func namedToolCandidate(t *testing.T, m *Manager, projectID, source, owner, name string) (Candidate, error) {
	t.Helper()
	dev := testSource(t, m, projectID, source, "tool", owner, Plugin)
	_, layout, err := m.registry.Resolve(projectID, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(layout.ContentRoot, source, Plugin.manifestFile())
	var manifest Manifest
	if err := readJSON(path, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Contributes.Tools[0].ID = name
	manifest.Contributes.Toolsets[0].Tools = []string{name}
	manifest.Description.English = source
	if err := writeJSON(path, manifest); err != nil {
		t.Fatal(err)
	}
	return m.CheckDevelopment(dev.ID)
}

func TestPluginToolNameInstallationOwnership(t *testing.T) {
	for _, mode := range []string{"single", "batch"} {
		t.Run(mode, func(t *testing.T) {
			m, projectID := testManager(t)
			candidate := func(source, owner, name string) Candidate {
				c, err := namedToolCandidate(t, m, projectID, source, owner, name)
				if err != nil {
					t.Fatal(err)
				}
				return c
			}
			install := func(c Candidate) error {
				if mode == "single" {
					_, err := m.Install(c.ID, c.Manifest.Permissions.Required)
					return err
				}
				state, err := m.InstallState()
				if err != nil {
					t.Fatal(err)
				}
				return m.WithInstallBatch(context.Background(), state, []InstallSelection{{CandidateID: c.ID, Grants: c.Manifest.Permissions.Required}}, func(writes []InstallWrite) error {
					for _, write := range writes {
						if err := writeBytes(filepath.Join(m.root, filepath.FromSlash(write.Path)), write.Content); err != nil {
							return err
						}
					}
					return nil
				})
			}
			for _, c := range []Candidate{candidate("a", "test.a", "comic_inspect"), candidate("b", "test.b", "comic-inspect"), candidate("self-update", "test.a", "comic_inspect")} {
				if err := install(c); err != nil {
					t.Fatal(err)
				}
			}
			// Disabled plugins and retained releases still own their names: existing
			// consumers may be pinned to older releases when another plugin installs.
			if err := install(candidate("a-next", "test.a", "comic_inspect_v2")); err != nil {
				t.Fatal(err)
			}
			if err := m.SetAvailability(context.Background(), Plugin, "test.a", PackageAvailability{}); err != nil {
				t.Fatal(err)
			}
			for _, c := range []Candidate{candidate("new-conflict", "test.c", "comic_inspect"), candidate("update-conflict", "test.b", "comic_inspect_v2")} {
				before, _ := m.InstallState()
				err := install(c)
				if err == nil {
					t.Fatal("conflicting tool name was installed")
				}
				status, detail := ErrorResponse(err)
				if status != 409 || detail.Code != "TOOL_NAME_CONFLICT" || !strings.Contains(detail.Diagnostic, "plugin_"+c.Manifest.Contributes.Tools[0].ID) || !strings.Contains(detail.Diagnostic, "test.a") || !strings.Contains(detail.Diagnostic, c.Manifest.ID) {
					t.Fatalf("conflict must identify the tool and both owners: %v", err)
				}
				after, _ := m.InstallState()
				if after != before {
					t.Fatal("rejected install changed installed pointers or settings")
				}
			}
			if err := m.SetAvailability(context.Background(), Plugin, "test.a", PackageAvailability{Removed: true}); err != nil {
				t.Fatal(err)
			}
			if err := install(candidate("after-removal", "test.c", "comic_inspect")); err != nil {
				t.Fatal(err)
			}
			if err := install(candidate("reinstall", "test.a", "another_name")); err == nil {
				t.Fatal("reinstall restored conflicting retained releases")
			}
			if err := m.SetAvailability(context.Background(), Plugin, "test.a", PackageAvailability{Enabled: true}); err == nil {
				t.Fatal("availability change bypassed name ownership")
			}
		})
	}
}

func TestPluginToolNameBatchConflictBeforeCommit(t *testing.T) {
	m, projectID := testManager(t)
	selections := []InstallSelection{}
	for _, owner := range []string{"test.a", "test.b"} {
		c, err := namedToolCandidate(t, m, projectID, owner, owner, "comic_render")
		if err != nil {
			t.Fatal(err)
		}
		selections = append(selections, InstallSelection{CandidateID: c.ID, Grants: c.Manifest.Permissions.Required})
	}
	state, _ := m.InstallState()
	committed := false
	err := m.WithInstallBatch(context.Background(), state, selections, func([]InstallWrite) error { committed = true; return nil })
	if err == nil || committed {
		t.Fatalf("batch collision reached commit: %v", err)
	}
	if _, detail := ErrorResponse(err); detail.Code != "TOOL_NAME_CONFLICT" {
		t.Fatal(err)
	}
}

func TestToolNameManifestAdmission(t *testing.T) {
	for _, name := range []string{"comic.inspect", "ComicInspect", strings.Repeat("a", 58)} {
		t.Run(name, func(t *testing.T) {
			m, projectID := testManager(t)
			if _, err := namedToolCandidate(t, m, projectID, "source", "test.tools", name); err == nil {
				t.Fatal("accepted a tool ID that cannot be used verbatim as a prefixed model function name")
			}
		})
	}
}
