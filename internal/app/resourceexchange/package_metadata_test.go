package resourceexchange

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"denova/internal/platform"
)

func TestPackageMetadataSurvivesPreviewInstallAndExport(t *testing.T) {
	ctx := context.Background()
	s := testService(t)
	info := PackageInfo{
		ID: "localized", Name: "中文资源包", Description: "创作素材", Author: "Author", Version: "1.0.0", Locale: "zh-CN",
		Usage: "导入后选用", Compatibility: "需要图像模型", Tags: []string{"writing"}, Cover: "https://example.com/cover.png", UpdatedAt: "2026-09-29",
		Translations: map[string]PackageTranslation{"en-US": {Name: "Creative kit", Description: "Writing resources", Usage: "Select after import", Compatibility: "Image model required"}},
	}
	manifest := Manifest{Format: "denova.resource-pack", SchemaVersion: 1, Package: info, Resources: []Resource{{ID: "image", Kind: "preset.image", Path: "image.json"}}}
	files := map[string][]byte{"denova-pack.json": jsonBytes(t, manifest), "image.json": []byte(`{"name":"Ink","prompt":"Draw in ink"}`)}
	preview, err := s.previewFiles(ctx, Source{Kind: "file", Filename: "localized.zip"}, files)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview.Candidates[0].Package, info) {
		t.Fatal("preview lost package metadata")
	}
	plan, err := s.Plan(ctx, PlanRequest{PreviewID: preview.ID, CandidateID: preview.Candidates[0].ID, Resources: []string{"image"}})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := s.Apply(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := s.loadInstallation(ctx, installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Package, info) {
		t.Fatal("installation lost package metadata")
	}
	request := ExportRequest{InstallationID: installed.ID, Package: loaded.Package, Resources: []LocalRef{installed.Bindings[0].Local}}
	if _, err := s.SaveExportDefinition(ctx, ExportDefinition{ExportRequest: request}); err != nil {
		t.Fatal(err)
	}
	definitions, err := s.ExportDefinitions(ctx)
	if err != nil || len(definitions) != 1 || !reflect.DeepEqual(definitions[0].Package, info) {
		t.Fatal("saved export lost metadata", err)
	}
	raw, err := s.Export(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := platform.ArchiveFiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive) != 2 {
		t.Fatal("export needs only the manifest and payload", len(archive))
	}
	var result Manifest
	if err := json.Unmarshal(archive["denova-pack.json"], &result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Package, info) {
		t.Fatalf("export lost metadata: %+v", result.Package)
	}
	if _, err := s.Preview(ctx, Source{Kind: "file", Filename: "copy.zip"}, raw); err != nil {
		t.Fatal(err)
	}
}
