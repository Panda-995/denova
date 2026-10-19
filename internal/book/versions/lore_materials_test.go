package versions

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"denova/internal/assetstore"
	"denova/internal/book/lore"
)

func TestLoreRestoreRetainsNewMediaAndRestoresDependencies(t *testing.T) {
	workspace := t.TempDir()
	service := newVersionTestService(t, workspace)
	defer service.Close()
	store := lore.NewStore(workspace)
	old := &lore.Image{ImagePath: "assets/lore/old--asset_old.png", MetaPath: "assets/lore/meta.json"}
	if _, err := store.Create(lore.ItemInput{ID: "hero", Name: "Hero", Image: old}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, old.ImagePath, "old-image")
	writeFile(t, workspace, old.MetaPath, `{"version":1,"files":{"old--asset_old.png":{"prompt":"old prompt"}}}`)
	first, err := service.Create("legacy", VersionSourceManual, DefaultAutoSettings())
	if err != nil {
		t.Fatal(err)
	}
	asset := lore.Asset{ID: "asset_new", Path: "assets/lore/new--asset_new.wav", MIMEType: "audio/wav", Source: lore.AssetSource{Kind: "upload"}}
	writeFile(t, workspace, asset.Path, "new-audio")
	if _, err := store.AttachAsset("hero", asset, lore.MaterialEntry{Description: "Ambient reference"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, old.MetaPath, `{"version":1,"files":{"old--asset_old.png":{"prompt":"old prompt"},"new--asset_new.wav":{"request":"new source"}}}`)
	writeFile(t, workspace, "assets/game/story-one/new.png", "new game image")
	writeFile(t, workspace, "assets/writing/new--asset_writing.png", "new illustration")
	writeFile(t, workspace, assetstore.CoverPath, "mutable cover")
	writeFile(t, workspace, "chapters/new.md", "new chapter")
	if _, err := service.Create("new media", VersionSourceManual, DefaultAutoSettings()); err != nil {
		t.Fatal(err)
	}
	plan, err := service.RestorePlan(first.Version.ID, nil, DefaultAutoSettings())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.RetainedMedia, asset.Path) {
		t.Fatalf("retained file not disclosed: %+v", plan)
	}
	result, err := service.Restore(first.Version.ID, DefaultAutoSettings())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == nil || result.Status.Clean {
		t.Fatal("retained media must be reported as outside target")
	}
	got, err := os.ReadFile(filepath.Join(workspace, asset.Path))
	if err != nil || string(got) != "new-audio" {
		t.Fatal("history asset lost", err)
	}
	for _, name := range []string{"assets/game/story-one/new.png", "assets/writing/new--asset_writing.png"} {
		if _, err := os.Stat(filepath.Join(workspace, name)); err != nil {
			t.Fatalf("newer creative media lost: %s %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, assetstore.CoverPath)); !os.IsNotExist(err) {
		t.Fatalf("mutable cover was incorrectly retained: %v", err)
	}
	metadata, err := os.ReadFile(filepath.Join(workspace, old.MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := assetstore.DecodeMetadata(metadata)
	if err != nil || len(decoded.Files) != 2 {
		t.Fatalf("restore lost newer generation details: %+v %v", decoded, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "chapters/new.md")); !os.IsNotExist(err) {
		t.Fatal("non-media restore behavior changed", err)
	}
	item, err := store.ReadAny("hero")
	if err != nil || item.Materials != nil || len(item.ResolvedMaterials) != 1 {
		t.Fatal("legacy restore", item, err)
	}
	if err := os.Remove(filepath.Join(workspace, old.ImagePath)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, old.MetaPath, `{"version":1,"files":{"new--asset_new.wav":{"request":"new source"}}}`)
	plan, err = service.RestorePlan(first.Version.ID, []string{lore.ItemsRelativePath}, DefaultAutoSettings())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.Paths, old.ImagePath) || !slices.Contains(plan.Paths, old.MetaPath) {
		t.Fatal("dependencies absent", plan)
	}
	if _, err := service.RestoreWithPaths(first.Version.ID, []string{lore.ItemsRelativePath}, DefaultAutoSettings()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(workspace, old.ImagePath)); err != nil || string(got) != "old-image" {
		t.Fatal("dependency not restored", err)
	}
	metadata, err = os.ReadFile(filepath.Join(workspace, old.MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = assetstore.DecodeMetadata(metadata)
	if err != nil || len(decoded.Files) != 2 {
		t.Fatalf("selective restore lost newer generation details: %+v %v", decoded, err)
	}

}

func TestLoreRestoreRemoteCoverKeepsURLWithoutFileDependencies(t *testing.T) {
	workspace := t.TempDir()
	service := newVersionTestService(t, workspace)
	defer service.Close()
	store := lore.NewStore(workspace)
	if _, err := store.Create(lore.ItemInput{ID: "hero", Name: "Hero"}); err != nil {
		t.Fatal(err)
	}
	item, err := store.RemoteMaterial(context.Background(), "hero", lore.MaterialMutation{Op: "remote", URL: "https://unreachable.invalid/old.png"})
	if err != nil {
		t.Fatal(err)
	}
	id := item.ResolvedMaterials[0].ID
	if _, err = store.MutateMaterial("hero", lore.MaterialMutation{Op: "cover", AssetID: id}); err != nil {
		t.Fatal(err)
	}
	first, err := service.Create("remote cover", VersionSourceManual, DefaultAutoSettings())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RemoteMaterial(context.Background(), "hero", lore.MaterialMutation{Op: "remote", AssetID: id, URL: "https://unreachable.invalid/new.png"}); err != nil {
		t.Fatal(err)
	}
	plan, err := service.RestorePlan(first.Version.ID, []string{lore.ItemsRelativePath}, DefaultAutoSettings())
	if err != nil || len(plan.Paths) != 1 || plan.Paths[0] != lore.ItemsRelativePath {
		t.Fatal(plan, err)
	}
	if _, err := service.RestoreWithPaths(first.Version.ID, []string{lore.ItemsRelativePath}, DefaultAutoSettings()); err != nil {
		t.Fatal(err)
	}
	restored, err := store.ReadAny("hero")
	if err != nil || restored.Image.ImageURL != "https://unreachable.invalid/old.png" || restored.Materials.CoverAssetID != id {
		t.Fatal(restored, err)
	}
}
