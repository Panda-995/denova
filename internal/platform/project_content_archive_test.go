package platform

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/project"
)

func TestPluginContentArchivePreservesPortableDataAndRejectsOverwrite(t *testing.T) {
	m, projectID := testManager(t)
	const pluginID = "example.relationship-map"
	directory, err := m.projectContentPath(projectID, pluginID)
	if err != nil {
		t.Fatal(err)
	}
	image := resourcePNG(t)
	digest := sha256.Sum256(image)
	asset := "assets/" + hex.EncodeToString(digest[:]) + ".png"
	document := []byte(`{"format":1,"nodes":[{"id":"person","name":"Ada"}]}`)
	for name, data := range map[string][]byte{"documents/relationships.json": document, asset: image} {
		if err := writeBytes(filepath.Join(directory, filepath.FromSlash(name)), data); err != nil {
			t.Fatal(err)
		}
	}
	var archive bytes.Buffer
	if err := m.ExportProjectPluginContent(projectID, pluginID, &archive); err != nil {
		t.Fatal(err)
	}
	if err := m.ImportProjectPluginContent(projectID, pluginID, archive.Bytes()); err == nil {
		t.Fatal("import overwrote existing author content")
	}
	destination := filepath.Join(m.root, "projects", "Other")
	if err := os.MkdirAll(destination, 0700); err != nil {
		t.Fatal(err)
	}
	record, err := m.registry.Add(destination, project.TypeGeneral, "Other")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := m.registry.EnsureStore(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ImportProjectPluginContent(record.ID, pluginID, archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(layout.StoreRoot, "extensions", pluginID, "content", "documents", "relationships.json"))
	if err != nil || !bytes.Equal(stored, document) {
		t.Fatalf("import changed content: %s %v", stored, err)
	}
	if current, err := m.registry.Get(record.ID); err != nil || current.StoreDirName != record.StoreDirName {
		t.Fatalf("import changed Project identity: %+v %v", current, err)
	}
	if err := m.ImportProjectPluginContent(record.ID, "example.other", archive.Bytes()); err == nil {
		t.Fatal("import accepted another plugin's identity")
	}
	var attack bytes.Buffer
	writer := zip.NewWriter(&attack)
	entry, _ := writer.Create("../config.toml")
	_, _ = entry.Write([]byte("overwrite"))
	_ = writer.Close()
	if err := m.ImportProjectPluginContent(record.ID, pluginID, attack.Bytes()); err == nil {
		t.Fatal("import accepted traversal")
	}
	// Moving the complete data root requires no persistent path rewriting.
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.CopyFS(moved, os.DirFS(m.root)); err != nil {
		t.Fatal(err)
	}
	other := New(moved, project.NewRegistry(moved))
	var exported bytes.Buffer
	if err := other.ExportProjectPluginContent(projectID, pluginID, &exported); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archive.Bytes(), exported.Bytes()) {
		t.Fatal("moving data changed the portable export")
	}
}
