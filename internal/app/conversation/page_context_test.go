package conversationapp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"denova/config"
	chat "denova/internal/agents/chat"
	"denova/internal/agents/lifecycle"
	agentrun "denova/internal/agents/run"
	"denova/internal/agents/runtime"
	"denova/internal/agents/session"
	project "denova/internal/project"
)

func TestPageContextSurvivesDurableInputAndUsesProjectPreparation(t *testing.T) {
	snapshot := json.RawMessage(`{"pageId":"page-one","selection":{"x":0.2},"references":["reference-one"]}`)
	request := chat.CaptureChatRequestCallerInput(chat.ChatRequest{CommandID: "command-one", Message: "Change this image", WritingSkill: "novel-lite", PageContext: &chat.PageContext{PluginID: "example.art", PageID: "studio", Data: snapshot}})
	// The accepted command owns its copy even if the caller reuses its buffer.
	copy(snapshot, []byte(`{"pageId":"page-two"}`))
	restored := chat.CallerView(request).Request()
	if !strings.Contains(string(restored.PageContext.Data), "page-one") {
		t.Fatal("page changed after admission")
	}
	input, err := lifecycle.TurnInput(lifecycle.TurnStart, restored, agentrun.Options{})
	if err != nil {
		t.Fatal(err)
	}
	native, err := lifecycle.DecodeTurnHostData(input)
	if err != nil || native.ChatRequest().PageContext == nil || string(native.ChatRequest().PageContext.Data) != string(restored.PageContext.Data) {
		t.Fatalf("Native recovery lost the page snapshot: %v", err)
	}
	encoded, err := json.Marshal(agentruntime.ExternalCycleInput{Request: restored})
	if err != nil {
		t.Fatal(err)
	}
	var cold agentruntime.ExternalCycleInput
	if err := json.Unmarshal(encoded, &cold); err != nil {
		t.Fatal(err)
	}
	if string(cold.Request.PageContext.Data) != string(restored.PageContext.Data) {
		t.Fatal("cold recovery lost the page snapshot")
	}
	root := t.TempDir()
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess, err := store.GetOrCreate("page-test")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	adapter := Runtime{ProjectID: "project-one", ProjectType: project.TypeGeneral, AgentKind: config.AgentKindGeneral, Session: sess, Workspace: root, Config: config.Config{DenovaDir: t.TempDir()}, PageInstructions: func(_ context.Context, projectID, pluginID, pageID string) (string, error) {
		if projectID != "project-one" || pluginID != "example.art" || pageID != "studio" {
			t.Fatal("page resolver used another scope")
		}
		called = true
		return "Use the page's image tools.", nil
	}}
	_, prepared, err := Prepare(t.Context(), adapter, cold.Request)
	if err != nil {
		t.Fatal(err)
	}
	if !called || prepared.PageInstructions == "" || prepared.WritingSkill != "" {
		t.Fatal("page workflow or shared preparation was bypassed")
	}
	prepared.PageContext.Data = json.RawMessage(`"` + strings.Repeat("x", 32<<10) + `"`)
	if _, _, err := Prepare(t.Context(), adapter, prepared); err == nil {
		t.Fatal("oversized page context accepted")
	}
}
