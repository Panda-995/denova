package asset

import (
	"context"
	"fmt"
	"strings"
	"time"

	"denova/config"
	"denova/internal/assetstore"
	"denova/internal/book"
	imagegen "denova/internal/image/generation"
)

const (
	InteractiveResultSchema = "interactive_image.v1"
)

type InteractiveGenerateRequest struct {
	StoryID      string
	BranchID     string
	TurnID       string
	Prompt       string
	AltText      string
	ProfileID    string
	Size         string
	AspectRatio  string
	Resolution   string
	Quality      string
	OutputFormat string
}

type InteractiveResult struct {
	Schema       string `json:"schema"`
	StoryID      string `json:"story_id"`
	BranchID     string `json:"branch_id"`
	TurnID       string `json:"turn_id"`
	ImagePath    string `json:"image_path"`
	MetaPath     string `json:"meta_path,omitempty"`
	AltText      string `json:"alt_text,omitempty"`
	ProfileID    string `json:"profile_id"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`

	RevisedPrompt string `json:"revised_prompt,omitempty"`
	MIMEType      string `json:"mime_type,omitempty"`
	SizeBytes     int    `json:"size_bytes,omitempty"`
}

func (s *Service) GenerateInteractive(ctx context.Context, cfg *config.Config, bookService *book.Service, request InteractiveGenerateRequest) (InteractiveResult, error) {
	if s == nil {
		s = NewService()
	}
	if s.generator == nil {
		s.generator = imagegen.NewService()
	}
	if cfg == nil {
		return InteractiveResult{}, fmt.Errorf("运行配置不可用")
	}
	if bookService == nil || strings.TrimSpace(bookService.Workspace()) == "" {
		return InteractiveResult{}, fmt.Errorf("workspace 不可用")
	}
	storyID := strings.TrimSpace(request.StoryID)
	branchID := strings.TrimSpace(request.BranchID)
	turnID := strings.TrimSpace(request.TurnID)
	if storyID == "" || branchID == "" || turnID == "" {
		return InteractiveResult{}, fmt.Errorf("互动图像缺少 story_id、branch_id 或 turn_id")
	}
	directory, err := assetstore.GameDirectory(storyID)
	if err != nil {
		return InteractiveResult{}, err
	}
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return InteractiveResult{}, imagegen.ErrPromptRequired
	}
	generated, err := s.generator.Generate(ctx, cfg, imagegen.GenerateRequest{
		ProfileID:    strings.TrimSpace(request.ProfileID),
		Prompt:       prompt,
		N:            1,
		Size:         strings.TrimSpace(request.Size),
		AspectRatio:  strings.TrimSpace(request.AspectRatio),
		Resolution:   strings.TrimSpace(request.Resolution),
		Quality:      strings.TrimSpace(request.Quality),
		OutputFormat: strings.TrimSpace(request.OutputFormat),
	})
	if err != nil {
		return InteractiveResult{}, err
	}
	if len(generated.Images) == 0 {
		return InteractiveResult{}, fmt.Errorf("图像模型未返回图像")
	}
	image := generated.Images[0]
	if len(image.Data) == 0 {
		return InteractiveResult{}, fmt.Errorf("图像模型返回了空图像")
	}
	ext := normalizeImageExtension(image.Extension, generated.OutputFormat, request.OutputFormat)
	if ext == "" {
		return InteractiveResult{}, fmt.Errorf("无法识别图像格式")
	}

	createdAt := s.now().UTC()
	imagePath := assetstore.NewPath(directory, ext)

	result := InteractiveResult{
		Schema:        InteractiveResultSchema,
		StoryID:       storyID,
		BranchID:      branchID,
		TurnID:        turnID,
		ImagePath:     imagePath,
		AltText:       strings.TrimSpace(request.AltText),
		ProfileID:     generated.ProfileID,
		Provider:      generated.Provider,
		Model:         generated.Model,
		Size:          generated.Size,
		Quality:       generated.Quality,
		OutputFormat:  firstNonEmpty(generated.OutputFormat, ext),
		CreatedAt:     createdAt.Format(time.RFC3339),
		RevisedPrompt: image.RevisedPrompt,
		MIMEType:      image.MIMEType,
		SizeBytes:     len(image.Data),
	}

	if err := assetstore.Save(ctx, bookService.Workspace(), assetstore.File{Path: imagePath, Data: image.Data}); err != nil {
		return InteractiveResult{}, fmt.Errorf("save game image: %w", err)
	}
	return result, nil
}
