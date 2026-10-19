package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"denova/config"
	"github.com/google/uuid"
)

func pluginPanelCandidate(t *testing.T, m *Manager, projectID, id string) Candidate {
	t.Helper()
	candidate := testCandidate(t, m, projectID, "tool", id, Plugin)
	manifest := candidate.Manifest
	manifest.Views = []View{{ID: "main"}, {ID: "other"}}
	for i := range manifest.Views {
		manifest.Views[i].Source.Kind = "static"
		manifest.Views[i].Source.Path = "index.html"
	}
	manifest.Contributes.Panels = []PanelContribution{{ID: "board", TitleKey: "board", Contexts: []ContributionContext{ContextWriting, ContextGame, ContextGeneral}, ViewID: "main"}, {ID: "second", TitleKey: "board", Contexts: []ContributionContext{ContextGeneral}, ViewID: "other"}}
	manifest.Contributes.Commands = []CommandContribution{{ID: "count", TitleKey: "count", Contexts: []ContributionContext{ContextGeneral, ContextWriting}, Target: CommandTarget{Kind: "tool", ID: "probe"}}}
	for _, language := range []string{"zh", "en"} {
		var labels map[string]any
		if err := json.Unmarshal(candidate.files[language+".json"], &labels); err != nil {
			t.Fatal(err)
		}
		labels["board"], labels["count"] = "Board", "Count"
		labels["text"] = "Text"
		candidate.files[language+".json"], _ = json.Marshal(labels)
	}
	candidate.files["index.html"] = []byte("<!doctype html><title>Plugin board</title>")
	var tool map[string]any
	if err := json.Unmarshal(candidate.files["probe.json"], &tool); err != nil {
		t.Fatal(err)
	}
	tool["inputSchema"].(map[string]any)["properties"].(map[string]any)["text"].(map[string]any)["x-titleKey"] = "text"
	candidate.files["probe.json"], _ = json.Marshal(tool)
	candidate.files[Plugin.manifestFile()], _ = json.Marshal(manifest)
	result, err := m.freeze(Plugin, candidate.files)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPluginPanelsCommandsAndProjectRevocation(t *testing.T) {
	m, projectID := testManager(t)
	release := testInstall(t, m, pluginPanelCandidate(t, m, projectID, "test.interface"))
	entries, err := m.ProjectPlugins(projectID, ContextGeneral, "en-US")
	if err != nil || len(entries) != 1 || len(entries[0].Actions) != 3 || len(m.RuntimeSnapshots()) != 0 {
		t.Fatalf("discovery: %#v %v", entries, err)
	}
	options := OpenOptions{ParentOrigin: "http://127.0.0.1:15173"}
	first := OpenPluginAction{PluginID: release.Manifest.ID, ReleaseID: release.Ref.ReleaseID, ProjectID: projectID, Context: ContextGeneral, Kind: "panel", ActionID: "board", ConsumerID: uuid.NewString(), OpenOptions: options}
	invalid := first
	invalid.ConsumerID = "invalid"
	if _, err := m.OpenPluginAction(t.Context(), invalid); err == nil || len(m.RuntimeSnapshots()) != 0 {
		t.Fatal("invalid consumer started plugin code")
	}
	opened, err := m.OpenPluginAction(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.ConsumerID, second.ActionID = uuid.NewString(), "second"
	configuration, err := m.ProjectConfiguration(projectID)
	if err != nil {
		t.Fatal(err)
	}
	configuration.Extensions.Models["unrelated.plugin/image"] = "other-profile"
	if _, err := m.SaveProjectConfiguration(t.Context(), projectID, ProjectConfigurationInput{ExpectedRevision: configuration.Revision, Extensions: configuration.Extensions}); err != nil {
		t.Fatal(err)
	}
	other, err := m.OpenPluginAction(t.Context(), second)
	if err != nil || other.ID != opened.ID || other.ViewURL == opened.ViewURL {
		t.Fatalf("shared runtime with distinct views: %#v %v", other, err)
	}
	command := first
	command.Kind, command.ActionID, command.ConsumerID = "command", "count", uuid.NewString()
	called, err := m.OpenPluginAction(t.Context(), command)
	if err != nil || called.ID != opened.ID || called.ViewURL != "" {
		t.Fatalf("command binding: %#v %v", called, err)
	}
	if err := m.ReleaseConsumer(t.Context(), opened.ID, first.ConsumerID); err != nil {
		t.Fatal(err)
	}
	if len(m.RuntimeSnapshots()) != 1 {
		t.Fatal("closing a panel stopped another consumer")
	}
	request, _ := http.NewRequest("GET", opened.Connection.BaseURL+"/context", nil)
	request.Header.Set("Authorization", "Bearer "+opened.Connection.Token)
	request.Header.Set("X-Denova-Consumer", first.ConsumerID)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("closed consumer retained request authority")
	}
	if err := m.ReleaseConsumer(t.Context(), other.ID, second.ConsumerID); err != nil {
		t.Fatal(err)
	}
	status, body := testRequest(t, called.Connection, "POST", "/tools/test.interface/probe/invoke", "", map[string]any{"input": map[string]string{"text": "A🌷中"}})
	if status != 200 {
		t.Fatalf("surviving command failed: %d %s", status, body)
	}
	if err := m.ReleaseConsumer(t.Context(), called.ID, command.ConsumerID); err != nil {
		t.Fatal(err)
	}
	if len(m.RuntimeSnapshots()) != 0 {
		t.Fatal("last consumer leaked its runtime")
	}
	opened, err = m.OpenPluginAction(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Connection.Token == other.Connection.Token {
		t.Fatal("stopped credentials were reused")
	}
	configuration, err = m.ProjectConfiguration(projectID)
	if err != nil {
		t.Fatal(err)
	}
	configuration.Extensions.DisabledPlugins = []string{release.Manifest.ID}
	_, err = m.SaveProjectConfiguration(t.Context(), projectID, ProjectConfigurationInput{ExpectedRevision: configuration.Revision, Extensions: configuration.Extensions})
	if err != nil || len(m.RuntimeSnapshots()) != 0 {
		t.Fatalf("project disable did not stop owned runtimes: %v", err)
	}
	if _, err := m.OpenPluginAction(t.Context(), first); err == nil {
		t.Fatal("project-disabled panel started")
	}
	if set, err := m.HostAgentTools(pluginTestConfig(t, m, projectID), config.AgentKindIDE); err != nil || set != nil {
		t.Fatalf("disabled tools: %v", err)
	}
}

func TestPluginManifestBoundariesAndStaticViews(t *testing.T) {
	m, projectID := testManager(t)
	base := pluginPanelCandidate(t, m, projectID, "test.static-plugin")
	manifest := base.Manifest
	manifest.Runtime = nil
	manifest.Contributes.Tools, manifest.Contributes.Toolsets, manifest.Contributes.Commands = nil, nil, nil
	base.files[Plugin.manifestFile()], _ = json.Marshal(manifest)
	candidate, err := m.freeze(Plugin, base.files)
	if err != nil {
		t.Fatal(err)
	}
	release := testInstall(t, m, candidate)
	t.Setenv("PATH", t.TempDir())
	opened, err := m.OpenPluginAction(t.Context(), OpenPluginAction{PluginID: release.Manifest.ID, ReleaseID: release.Ref.ReleaseID, ProjectID: projectID, Context: ContextWriting, Kind: "panel", ActionID: "board", ConsumerID: uuid.NewString(), OpenOptions: OpenOptions{ParentOrigin: "http://127.0.0.1:15173"}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(opened.ViewURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("static panel requires no Node: %d", response.StatusCode)
	}
	for _, mutate := range []func(*Manifest){
		func(m *Manifest) { m.MinHostVersion = "99.0.0" },
		func(m *Manifest) { m.MinHostVersion = "" },
		func(m *Manifest) { m.Contributes.Panels[0].Contexts = []ContributionContext{"editor"} },
		func(m *Manifest) { m.Contributes.Panels[0].ViewID = "missing" },
		func(m *Manifest) { m.Contributes.Panels[0].TitleKey = "missing" },
	} {
		var value Manifest
		if err := json.Unmarshal(base.files[Plugin.manifestFile()], &value); err != nil {
			t.Fatal(err)
		}
		mutate(&value)
		files := map[string][]byte{}
		for key, raw := range base.files {
			files[key] = raw
		}
		files[Plugin.manifestFile()], _ = json.Marshal(value)
		if _, err := m.freeze(Plugin, files); err == nil {
			t.Fatalf("invalid manifest was accepted: %#v", value)
		}
	}
}

func TestProjectPluginConfigurationPreservesOtherSettings(t *testing.T) {
	m, projectID := testManager(t)
	_, layout, _ := m.registry.Resolve(projectID, true)
	if err := config.WriteSettingsFile(layout.ConfigPath(), config.Settings{Theme: "light"}); err != nil {
		t.Fatal(err)
	}
	before, err := m.ProjectConfiguration(projectID)
	if err != nil {
		t.Fatal(err)
	}
	input := ProjectConfigurationInput{ExpectedRevision: before.Revision, Extensions: config.ExtensionSettings{DisabledPlugins: []string{"test.disabled"}, Models: map[string]string{"builtin/assistant": "writer"}}}
	if _, err := m.SaveProjectConfiguration(context.Background(), projectID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveProjectConfiguration(context.Background(), projectID, input); err == nil {
		t.Fatal("stale project settings overwrote a new snapshot")
	}
	stored, err := config.ReadSettingsFile(filepath.Join(layout.StoreRoot, "config.toml"))
	if err != nil || stored.Theme != "light" || stored.Extensions.Models["builtin/assistant"] != "writer" {
		t.Fatalf("settings were lost: %#v %v", stored, err)
	}
}
