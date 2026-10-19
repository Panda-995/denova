package asset

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"denova/config"
	"denova/internal/book"
	imagegen "denova/internal/image/generation"
)

type illustrationFakeGenerator struct {
	request imagegen.GenerateRequest
	result  imagegen.Result
}

func (g *illustrationFakeGenerator) Generate(_ context.Context, _ *config.Config, request imagegen.GenerateRequest) (imagegen.Result, error) {
	g.request = request
	return g.result, nil
}

func TestGenerateWritesShallowIllustrationWithJournalProvenance(t *testing.T) {
	workspace := t.TempDir()
	bookService := book.NewService(workspace)
	if err := bookService.Create("chapters/ch01.md", "file", "# 第一章\n\n雨夜。"); err != nil {
		t.Fatalf("create chapter: %v", err)
	}
	generator := &illustrationFakeGenerator{result: imagegen.Result{
		ProfileID:    "default",
		Provider:     "openai",
		Model:        "gpt-image-1",
		Size:         "4096x2304",
		Quality:      "high",
		OutputFormat: "png",
		Images: []imagegen.Image{{
			Data:          []byte("fake-png"),
			MIMEType:      "image/png",
			Extension:     "png",
			RevisedPrompt: "revised prompt",
		}},
	}}
	service := NewServiceWithGenerator(generator)
	service.now = func() time.Time { return time.Date(2026, 6, 27, 12, 30, 0, 0, time.UTC) }

	result, err := service.GenerateIllustration(context.Background(), &config.Config{Workspace: workspace}, bookService, IllustrationGenerateRequest{
		ChapterPath: "chapters/ch01.md",
		Prompt:      "rainy alley, cinematic",
		AltText:     "雨夜小巷",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if filepath.ToSlash(filepath.Dir(result.ImagePath)) != "assets/writing" {
		t.Fatalf("image path = %q", result.ImagePath)
	}
	if result.MetaPath != "" {
		t.Fatalf("meta path = %q", result.MetaPath)
	}
	if result.Markdown != "![雨夜小巷]("+result.ImagePath+")" {
		t.Fatalf("markdown = %q", result.Markdown)
	}
	imageBytes, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(result.ImagePath)))
	if err != nil {
		t.Fatalf("read image: %v", err)
	}
	if string(imageBytes) != "fake-png" {
		t.Fatalf("image bytes = %q", string(imageBytes))
	}
	if result.Provider != "openai" || result.RevisedPrompt != "revised prompt" {
		t.Fatalf("journal result lost provenance: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "assets/writing/meta.json")); !os.IsNotExist(err) {
		t.Fatalf("journaled generation created redundant metadata: %v", err)
	}
	if generator.request.N != 1 {
		t.Fatalf("image request should generate one image, got %#v", generator.request)
	}
}
