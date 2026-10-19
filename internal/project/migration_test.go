package project

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"denova/internal/agents/conversationjournal"
	productsession "denova/internal/agents/session"
	"denova/internal/assetstore"
	"denova/internal/book/lore"
	bookversions "denova/internal/book/versions"
	"denova/internal/interactive"
	workspacelayout "denova/internal/workspace"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestEnsureStoreCopiesLegacyProjectDataWithoutDeletingSource(t *testing.T) {
	denovaDir := t.TempDir()
	workspace := t.TempDir()
	legacyRoot := workspacelayout.Dir(workspace)
	fixtures := map[string]string{
		filepath.Join(legacyRoot, "sessions", "session.jsonl"): "history\n",
		filepath.Join(legacyRoot, "config.toml"):               "[agent_tools.general]\nshell = false\n",
		filepath.Join(legacyRoot, "changes", "events.jsonl"):   "change\n",
		filepath.Join(legacyRoot, "reviews", "ledger.jsonl"):   "review\n",
		filepath.Join(legacyRoot, "runs", "run.json"):          "{}\n",
		filepath.Join(legacyRoot, "artifacts", "artifact.txt"): "artifact\n",
		filepath.Join(legacyRoot, "automations", "tasks.json"): "{\"tasks\":[]}\n",
	}
	for path, content := range fixtures {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	registry := NewRegistry(denovaDir)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.EnsureStore(record)
	if err != nil {
		t.Fatal(err)
	}
	destinations := map[string]string{
		layout.SessionsDir() + string(filepath.Separator) + "session.jsonl": "history\n",
		layout.ConfigPath(): "[agent_tools.general]\nshell = false\n",
		filepath.Join(layout.ChangesDir(), "events.jsonl"):   "change\n",
		filepath.Join(layout.ReviewsDir(), "ledger.jsonl"):   "review\n",
		filepath.Join(layout.RunsDir(), "run.json"):          "{}\n",
		filepath.Join(layout.ArtifactsDir(), "artifact.txt"): "artifact\n",
		filepath.Join(layout.AutomationsDir(), "tasks.json"): "{\"tasks\":[]}\n",
	}
	for path, want := range destinations {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != want {
			t.Fatalf("migrated state mismatch path=%s data=%q err=%v", path, data, readErr)
		}
	}
	for path, want := range fixtures {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != want {
			t.Fatalf("legacy rollback source changed path=%s data=%q err=%v", path, data, readErr)
		}
	}

	if _, err := registry.EnsureStore(record); err != nil {
		t.Fatalf("migration should be idempotent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(layout.StoreRoot, "migration.json")); err != nil {
		t.Fatalf("migration receipt missing: %v", err)
	}
}

func TestResolveMissingProjectDefersStateMigrationUntilContentReturns(t *testing.T) {
	denovaDir := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "book")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(denovaDir)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(workspace); err != nil {
		t.Fatal(err)
	}

	missing, layout, err := registry.Resolve(record.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Status != StatusMissing {
		t.Fatalf("resolved Project status = %s, want %s", missing.Status, StatusMissing)
	}
	if _, err := os.Stat(layout.StoreRoot); !os.IsNotExist(err) {
		t.Fatalf("missing finalized Project Store migration: %v", err)
	}

	legacySession := workspacelayout.Path(workspace, "sessions", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(legacySession), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacySession, []byte("history\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, restoredLayout, err := registry.Resolve(record.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	migratedSession := filepath.Join(restoredLayout.SessionsDir(), "session.jsonl")
	if data, err := os.ReadFile(migratedSession); err != nil || string(data) != "history\n" {
		t.Fatalf("deferred Project Store migration data=%q err=%v", data, err)
	}
}

func TestResolveMalformedExternalProjectForArchiveDoesNotOpenWorkspace(t *testing.T) {
	denovaDir := t.TempDir()
	const projectID = "project-broken-path"
	registry := NewRegistry(denovaDir)
	if err := registry.saveLocked(registryData{
		Version: registryVersion,
		Projects: []Record{{
			ID:           projectID,
			Type:         TypeBook,
			Name:         "Book",
			StoreDirName: "Book",
			Location: ProjectLocation{
				Kind: LocationExternal,
				Path: `D:\mnt\d\Code\denova\D:\mnt\d\Code\denova\.denova\projects\Book`,
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	record, layout, err := registry.Resolve(projectID, false)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusMissing {
		t.Fatalf("malformed external Project status = %s, want %s", record.Status, StatusMissing)
	}
	if _, err := os.Stat(layout.StoreRoot); !os.IsNotExist(err) {
		t.Fatalf("malformed external Project opened migration state: %v", err)
	}
	archived, err := registry.Archive(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != StatusArchived {
		t.Fatalf("malformed external Project archive status = %s", archived.Status)
	}
}

func TestEnsureStoreRejectsUnsupportedIntermediateReceipt(t *testing.T) {
	denovaDir := t.TempDir()
	workspace := t.TempDir()
	registry := NewRegistry(denovaDir)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.Layout(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.StoreRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.StoreRoot, "migration.json"), []byte("{\"version\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.EnsureStore(record); err == nil || !strings.Contains(err.Error(), "does not match supported version") {
		t.Fatalf("unsupported receipt error = %v", err)
	}
}

func TestEnsureStoreMigratesReleasedReceiptWithoutRecopyingData(t *testing.T) {
	denovaDir := t.TempDir()
	workspace := t.TempDir()
	registry := NewRegistry(denovaDir)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.Layout(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.SessionsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	currentSession := filepath.Join(layout.SessionsDir(), "current.jsonl")
	if err := os.WriteFile(currentSession, []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	completedAt := time.Date(2026, time.August, 1, 2, 3, 4, 0, time.UTC)
	released := struct {
		Version     int       `json:"version"`
		Source      string    `json:"source"`
		CompletedAt time.Time `json:"completed_at"`
		Copied      []string  `json:"copied"`
	}{
		Version: 3, Source: workspace,
		CompletedAt: completedAt, Copied: []string{"sessions"},
	}
	raw, err := json.MarshalIndent(released, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(layout.StoreRoot, "migration.json")
	if err := os.WriteFile(receiptPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := registry.EnsureStore(record); err != nil {
		t.Fatal(err)
	}
	if session, err := os.ReadFile(currentSession); err != nil || string(session) != "current\n" {
		t.Fatalf("completed Store migration was repeated: data=%q err=%v", session, err)
	}
	migratedRaw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(migratedRaw), `"source"`) || strings.Contains(string(migratedRaw), workspace) {
		t.Fatalf("migrated receipt retained its runtime source: %s", migratedRaw)
	}
	var migrated migrationReceipt
	if err := json.Unmarshal(migratedRaw, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Version != storeMigrationVersion || !migrated.CompletedAt.Equal(completedAt) ||
		len(migrated.Copied) != 1 || migrated.Copied[0] != "sessions" {
		t.Fatalf("migrated receipt = %#v", migrated)
	}
}

func TestRegistryDefersReleasedReceiptsUntilStoreIsUsed(t *testing.T) {
	denovaDir := t.TempDir()
	registry := NewRegistry(denovaDir)
	workspaces := []string{t.TempDir(), t.TempDir()}
	records := make([]Record, 0, len(workspaces))
	for _, workspace := range workspaces {
		record, err := registry.Add(workspace, TypeGeneral, "Project")
		if err != nil {
			t.Fatal(err)
		}
		layout, err := registry.Layout(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(layout.StoreRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		released := struct {
			Version int    `json:"version"`
			Source  string `json:"source"`
		}{Version: 3, Source: workspace}
		raw, err := json.Marshal(released)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(layout.StoreRoot, "migration.json"), append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := os.Remove(workspaces[1]); err != nil {
		t.Fatal(err)
	}

	projects, err := NewRegistry(denovaDir).List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != len(records) {
		t.Fatalf("migrated Projects = %#v", projects)
	}
	statusByID := make(map[string]Status, len(projects))
	for _, project := range projects {
		statusByID[project.ID] = project.Status
	}
	if statusByID[records[0].ID] != StatusAvailable || statusByID[records[1].ID] != StatusMissing {
		t.Fatalf("migrated Project statuses = %#v", statusByID)
	}
	// Listing a missing or unused Project must not mutate its Store.
	for _, record := range records {
		receiptPath := filepath.Join(denovaDir, storeDirectoryName, record.StoreDirName, "migration.json")
		raw, err := os.ReadFile(receiptPath)
		if err != nil || !strings.Contains(string(raw), `"source"`) {
			t.Fatalf("listing migrated an unused Store: %s %v", raw, err)
		}
	}
	for _, record := range records {
		if _, err := registry.EnsureStore(record); err != nil {
			t.Fatal(err)
		}
	}

	for _, record := range records {
		receiptPath := filepath.Join(denovaDir, storeDirectoryName, record.StoreDirName, "migration.json")
		raw, err := os.ReadFile(receiptPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"source"`) {
			t.Fatalf("Project %s receipt was not upgraded: %s", record.ID, raw)
		}
		var receipt migrationReceipt
		if err := json.Unmarshal(raw, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Version != storeMigrationVersion {
			t.Fatalf("Project %s receipt version = %d", record.ID, receipt.Version)
		}
	}
}

func TestEnsureStoreMigratesReleasedVersionRepositoryWithoutDeletingSource(t *testing.T) {
	denovaDir := t.TempDir()
	workspace := t.TempDir()
	chapter := filepath.Join(workspace, "chapters", "ch0001.md")
	if err := os.MkdirAll(filepath.Dir(chapter), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chapter, []byte("released content"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyRepository := filepath.Join(workspace, ".git")
	legacy := bookversions.NewService(workspace, legacyRepository)
	created, err := legacy.Create("released version", bookversions.VersionSourceManual, bookversions.DefaultAutoSettings())
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	registry := NewRegistry(denovaDir)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.EnsureStore(record)
	if err != nil {
		t.Fatal(err)
	}
	migrated := bookversions.NewService(workspace, layout.VersionRepositoryDir())
	history, err := migrated.History(10)
	if err != nil || len(history) != 1 || history[0].ID != created.Version.ID {
		t.Fatalf("migrated Project history=%#v err=%v", history, err)
	}
	if _, err := os.Stat(legacyRepository); err != nil {
		t.Fatalf("released version source must remain available: %v", err)
	}
	receipt, err := os.ReadFile(filepath.Join(layout.StoreRoot, "migration.json"))
	if err != nil || !strings.Contains(string(receipt), `"versions"`) {
		t.Fatalf("version migration receipt=%q err=%v", receipt, err)
	}
}

func TestEnsureStoreIsIdempotentForConcurrentProjectResolution(t *testing.T) {
	denovaDir := t.TempDir()
	workspace := t.TempDir()
	legacyChanges := workspacelayout.Path(workspace, "changes")
	if err := os.MkdirAll(legacyChanges, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyChanges, "events.jsonl"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := NewRegistry(denovaDir)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}

	const callers = 16
	start := make(chan struct{})
	errorsByCaller := make([]error, callers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)
	for index := range callers {
		go func() {
			defer waitGroup.Done()
			<-start
			_, errorsByCaller[index] = registry.EnsureStore(record)
		}()
	}
	close(start)
	waitGroup.Wait()
	for index, callErr := range errorsByCaller {
		if callErr != nil {
			t.Fatalf("concurrent Store migration %d failed: %v", index, callErr)
		}
	}

	layout, err := registry.Layout(record)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(layout.ChangesDir(), "events.jsonl"))
	if err != nil || string(data) != "change\n" {
		t.Fatalf("migrated state mismatch data=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(layout.StoreRoot, "migration.json")); err != nil {
		t.Fatalf("migration receipt missing: %v", err)
	}
}

func TestEnsureStoreRejectsLegacySymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires optional Windows symlink privileges")
	}
	denovaDir := t.TempDir()
	workspace := t.TempDir()
	legacySessions := workspacelayout.Path(workspace, "sessions")
	if err := os.MkdirAll(filepath.Dir(legacySessions), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), legacySessions); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(denovaDir)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.EnsureStore(record); err == nil {
		t.Fatal("expected legacy state symlink migration to fail")
	}
}

func TestShallowAssetsMigrationPreservesAttributesJournalsAndVersionRestore(t *testing.T) {
	workspace, denovaDir := t.TempDir(), t.TempDir()
	registry := NewRegistry(denovaDir)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.Layout(record)
	if err != nil {
		t.Fatal(err)
	}
	const oldImage = "assets/lore/media/asset_original/file.png"
	const oldMeta = "assets/lore/media/asset_original/meta.json"
	const oldGame = "assets/interactive/images/story/main/turn/run/image.png"
	const oldWriting = "assets/illustrations/ch01/run/image.png"
	const oldCover = "assets/image/cover.png"
	const oldCoverSource = "assets/image/covers/run/cover.png"
	const oldCoverMeta = "assets/image/covers/run/meta.json"
	original := lore.Asset{ID: "asset_original", Path: oldImage, OriginalName: "Hero portrait.png", MIMEType: "image/png", SizeBytes: 5, CreatedAt: "2026-09-28T00:00:00Z", Source: lore.AssetSource{Kind: "generated", MetaPath: oldMeta}}
	collection := lore.Collection{Version: 2, Assets: []lore.Asset{original}, Items: []lore.Item{{ID: "hero", Name: "Hero", Content: "Keep lore", Materials: &lore.Materials{Entries: []lore.MaterialEntry{{AssetID: original.ID, Name: "Portrait", Description: "Keep description"}}, CoverAssetID: original.ID}}}}
	items, err := json.Marshal(collection)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		lore.ItemsRelativePath: items, oldImage: []byte("image"), oldMeta: []byte(`{"prompt":"Draw hero","provider":"test","image_path":"` + oldImage + `","mime_type":"image/png","size_bytes":5}`),
		oldCoverSource: []byte("cover source"), oldCoverMeta: []byte(`{"prompt":"Book cover","image_path":"` + oldCoverSource + `"}`),
		oldGame: []byte("game"), oldWriting: []byte("writing"), oldCover: []byte("cover"),
		"chapters/ch01.md": []byte("![Scene](" + oldWriting + ")\n[Remote](https://example.com/" + oldWriting + ")"),
	}
	for name, data := range files {
		absolute := filepath.Join(workspace, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	game := interactive.NewStore(workspace)
	story, err := game.CreateStory(interactive.CreateStoryRequest{Title: "Asset migration", StoryTellerID: "classic", Origin: "![Scene](" + oldGame + ")"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := game.AppendTurn(story.ID, interactive.AppendTurnRequest{BranchID: "main", User: "Continue", Narrative: "![Scene](" + oldGame + ")"}); err != nil {
		t.Fatal(err)
	}
	if err := game.Close(); err != nil {
		t.Fatal(err)
	}
	storyPath := "interactive/story/story-" + story.ID + ".jsonl"
	files[storyPath], err = os.ReadFile(filepath.Join(workspace, storyPath))
	if err != nil {
		t.Fatal(err)
	}
	versions := bookversions.NewService(workspace, layout.VersionRepositoryDir())
	defer versions.Close()
	released, err := versions.Create("released assets", bookversions.VersionSourceManual, bookversions.DefaultAutoSettings())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := productsession.NewStore(layout.SessionsDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := sessions.GetOrCreate("asset-migration")
	if err != nil {
		t.Fatal(err)
	}
	remote := "https://example.com/" + oldImage
	toolOutput := `{"image_path":"` + oldImage + `","remote_url":"` + remote + `"}`
	for _, msg := range []*agentschema.Message{agentschema.UserMessage("![Hero](" + oldImage + ")"), {Role: agentschema.ToolRole, Content: toolOutput, ToolCallID: "call", ToolName: "generate_image"}, agentschema.AssistantMessage("Done", nil)} {
		if err := session.Append(msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := sessions.Close(); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(layout.SessionsDir(), "asset-migration.jsonl")
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.EnsureStore(record); err != nil {
		t.Fatal(err)
	}
	store := lore.NewStore(workspace)
	item, err := store.ReadAny("hero")
	if err != nil || len(item.ResolvedMaterials) != 1 {
		t.Fatalf("migrated item: %+v %v", item, err)
	}
	actual := item.ResolvedMaterials[0].Asset
	expected := original
	expected.Path, expected.Source.MetaPath = actual.Path, actual.Source.MetaPath
	if !reflect.DeepEqual(actual, expected) || filepath.ToSlash(filepath.Dir(actual.Path)) != "assets/lore" || actual.Source.MetaPath != "assets/lore/meta.json" || item.Content != "Keep lore" || item.Materials.CoverAssetID != original.ID {
		t.Fatalf("asset properties or references changed: %+v", item)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, lore.ItemsRelativePath))
	if err != nil {
		t.Fatal(err)
	}
	var persisted lore.Collection
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Version != 2 || len(persisted.Assets) != 1 || !reflect.DeepEqual(persisted.Assets[0], actual) {
		t.Fatalf("attributes left items.json: %+v", persisted)
	}
	for _, old := range []string{oldImage, oldGame, oldWriting, oldCover} {
		if _, err := os.Stat(filepath.Join(workspace, old)); !os.IsNotExist(err) {
			t.Fatalf("nested asset survived migration: %s %v", old, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(workspace, actual.Path)); err != nil || string(data) != "image" {
		t.Fatalf("image lost: %q %v", data, err)
	}
	metadataRaw, err := os.ReadFile(filepath.Join(workspace, actual.Source.MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := assetstore.DecodeMetadata(metadataRaw)
	if err != nil || !strings.Contains(string(metadata.Files[filepath.Base(actual.Path)]), "Draw hero") || bytes.Contains(metadataRaw, []byte("mime_type")) {
		t.Fatalf("wrong generation fallback: %s %v", metadataRaw, err)
	}
	coverData, err := os.ReadFile(filepath.Join(workspace, assetstore.CoverPath))
	if err != nil || string(coverData) != "cover" {
		t.Fatalf("display cover did not migrate into its scene: %q %v", coverData, err)
	}
	coverMeta, err := os.ReadFile(filepath.Join(workspace, "assets/covers/meta.json"))
	if err != nil || !bytes.Contains(coverMeta, []byte("Book cover")) {
		t.Fatalf("cover provenance did not migrate into its scene: %s %v", coverMeta, err)
	}
	chapter, err := os.ReadFile(filepath.Join(workspace, "chapters/ch01.md"))
	if err != nil || !strings.Contains(string(chapter), "https://example.com/"+oldWriting) {
		t.Fatalf("remote reference was rewritten: %q %v", chapter, err)
	}
	backupRoot := filepath.Join(layout.StoreRoot, "versions", "asset-layout-v1")
	for name, expectedBytes := range files {
		got, err := os.ReadFile(filepath.Join(backupRoot, "content", filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, expectedBytes) {
			t.Fatalf("original backup changed: %s %v", name, err)
		}
	}
	backupJournal, err := os.ReadFile(filepath.Join(backupRoot, "store", "sessions", "asset-migration.jsonl"))
	if err != nil || !bytes.Equal(backupJournal, beforeJournal) {
		t.Fatal("journal rollback backup lost", err)
	}
	if _, err := os.Stat(conversationjournal.SidecarPath(journalPath)); !os.IsNotExist(err) {
		t.Fatal("stale journal index kept", err)
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err = productsession.NewStore(layout.SessionsDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sessions.Close()
	session, err = sessions.Get("asset-migration")
	if err != nil {
		t.Fatalf("canonical checksum chain broke: %v", err)
	}
	messages := session.GetMessages()
	if len(messages) != 3 || messages[0].Content != "![Hero]("+actual.Path+")" || !strings.Contains(messages[1].Content, actual.Path) || !strings.Contains(messages[1].Content, remote) || messages[2].Content != "Done" {
		t.Fatalf("journal projection changed: %+v", messages)
	}
	gameSnapshot, err := game.Snapshot(story.ID, "main")
	if err != nil || len(gameSnapshot.Turns) != 1 || !strings.HasPrefix(gameSnapshot.Turns[0].Narrative, "![Scene](assets/game/story/") {
		t.Fatalf("game journal migration failed: %+v %v", gameSnapshot, err)
	}
	if err := game.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.EnsureStore(record); err != nil {
		t.Fatal(err)
	}
	retriedJournal, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(retriedJournal, afterJournal) {
		t.Fatal("retry rewrote canonical journal", err)
	}
	gameDirectory, err := assetstore.GameDirectory("new-story")
	if err != nil {
		t.Fatal(err)
	}
	newer := assetstore.NewPath(gameDirectory, "png")
	if err := assetstore.Save(context.Background(), workspace, assetstore.File{Path: newer, Data: []byte("new scene")}); err != nil {
		t.Fatal(err)
	}
	if _, err := versions.Restore(released.Version.ID, bookversions.DefaultAutoSettings()); err != nil {
		t.Fatal(err)
	}
	restored, err := store.ReadAny("hero")
	if err != nil || restored.ResolvedMaterials[0].Path != actual.Path || restored.ResolvedMaterials[0].ID != original.ID {
		t.Fatalf("old version changed migrated identity: %+v %v", restored, err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, newer)); err != nil || string(data) != "new scene" {
		t.Fatal("restore lost newer game image", err)
	}
	restoredJournal, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(restoredJournal, afterJournal) {
		t.Fatal("restore rewrote an active journal", err)
	}
	if err := session.Append(agentschema.UserMessage("After restore")); err != nil {
		t.Fatalf("journal cannot append after migration and restore: %v", err)
	}
	restoredGame, err := game.Snapshot(story.ID, "main")
	if err != nil || len(restoredGame.Turns) != 1 || restoredGame.Turns[0].Narrative != gameSnapshot.Turns[0].Narrative {
		t.Fatalf("old version lost migrated game history: %+v %v", restoredGame, err)
	}
	if _, err := game.AppendTurn(story.ID, interactive.AppendTurnRequest{BranchID: "main", User: "Continue", Narrative: "After restore"}); err != nil {
		t.Fatalf("game cannot append after migration and restore: %v", err)
	}
	if err := game.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, actual.Path)); err != nil {
		t.Fatal(err)
	}
	if _, err := versions.RestoreWithPaths(released.Version.ID, []string{lore.ItemsRelativePath}, bookversions.DefaultAutoSettings()); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, actual.Path)); err != nil || string(data) != "image" {
		t.Fatal("selective old-version restore lost dependency", err)
	}
}
