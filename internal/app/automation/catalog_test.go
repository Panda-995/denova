package automationapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/automation"
)

type catalogReadHost struct{ effectRecoveryHost }

func (catalogReadHost) RuntimeForTarget(context.Context, automation.ExecutionTarget) (Runtime, error) {
	return Runtime{}, errors.New("Project runtime is unavailable")
}

type countingCatalogHost struct {
	catalogReadHost
	calls int
}

func (h *countingCatalogHost) RuntimeForTarget(context.Context, automation.ExecutionTarget) (Runtime, error) {
	h.calls++
	return Runtime{}, errors.New("Project runtime is unavailable")
}

func TestProjectTasksOnlyLoadsRuntimesWithRuns(t *testing.T) {
	host := &countingCatalogHost{}
	service := NewService(host)
	target := automation.ExecutionTarget{Kind: automation.TargetKindWorkspace, ProjectID: "one"}
	tasks := []automation.Task{{Target: target}, {Target: target}}
	if _, err := service.projectTasks(tasks, nil); err != nil || host.calls != 0 {
		t.Fatalf("idle tasks loaded execution runtimes: calls=%d err=%v", host.calls, err)
	}
	for i := range tasks {
		tasks[i].LastRun = &automation.RunRecord{DeliveryStatus: automation.RunStatusSuccess}
	}
	projected, err := service.projectTasks(tasks, nil)
	if err != nil || host.calls != 1 {
		t.Fatalf("tasks did not share their Project lookup: calls=%d err=%v", host.calls, err)
	}
	for _, task := range projected {
		if task.LastRun.Status != automation.RunStatusSuccess {
			t.Fatalf("delivery fallback lost: %#v", task.LastRun)
		}
	}
}

func BenchmarkAutomationCatalog(b *testing.B) {
	root := b.TempDir()
	catalog := Catalog{DataDir: root}
	for index := range 50 {
		id := fmt.Sprintf("project-%d", index)
		location := automation.ProjectLocation{ProjectID: id, Workspace: filepath.Join(root, "projects", id), StateRoot: filepath.Join(root, "stores", id)}
		if err := os.MkdirAll(location.StateRoot, 0o700); err != nil {
			b.Fatal(err)
		}
		catalog.Projects = append(catalog.Projects, location)
	}
	service := NewService(catalogReadHost{effectRecoveryHost{catalog: catalog}})
	b.Run("per_project_requests", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for _, location := range catalog.Projects {
				if _, err := service.ListForProject(location.ProjectID, location.Workspace); err != nil {
					b.Fatal(err)
				}
				if _, err := service.InboxForProject(location.ProjectID, location.Workspace); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := service.CatalogSnapshot(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestCatalogSnapshotPreservesProjectOwnershipAndPartialFailures(t *testing.T) {
	root := t.TempDir()
	catalog := Catalog{DataDir: root}
	for _, id := range []string{"one", "two", "broken", "empty"} {
		location := automation.ProjectLocation{ProjectID: id, Workspace: filepath.Join(root, "projects", id), StateRoot: filepath.Join(root, "stores", id)}
		catalog.Projects = append(catalog.Projects, location)
		if id == "empty" {
			continue
		}
		store := automation.NewProjectStore(root, id, location.Workspace, location.StateRoot)
		task, err := store.Create(automation.TaskDefinition{Scope: automation.ScopeWorkspace, Name: id, Template: automation.TemplateCustomPrompt})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateInboxItem(automation.TriggerInboxItem{ID: "inbox-" + id, TaskID: task.ID, TriggerID: "manual", Scope: automation.ScopeWorkspace, Workspace: location.Workspace, Status: automation.InboxStatusPending, Purpose: automation.InboxPurposeTrigger}); err != nil {
			t.Fatal(err)
		}
		if id == "broken" {
			if err := os.WriteFile(filepath.Join(location.StateRoot, "automations", "tasks.json"), []byte("invalid JSON"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	groups, err := NewService(catalogReadHost{effectRecoveryHost{catalog: catalog}}).CatalogSnapshot()
	if err != nil || len(groups) != 4 {
		t.Fatalf("catalog lost Project results: groups=%#v err=%v", groups, err)
	}
	for index, group := range groups {
		id := catalog.Projects[index].ProjectID
		if group.ProjectID != id {
			t.Fatalf("catalog owner changed: %#v", group)
		}
		switch id {
		case "one", "two":
			if group.Error != "" || len(group.Tasks) != 1 || len(group.Inbox) != 1 || group.Tasks[0].Target.ProjectID != id || group.Inbox[0].ProjectID != id || group.Tasks[0].CatalogID == "" {
				t.Fatalf("Project data crossed catalog boundaries: %#v", group)
			}
		case "broken":
			if group.Error == "" || len(group.Tasks) != 0 || len(group.Inbox) != 0 {
				t.Fatalf("corrupt Project did not report its local failure: %#v", group)
			}
		case "empty":
			if group.Error != "" || group.Tasks == nil || group.Inbox == nil || len(group.Tasks) != 0 || len(group.Inbox) != 0 {
				t.Fatalf("empty Project must retain a successful empty result: %#v", group)
			}
		}
	}
}
