package asset

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"denova/config"
	"denova/internal/assetstore"
	"denova/internal/book"
	"denova/internal/book/lore"
	imagegen "denova/internal/image/generation"
)

func TestGenerateSavesLoreImageAndMetadata(t *testing.T) {
	workspace := t.TempDir()
	generator := &loreFakeGenerator{result: imagegen.Result{
		ProfileID:    "default",
		Provider:     "openai",
		Model:        "gpt-image-1",
		Size:         "2048x2048",
		OutputFormat: "png",
		Images:       []imagegen.Image{{Data: []byte("image"), Extension: "png", MIMEType: "image/png", RevisedPrompt: "revised"}},
	}}
	service := NewServiceWithGenerator(generator)
	service.now = func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) }

	result, err := service.GenerateLore(context.Background(), &config.Config{}, book.NewService(workspace), LoreGenerateRequest{
		Provenance: ProvenanceDirectory,
		Item: lore.Item{
			ID:               "hero",
			Type:             "character",
			Name:             "林川",
			Tags:             []string{"主角"},
			BriefDescription: "角色 林川。谨慎。",
			Content:          "## 林川\n\n谨慎而疲惫。",
		},
		Instruction:       "夜色氛围",
		ImagePresetID:     "game-cg",
		ImagePresetPrompt: "电影感光影",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Schema != LoreResultSchema || filepath.ToSlash(filepath.Dir(result.ImagePath)) != "assets/lore" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.MetaPath != "assets/lore/meta.json" || result.ImagePresetID != "game-cg" {
		t.Fatalf("unexpected metadata paths: %#v", result)
	}
	assertFile(t, workspace, result.ImagePath, "image")
	meta, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(result.MetaPath)))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := assetstore.DecodeMetadata(meta)
	if err != nil {
		t.Fatal(err)
	}
	var detail generationMeta
	if err := json.Unmarshal(metadata.Files[filepath.Base(result.ImagePath)], &detail); err != nil {
		t.Fatal(err)
	}
	if detail.ImagePresetID != "game-cg" || detail.Prompt != generator.request.Prompt || detail.RevisedPrompt != "revised" {
		t.Fatalf("missing generation details: %+v", detail)
	}
	for _, forbidden := range []string{`"mime_type"`, `"size_bytes"`, `"item_id"`, `"image_path"`} {
		if strings.Contains(string(meta), forbidden) {
			t.Fatalf("directory metadata duplicates product attributes: %s", meta)
		}
	}
	second, err := service.GenerateLore(context.Background(), &config.Config{}, book.NewService(workspace), LoreGenerateRequest{Provenance: ProvenanceDirectory, Item: lore.Item{ID: "other", Name: "Other"}, Prompt: "second prompt"})
	if err != nil {
		t.Fatal(err)
	}
	meta, err = os.ReadFile(filepath.Join(workspace, filepath.FromSlash(second.MetaPath)))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err = assetstore.DecodeMetadata(meta)
	if err != nil || len(metadata.Files) != 2 || len(metadata.Files[filepath.Base(result.ImagePath)]) == 0 {
		t.Fatalf("generation overwrote another file's provenance: %+v %v", metadata, err)
	}
	if !strings.Contains(detail.Prompt, "电影感光影") || !strings.Contains(detail.Prompt, "夜色氛围") || !strings.Contains(detail.Prompt, "林川") {
		t.Fatalf("prompt missing expected context:\n%s", generator.request.Prompt)
	}
}

func TestBuildLorePromptBoundsLoreContent(t *testing.T) {
	prompt := BuildLorePrompt(LoreGenerateRequest{
		Item: lore.Item{
			ID:               "rule",
			Type:             "world",
			Name:             "长规则",
			BriefDescription: strings.Repeat("简介", 1000),
			Content:          strings.Repeat("正文", 5000),
		},
		Instruction:       strings.Repeat("要求", 1000),
		ImagePresetPrompt: strings.Repeat("风格", 3000),
	})
	if len([]rune(prompt)) > maxPresetChars+maxBriefChars+maxContentChars+maxInstructionChars+600 {
		t.Fatalf("prompt is not bounded, runes=%d", len([]rune(prompt)))
	}
	if !strings.Contains(prompt, "Lore type: world") || !strings.Contains(prompt, "Lore name: 长规则") {
		t.Fatalf("prompt missing lore identity:\n%s", prompt)
	}
}

func TestBuildLorePromptPreservesCustomFinalPrompt(t *testing.T) {
	const prompt = "masterpiece, 1girl, portrait, rim lighting"
	got := BuildLorePrompt(LoreGenerateRequest{
		Prompt: prompt, Item: lore.Item{ID: "hero", Name: "林川"},
		ImagePresetPrompt: "must not be appended", Instruction: "must not be appended",
	})
	if got != prompt {
		t.Fatalf("custom prompt changed: %q", got)
	}
}

func TestGenerateStopsBeforeWritingWhenContextCanceledAfterModel(t *testing.T) {
	workspace := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	generator := &loreFakeGenerator{
		cancel: cancel,
		result: imagegen.Result{
			ProfileID:    "default",
			Provider:     "openai",
			Model:        "gpt-image-1",
			Size:         "2048x2048",
			OutputFormat: "png",
			Images:       []imagegen.Image{{Data: []byte("image"), Extension: "png", MIMEType: "image/png"}},
		},
	}
	service := NewServiceWithGenerator(generator)

	_, err := service.GenerateLore(ctx, &config.Config{}, book.NewService(workspace), LoreGenerateRequest{
		Item: lore.Item{
			ID:      "hero",
			Type:    "character",
			Name:    "林川",
			Content: "谨慎。",
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate error = %v, want context canceled", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "assets")); !os.IsNotExist(err) {
		t.Fatalf("assets should not be written after cancellation, err=%v", err)
	}
}

type loreFakeGenerator struct {
	request imagegen.GenerateRequest
	result  imagegen.Result
	err     error
	cancel  context.CancelFunc
}

func (f *loreFakeGenerator) Generate(ctx context.Context, cfg *config.Config, request imagegen.GenerateRequest) (imagegen.Result, error) {
	f.request = request
	if f.cancel != nil {
		f.cancel()
	}
	if f.err != nil {
		return imagegen.Result{}, f.err
	}
	return f.result, nil
}

func assertFile(t *testing.T, workspace, relPath, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", relPath, string(data), want)
	}
}

func loreTestPNGBytes() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
		0xae, 0x42, 0x60, 0x82,
	}
}
