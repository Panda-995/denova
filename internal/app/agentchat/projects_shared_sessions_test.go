package agentchat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"denova/config"
	"denova/internal/agents/conversationconfig"
	"denova/internal/agents/session"
	projectdomain "denova/internal/project"
)

func TestProjectsAndHistoryShareEveryProjectSession(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := projectdomain.NewRegistry(filepath.Join(root, "denova"))
	record, err := registry.Add(workspace, projectdomain.TypeGeneral, "")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.EnsureStore(record)
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.NewStore(layout.SessionsDir())
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := store.Create("Ordinary conversation")
	if err != nil {
		t.Fatal(err)
	}
	seed := conversationconfig.Config{
		AgentKind:     config.AgentKindGeneral,
		ProfileID:     "default",
		ThinkingLevel: "medium",
		ApprovalMode:  config.AgentApprovalWrite,
	}
	configured, err := store.CreateWithRuntimeConfig("Configured conversation", seed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOrCreate("platform-owned-npc"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	service := NewService(activeViewTestHost{}, registry)
	t.Cleanup(func() { service.Close(context.Background()) })
	if _, err := service.ResolveBinding(Binding{ProjectID: record.ID, SessionID: "platform-owned-npc"}); err == nil {
		t.Fatal("adopted a Platform Agent as a Project conversation")
	}

	projects := service.Projects("")
	assertProjectSessionIDs(t, projects, ordinary.ID, configured.ID)

	history, err := service.History(context.Background(), HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if history.Total != 2 || len(history.Items) != 2 {
		t.Fatalf("history = %#v", history)
	}
	historyIDs := map[string]bool{}
	for _, item := range history.Items {
		historyIDs[item.Session.ID] = true
	}
	if !historyIDs[ordinary.ID] || !historyIDs[configured.ID] {
		t.Fatalf("history session IDs = %#v", historyIDs)
	}
	unrelatedPath := filepath.Join(root, "unrelated")
	if err := os.MkdirAll(unrelatedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	unrelated, err := registry.Add(unrelatedPath, projectdomain.TypeGeneral, "Unrelated")
	if err != nil {
		t.Fatal(err)
	}
	assertProjectSessionIDs(t, service.Projects(record.ID), ordinary.ID, configured.ID)
	if _, opened := service.stores[unrelated.ID]; opened {
		t.Fatal("project-scoped listing opened an unrelated Session Store")
	}
}

func assertProjectSessionIDs(t *testing.T, projects []Project, wantIDs ...string) {
	t.Helper()
	if len(projects) != 1 || projects[0].Total != len(wantIDs) || len(projects[0].Sessions) != len(wantIDs) {
		t.Fatalf("projects = %#v", projects)
	}
	gotIDs := map[string]bool{}
	for _, conversation := range projects[0].Sessions {
		gotIDs[conversation.ID] = true
	}
	for _, wantID := range wantIDs {
		if !gotIDs[wantID] {
			t.Fatalf("project session IDs = %#v, missing %q", gotIDs, wantID)
		}
	}
}
