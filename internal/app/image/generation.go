package imageapp

import (
	"context"
	"fmt"
	"log/slog"

	"denova/internal/assetstore"
	imagegen "denova/internal/image/generation"
)

type GenerateResult struct {
	ProfileID    string             `json:"profile_id"`
	Provider     string             `json:"provider"`
	Model        string             `json:"model"`
	Created      int64              `json:"created,omitempty"`
	Size         string             `json:"size,omitempty"`
	Quality      string             `json:"quality,omitempty"`
	OutputFormat string             `json:"output_format,omitempty"`
	Images       []SavedImage       `json:"images"`
	Failures     []imagegen.Failure `json:"failures,omitempty"`
}

type SavedImage struct {
	Path          string `json:"path"`
	MIMEType      string `json:"mime_type"`
	SizeBytes     int    `json:"size_bytes"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

func (service *Service) Generate(ctx context.Context, request imagegen.GenerateRequest) (GenerateResult, error) {
	runtime, err := service.AcquireRuntime(ctx, "")
	if err != nil {
		return GenerateResult{}, err
	}
	defer runtime.Release()
	result, err := imagegen.NewService().Generate(runtime.Context(), &runtime.Config, request)
	if err != nil {
		return GenerateResult{}, err
	}
	if err := runtime.Context().Err(); err != nil {
		return GenerateResult{}, err
	}
	saved := GenerateResult{
		ProfileID:    result.ProfileID,
		Provider:     result.Provider,
		Model:        result.Model,
		Created:      result.Created,
		Size:         result.Size,
		Quality:      result.Quality,
		OutputFormat: result.OutputFormat,
		Images:       make([]SavedImage, 0, len(result.Images)),
		Failures:     append([]imagegen.Failure(nil), result.Failures...),
	}
	for _, image := range result.Images {
		if image.Extension == "" {
			return GenerateResult{}, fmt.Errorf("cannot save an image with an unknown format")
		}
		relPath := assetstore.NewPath(assetstore.Writing, image.Extension)
		if err := runtime.Context().Err(); err != nil {
			return GenerateResult{}, err
		}
		if err := assetstore.Save(runtime.Context(), runtime.Workspace, assetstore.File{Path: relPath, Data: image.Data, Generation: struct {
			Request       imagegen.GenerateRequest `json:"request"`
			ProfileID     string                   `json:"profile_id"`
			Provider      string                   `json:"provider"`
			Model         string                   `json:"model"`
			RevisedPrompt string                   `json:"revised_prompt,omitempty"`
		}{request, result.ProfileID, result.Provider, result.Model, image.RevisedPrompt}}); err != nil {
			return GenerateResult{}, fmt.Errorf("save generated image: %w", err)
		}
		slog.InfoContext(ctx, fmt.Sprintf("[imagegen] saved image path=%s bytes=%d mime=%s", relPath, len(image.Data), image.MIMEType))
		saved.Images = append(saved.Images, SavedImage{
			Path:          relPath,
			MIMEType:      image.MIMEType,
			SizeBytes:     len(image.Data),
			RevisedPrompt: image.RevisedPrompt,
		})
	}
	return saved, nil
}
