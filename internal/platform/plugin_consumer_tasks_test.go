package platform

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"denova/internal/agents/canonicalstore"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	"github.com/google/uuid"
)

// A page can finish its HTTP request while both paid image work and an Agent
// continue. Closing that page must cancel both without stopping another page.
func TestPluginConsumerCancelsOwnedAgentAndImage(t *testing.T) {
	m, projectID := testManager(t)
	store, err := canonicalstore.New(m.root, m.registry)
	if err != nil {
		t.Fatal(err)
	}
	model := &cancellablePlatformModel{started: make(chan struct{})}
	m.ConfigureAgents(store, func(context.Context, string) (agentmodel.BaseChatModel, agentschema.CapabilityIdentity, error) {
		return model, agentschema.CapabilityIdentity{Kind: "test.consumer", Version: 1}, nil
	})
	host := &resourceTestHost{project: projectID, profile: "image", blocking: true, entered: make(chan struct{})}
	m.ConfigureResources(host)
	candidate := pluginPanelCandidate(t, m, projectID, "test.consumer")
	manifest := candidate.Manifest
	manifest.Permissions.Required = append(manifest.Permissions.Required, "agents.run", "images.generate")
	manifest.ModelSlots = []ModelSlot{{ID: "art", Kind: "image", TitleKey: "board", Required: true}}
	candidate.files[Plugin.manifestFile()], _ = json.Marshal(manifest)
	candidate, err = m.freeze(Plugin, candidate.files)
	if err != nil {
		t.Fatal(err)
	}
	release := testInstall(t, m, candidate)
	cfg, err := m.ProjectConfiguration(projectID)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Extensions.Models = map[string]string{"builtin/assistant": "text", "test.consumer/art": "image"}
	if _, err = m.SaveProjectConfiguration(t.Context(), projectID, ProjectConfigurationInput{ExpectedRevision: cfg.Revision, Extensions: cfg.Extensions}); err != nil {
		t.Fatal(err)
	}
	input := OpenPluginAction{PluginID: release.Manifest.ID, ReleaseID: release.Ref.ReleaseID, ProjectID: projectID, Context: ContextGeneral, Kind: "panel", ActionID: "board", ConsumerID: uuid.NewString(), OpenOptions: OpenOptions{ParentOrigin: "http://127.0.0.1:15173"}}
	first, err := m.OpenPluginAction(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.ConsumerID = uuid.NewString()
	input.ActionID = "second"
	second, err := m.OpenPluginAction(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	status, data := testRequest(t, first.Connection, "POST", "/agents/sessions", "", EnsureAgentSession{ProjectID: projectID, Definition: "builtin/assistant", Key: "owned"})
	if status != 201 {
		t.Fatalf("session: %d %s", status, data)
	}
	var session AgentSession
	_ = json.Unmarshal(data, &session)
	status, data = testRequest(t, first.Connection, "POST", "/agents/sessions/"+session.Ref.SessionID+"/runs", "", map[string]any{"commandId": "owned", "input": map[string]string{"text": "Wait"}})
	if status != 202 {
		t.Fatalf("run: %d %s", status, data)
	}
	var run RunResult
	_ = json.Unmarshal(data, &run)
	status, data = testRequest(t, first.Connection, "POST", "/images/generations", "", ImageRequest{CommandID: "owned", ModelSlot: "art", Prompt: "A tree"})
	if status != 202 {
		t.Fatalf("image: %d %s", status, data)
	}
	for _, started := range []chan struct{}{model.started, host.entered} {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("task did not start")
		}
	}
	if err = m.ReleaseConsumer(t.Context(), first.ID, first.Connection.ConsumerID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, data = testRequest(t, second.Connection, "GET", "/agents/runs/"+run.Run.RunID, "", nil)
		_ = json.Unmarshal(data, &run)
		_, data = testRequest(t, second.Connection, "GET", "/images/generations/owned", "", nil)
		var image ImageResult
		_ = json.Unmarshal(data, &image)
		if run.Status != "running" && run.Status != "accepted" && image.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("page leaked work: agent=%s image=%s", run.Status, image.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(m.RuntimeSnapshots()) != 1 {
		t.Fatal("closing first page stopped the second")
	}
	status, data = testRequest(t, second.Connection, "GET", "/capabilities", "", nil)
	var caps Capabilities
	_ = json.Unmarshal(data, &caps)
	if status != 200 || !caps.Capabilities["agents.run"].Configured || caps.Capabilities["stories.read"].Applicable {
		t.Fatalf("capability admission: %s", data)
	}
	if model.calls.Load() != 1 || host.calls.Load() != 1 {
		t.Fatal("cancellation replayed paid work")
	}
}
