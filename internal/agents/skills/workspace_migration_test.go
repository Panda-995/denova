package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceSkillMigrationDistinguishesUserDataFromBookState(t *testing.T) {
	for _, directory := range []string{".denova", ".nova"} {
		for _, registry := range []string{"", "projects.json", "books.json"} {
			t.Run(directory+"/"+registry, func(t *testing.T) {
				workspace := t.TempDir()
				private := filepath.Join(workspace, directory)
				source := filepath.Join(private, "skills", "example", "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
					t.Fatal(err)
				}
				const content = "---\nname: example\ndescription: Example\n---\nKeep skill content.\n"
				if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				if registry != "" {
					if err := os.WriteFile(filepath.Join(private, registry), []byte("{}"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := MigrateWorkspaceSkills(workspace); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(workspace, "skills", "example", "SKILL.md")
				if got, err := os.ReadFile(target); registry != "" {
					if !os.IsNotExist(err) {
						t.Fatalf("global Skill was imported into project: %q, %v", got, err)
					}
				} else if err != nil || string(got) != content {
					t.Fatalf("released Book Skill did not migrate: %q, %v", got, err)
				}
				if got, err := os.ReadFile(source); err != nil || string(got) != content {
					t.Fatalf("source Skill changed: %q, %v", got, err)
				}
			})
		}
	}
}
