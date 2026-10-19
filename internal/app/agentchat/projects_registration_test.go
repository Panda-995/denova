package agentchat

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	projectdomain "denova/internal/project"

	git "github.com/go-git/go-git/v5"
)

func TestAddCodeProjectWithUserDataDirectory(t *testing.T) {
	for _, retry := range []bool{false, true} {
		name := "new registration"
		if retry {
			name = "retry failed registration"
		}
		t.Run(name, func(t *testing.T) {
			workspace, dataRoot := t.TempDir(), t.TempDir()
			userData := filepath.Join(workspace, ".denova")
			for relative, content := range map[string]string{
				"projects.json":          `{"version":5,"projects":[]}`,
				"config.toml":            "global configuration",
				"automations/tasks.json": "global automations",
			} {
				path := filepath.Join(userData, filepath.FromSlash(relative))
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := git.PlainInit(workspace, false); err != nil {
				t.Fatal(err)
			}
			gitConfigPath := filepath.Join(workspace, ".git", "config")
			gitConfig := []byte("[core]\nrepositoryformatversion = 0\nbare = false\n[extensions]\nworktreeConfig = true\n")
			if err := os.WriteFile(gitConfigPath, gitConfig, 0o600); err != nil {
				t.Fatal(err)
			}
			var previous projectdomain.Record
			var registryBefore []byte
			if retry {
				registry := projectdomain.NewRegistry(dataRoot)
				var err error
				previous, err = registry.Add(workspace, projectdomain.TypeBook, "My code")
				if err != nil {
					t.Fatal(err)
				}
				layout, err := registry.Layout(previous)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(layout.ArtifactsDir(), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(layout.ArtifactsDir(), "keep.txt"), []byte("existing project state"), 0o600); err != nil {
					t.Fatal(err)
				}
				registryBefore, err = os.ReadFile(filepath.Join(dataRoot, "projects.json"))
				if err != nil {
					t.Fatal(err)
				}
			}
			registry := projectdomain.NewRegistry(dataRoot)
			service := NewService(activeViewTestHost{}, registry)
			t.Cleanup(func() { service.Close(context.Background()) })
			record, err := service.AddProject(workspace)
			if err != nil {
				t.Fatal(err)
			}
			if record.Type != projectdomain.TypeGeneral {
				t.Fatalf("code project type = %q", record.Type)
			}
			layout, err := registry.Layout(record)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{layout.ConfigPath(), layout.AutomationsDir(), layout.VersionRepositoryDir()} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("user data or source history was copied into Project Store: %s, %v", path, err)
				}
			}
			if got, err := os.ReadFile(gitConfigPath); err != nil || !bytes.Equal(got, gitConfig) {
				t.Fatalf("source Git configuration changed: %q, %v", got, err)
			}
			if retry {
				if got, err := os.ReadFile(filepath.Join(layout.ArtifactsDir(), "keep.txt")); err != nil || string(got) != "existing project state" {
					t.Fatalf("existing Project Store changed: %q, %v", got, err)
				}
				if record.ID != previous.ID || record.StoreDirName != previous.StoreDirName || record.Name != previous.Name {
					t.Fatalf("retry changed project identity: before=%#v, after=%#v", previous, record)
				}
				backups, err := filepath.Glob(filepath.Join(dataRoot, "backups", "project-classification-v1", "projects-*.json"))
				if err != nil || len(backups) != 1 {
					t.Fatalf("registry backups = %v, %v", backups, err)
				}
				if got, err := os.ReadFile(backups[0]); err != nil || !bytes.Equal(got, registryBefore) {
					t.Fatalf("original registry backup changed: %q, %v", got, err)
				}
			}
			if again, err := service.AddProject(workspace); err != nil || again.ID != record.ID || again.Type != record.Type {
				t.Fatalf("repeated add = %#v, %v", again, err)
			}
		})
	}
}
