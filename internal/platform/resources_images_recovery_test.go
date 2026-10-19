package platform

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/url"
	"reflect"
	"testing"
	"time"

	"denova/internal/project"
	"github.com/google/uuid"
)

func TestPluginImagesRecoverAcrossProjectAndSessionConsumers(t *testing.T) {
	m, projectID := testManager(t)
	finish := make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	host := &resourceTestHost{project: projectID, profile: "test-image", image: resourcePNG(t), entered: make(chan struct{}), release: finish}
	m.ConfigureResources(host)
	candidate := pluginPanelCandidate(t, m, projectID, "test.image-workspace")
	manifest := candidate.Manifest
	manifest.Runtime, manifest.Settings = nil, nil
	manifest.Contributes.Tools, manifest.Contributes.Toolsets, manifest.Contributes.Commands = nil, nil, nil
	manifest.Contributes.Pages = []PageContribution{{ID: "canvas", TitleKey: "board", ViewID: "main"}}
	manifest.Permissions.Required = []string{"assets.read", "images.generate"}
	candidate.files[Plugin.manifestFile()], _ = json.Marshal(manifest)
	candidate, err := m.freeze(Plugin, candidate.files)
	if err != nil {
		t.Fatal(err)
	}
	release := testInstall(t, m, candidate)
	options := OpenOptions{ParentOrigin: "http://127.0.0.1:15173"}
	session, err := m.ActivatePlugin(t.Context(), ActivatePlugin{PluginID: manifest.ID, ReleaseID: release.Ref.ReleaseID, Scope: Scope{Kind: "session", ProjectID: projectID, SessionID: "creator-session"}, ConsumerID: uuid.NewString(), OpenOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	command := ImageRequest{CommandID: "portrait", ModelID: "test-image", Prompt: "A portrait"}
	if status, data := testRequest(t, session.Connection, "POST", "/images/generations", "", command); status != 202 {
		t.Fatalf("session generation: %d %s", status, data)
	}
	select {
	case <-host.entered:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	openPage := func() RuntimeSnapshot {
		t.Helper()
		page, err := m.OpenPluginAction(t.Context(), OpenPluginAction{PluginID: manifest.ID, ReleaseID: release.Ref.ReleaseID, ProjectID: projectID, Context: ContextGeneral, Kind: "page", ActionID: "canvas", ConsumerID: uuid.NewString(), OpenOptions: options})
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	page := openPage()
	status, data := testRequest(t, page.Connection, "GET", "/images/generations/portrait", "", nil)
	var running ImageResult
	if err := json.Unmarshal(data, &running); err != nil || status != 200 || running.Status != "running" {
		t.Fatalf("Project page cannot recover its Session's running image: %d %s %v", status, data, err)
	}
	if status, data := testRequest(t, page.Connection, "POST", "/images/generations", "", command); status != 202 || host.calls.Load() != 1 {
		t.Fatalf("cross-consumer running replay repeated the paid request: %d %s calls=%d", status, data, host.calls.Load())
	}
	// Reopening the observing page must not cancel the Session-owned execution.
	if err := m.ReleaseConsumer(t.Context(), page.ID, page.Connection.ConsumerID); err != nil {
		t.Fatal(err)
	}
	page = openPage()
	status, data = testRequest(t, page.Connection, "GET", "/images/generations/portrait", "", nil)
	if err := json.Unmarshal(data, &running); err != nil || status != 200 || running.Status != "running" {
		t.Fatalf("observer close cancelled the owner: %d %s %v", status, data, err)
	}
	close(finish)
	result := awaitImage(t, page.Connection, command.CommandID)
	if result.Status != "completed" || len(result.Images) != 1 {
		t.Fatalf("page recovery: %+v", result)
	}
	assetRoute := "/assets/content?kind=generated&path=" + url.QueryEscape(result.Images[0].Asset.Path)
	if status, data := testRequest(t, page.Connection, "GET", assetRoute, "", nil); status != 200 || !bytes.Equal(data, host.image) {
		t.Fatalf("page cannot read recovered image: %d %s", status, data)
	}
	if err := m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	m.ConfigureResources(host)
	page = openPage()
	if recovered := awaitImage(t, page.Connection, command.CommandID); !reflect.DeepEqual(recovered, result) {
		t.Fatalf("cold recovery changed the receipt: got=%+v want=%+v", recovered, result)
	}
	if status, data := testRequest(t, page.Connection, "GET", assetRoute, "", nil); status != 200 || !bytes.Equal(data, host.image) {
		t.Fatalf("cold recovery lost image bytes: %d %s", status, data)
	}
	if status, data := testRequest(t, page.Connection, "POST", "/images/generations", "", command); status != 200 || host.calls.Load() != 1 {
		t.Fatalf("page replay repeated the paid request: %d %s calls=%d", status, data, host.calls.Load())
	}
	changed := command
	changed.Prompt = "A different portrait"
	if status, data := testRequest(t, page.Connection, "POST", "/images/generations", "", changed); status != 409 {
		t.Fatalf("cross-consumer input conflict: %d %s", status, data)
	}
	otherProject, err := m.registry.Add(t.TempDir(), project.TypeGeneral, "Another Project")
	if err != nil {
		t.Fatal(err)
	}
	otherManifest := manifest
	otherManifest.ID = "test.other-image-workspace"
	otherFiles := maps.Clone(candidate.files)
	otherFiles[Plugin.manifestFile()], _ = json.Marshal(otherManifest)
	otherCandidate, err := m.freeze(Plugin, otherFiles)
	if err != nil {
		t.Fatal(err)
	}
	otherRelease := testInstall(t, m, otherCandidate)
	preview, err := m.PreparePreview(candidate.ID, manifest.Permissions.Required)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		name    string
		project string
		release Release
	}{
		{"another Project", otherProject.ID, release},
		{"another plugin", projectID, otherRelease},
		{"preview", projectID, preview},
	} {
		t.Run(target.name, func(t *testing.T) {
			opened, err := m.ActivatePlugin(t.Context(), ActivatePlugin{PluginID: target.release.Manifest.ID, ReleaseID: target.release.Ref.ReleaseID, Scope: Scope{Kind: "project", ProjectID: target.project}, ConsumerID: uuid.NewString(), OpenOptions: options})
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range []string{"/images/generations/portrait", assetRoute} {
				if status, data := testRequest(t, opened.Connection, "GET", route, "", nil); status != 404 {
					t.Fatalf("image authority escaped to %s: %d %s", target.name, status, data)
				}
			}
		})
	}
}
