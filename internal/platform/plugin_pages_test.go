package platform

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/app/projectfiles"
	imagegen "denova/internal/image/generation"
	"github.com/google/uuid"
)

type pageContentTestHost struct {
	*resourceTestHost
	files   *projectfiles.Service
	manager *Manager
}

func (h *pageContentTestHost) ReadProjectAsset(ctx context.Context, project, path string) ([]byte, error) {
	_, layout, err := h.manager.registry.Resolve(project, true)
	if err != nil {
		return nil, err
	}
	return ReadRasterAsset(layout.ContentRoot, path)
}
func (h *pageContentTestHost) ReadProjectContent(ctx context.Context, project, path string) (FileSnapshot, error) {
	file, err := h.files.ReadFile(ctx, project, path)
	return FileSnapshot{Content: file.Content, Revision: file.Revision}, err
}
func (h *pageContentTestHost) SaveProjectContent(ctx context.Context, project string, input ContentDocument) (string, error) {
	revision := "missing"
	if input.ExpectedRevision != nil {
		revision = *input.ExpectedRevision
	}
	file, err := h.files.SaveFile(ctx, project, projectfiles.SaveRequest{Path: input.Path, Content: input.Content, BaseRevision: revision})
	return file.Revision, err
}
func (h *pageContentTestHost) CreateProjectAsset(ctx context.Context, project, path string, data []byte) error {
	_, err := h.files.CreateAsset(ctx, project, path, data)
	return err
}

