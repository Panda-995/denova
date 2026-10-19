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

func TestServiceGenerateSavesShallowInteractiveImageWithJournalProvenance(t *testing.T) {
	workspace := t.TempDir()
	generator := &interactiveFakeGenerator{
		result: imagegen.Result{
			ProfileID:    "default",
			Provider:     "openai",
			Model:        "gpt-image-1",
			Size:         "1024x1024",
			Quality:      "medium",
			OutputFormat: "png",
			Images: []imagegen.Image{{
				Data:          []byte("image-bytes"),
				MIMEType:      "image/png",
				Extension:     "png",
				RevisedPrompt: "revised",
			}},
		},
	}
	service := NewServiceWithGenerator(generator)
	service.now = func() time.Time { return time.Date(2026, 6, 27, 1, 2, 3, 0, time.UTC) }

	result, err := service.GenerateInteractive(context.Background(), &config.Config{}, book.NewService(workspace), InteractiveGenerateRequest{
		StoryID:  "story-one",
		BranchID: "main",
		TurnID:   "turn-1",
		Prompt:   "画出当前回合",
		AltText:  "互动图像",
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if generator.request.Prompt != "画出当前回合" {
		t.Fatalf("prompt = %q", generator.request.Prompt)
	}
	if result.Schema != InteractiveResultSchema || filepath.ToSlash(filepath.Dir(result.ImagePath)) != "assets/game/story-one" {
		t.Fatalf("unexpected result: %#v", result)
	}
	imageBytes, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(result.ImagePath)))
	if err != nil {
		t.Fatalf("read image failed: %v", err)
	}
	if string(imageBytes) != "image-bytes" {
		t.Fatalf("image bytes = %q", string(imageBytes))
	}
	if result.StoryID != "story-one" || result.BranchID != "main" || result.TurnID != "turn-1" || result.RevisedPrompt != "revised" {
		t.Fatalf("journal context lost: %#v", result)
	}
	if result.MetaPath != "" {
		t.Fatalf("journaled generation returned metadata: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "assets/game/story-one/meta.json")); !os.IsNotExist(err) {
		t.Fatalf("redundant directory metadata: %v", err)
	}

	second, err := service.GenerateInteractive(context.Background(), &config.Config{}, book.NewService(workspace), InteractiveGenerateRequest{
		StoryID: "story-two", BranchID: "main", TurnID: "turn-1", Prompt: "Another scene",
	})
	if err != nil || filepath.ToSlash(filepath.Dir(second.ImagePath)) != "assets/game/story-two" {
		t.Fatalf("different stories must own separate directories: %+v %v", second, err)
	}
	if _, err := service.GenerateInteractive(context.Background(), &config.Config{}, book.NewService(workspace), InteractiveGenerateRequest{
		StoryID: "../writing", BranchID: "main", TurnID: "turn-1", Prompt: "Invalid story",
	}); err == nil {
		t.Fatal("story ID must not escape its scene directory")
	}
}

type interactiveFakeGenerator struct {
	request imagegen.GenerateRequest
	result  imagegen.Result
	err     error
}

func (f *interactiveFakeGenerator) Generate(_ context.Context, _ *config.Config, request imagegen.GenerateRequest) (imagegen.Result, error) {
	f.request = request
	return f.result, f.err
}