func TestPluginPageProjectContentAndImageEditLifecycle(t *testing.T) {
	m, project := testManager(t)
	candidate := pluginPanelCandidate(t, m, project, "test.comic")
	manifest := candidate.Manifest
	manifest.ProjectContent = []string{"comic"}
	manifest.Contributes.Pages = []PageContribution{{ID: "studio", TitleKey: "board", ViewID: "main", Agent: &PageAgent{Instructions: "Create comics."}}}
	manifest.ModelSlots = nil
	manifest.Permissions.Required = []string{"tools.invoke", "assets.read", "images.generate", "project.content.read", "project.content.write"}
	candidate.files[Plugin.manifestFile()], _ = json.Marshal(manifest)
	candidate, err := m.freeze(Plugin, candidate.files)
	if err != nil {
		t.Fatal(err)
	}
	release := testInstall(t, m, candidate)
	files := projectfiles.NewService(m.registry)
	defer files.Close()
	host := &pageContentTestHost{resourceTestHost: &resourceTestHost{project: project, profile: "test-image", image: resourcePNG(t), requests: make(chan ImageRequest, 2)}, files: files, manager: m}
	m.ConfigureResources(host)
	pages, err := m.PluginPages()
	if err != nil || len(pages) != 1 || pages[0].ID != "studio" || !pages[0].Agent {
		t.Fatalf("pages: %+v %v", pages, err)
	}
	if instructions, err := m.PageInstructions(project, manifest.ID, "studio"); err != nil || instructions != "Create comics." {
		t.Fatalf("host page instructions: %q %v", instructions, err)
	}
	opened, err := m.OpenPluginAction(t.Context(), OpenPluginAction{PluginID: manifest.ID, ReleaseID: release.Ref.ReleaseID, ProjectID: project, Context: ContextGeneral, Kind: "page", ActionID: "studio", ConsumerID: uuid.NewString(), OpenOptions: OpenOptions{ParentOrigin: "http://127.0.0.1:15173"}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, input any) []byte {
		t.Helper()
		status, data := testRequest(t, opened.Connection, method, path, "", input)
		if status < 200 || status >= 300 {
			t.Fatalf("%s %s: %d %s", method, path, status, data)
		}
		return data
	}
	call("PUT", "/project-content/file", ContentDocument{Path: "comic/comic.json", Content: `{"version":1,"episodes":[]}`})
	data := call("GET", "/project-content/file?path=comic/comic.json", nil)
	var file FileSnapshot
	_ = json.Unmarshal(data, &file)
	missing := "missing"
	if status, _ := testRequest(t, opened.Connection, "PUT", "/project-content/file", "", ContentDocument{Path: "comic/comic.json", Content: `{}`, ExpectedRevision: &missing}); status != 409 {
		t.Fatalf("lost-update fence: %d", status)
	}
	for _, path := range []string{"novel/story.md", "comic/../story.md", "comic/CON.json"} {
		if status, _ := testRequest(t, opened.Connection, "PUT", "/project-content/file", "", ContentDocument{Path: path, Content: "overwrite"}); status < 400 {
			t.Fatalf("escaped content authority: %s", path)
		}
	}
	// Use the application-owned asset seam to seed the source image.
	if _, err := files.CreateAsset(t.Context(), project, "comic/assets/pages/page/source.png", host.image); err != nil {
		t.Fatal(err)
	}
	source := AssetRef{Kind: "project", Path: "comic/assets/pages/page/source.png"}
	input := ImageRequest{CommandID: "edit-page", ModelID: "test-image", Prompt: "Change the dialogue.", Source: &source, References: []AssetRef{{Kind: "project", Path: "assets/portrait.png"}}, Selection: &imagegen.ImageRegion{X: .1, Y: .2, Width: .5, Height: .5}}
	var models []ImageModel
	if err := json.Unmarshal(call("GET", "/images/models", nil), &models); err != nil || len(models) != 1 || models[0].ID != "test-image" {
		t.Fatalf("image catalog: %+v %v", models, err)
	}
	invalid := input
	invalid.ModelID = "deleted-profile"
	if status, _ := testRequest(t, opened.Connection, "POST", "/images/generations", "", invalid); status < 400 || host.calls.Load() != 0 {
		t.Fatal("unavailable model reached provider")
	}
	call("POST", "/images/generations", input)
	result := awaitImage(t, opened.Connection, input.CommandID)
	if result.Status != "completed" || result.ModelID != "test-image" {
		t.Fatalf("generation: %+v", result)
	}
	forwarded := <-host.requests
	if len(forwarded.Inputs) != 2 || forwarded.Selection == nil {
		t.Fatalf("image edit inputs omitted: %+v", forwarded)
	}
	call("POST", "/project-content/adopt", map[string]any{"path": "comic/assets/pages/page/candidate.png", "asset": result.Images[0].Asset})
	call("POST", "/project-content/adopt", map[string]any{"path": "comic/assets/pages/page/candidate.png", "asset": result.Images[0].Asset})
	call("POST", "/images/generations", input)
	if host.calls.Load() != 1 {
		t.Fatal("replayed edit called the paid provider again")
	}
	_, layout, err := m.registry.Resolve(project, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"comic/comic.json", "comic/assets/pages/page/source.png", "comic/assets/pages/page/candidate.png"} {
		if _, err := os.Stat(filepath.Join(layout.ContentRoot, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(layout.StoreRoot, "comic")); !os.IsNotExist(err) {
		t.Fatal("comic content leaked into Project Store")
	}
	if err := m.ReleaseConsumer(t.Context(), opened.ID, opened.Connection.ConsumerID); err != nil {
		t.Fatal(err)
	}
	if len(m.RuntimeSnapshots()) != 0 {
		t.Fatal("closed page retained content authority")
	}
	config, err := m.ProjectConfiguration(project)
	if err != nil {
		t.Fatal(err)
	}
	config.Extensions.DisabledPlugins = []string{manifest.ID}
	if _, err := m.SaveProjectConfiguration(t.Context(), project, ProjectConfigurationInput{ExpectedRevision: config.Revision, Extensions: config.Extensions}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PageInstructions(project, manifest.ID, "studio"); err == nil {
		t.Fatal("disabled plugin still supplied page instructions")
	}
}
