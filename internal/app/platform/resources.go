package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"denova/internal/app/projectfiles"
	appsettings "denova/internal/app/settings"
	booklore "denova/internal/book/lore"
	imagegen "denova/internal/image/generation"
	"denova/internal/platform"
	"denova/internal/portablepath"
)

func (host Resources) LibraryItems(ctx context.Context, projectID string) ([]platform.LibraryItem, error) {
	result := []platform.LibraryItem{}
	_, err := host.host.WithLoreStore(ctx, projectID, func(store *booklore.Store) error {
		items, err := store.ListAll()
		if err != nil {
			return err
		}
		for _, item := range items {
			entry := platform.LibraryItem{
				ID: item.ID, Type: item.Type, Name: item.Name, Enabled: item.Enabled,
				Tags: item.Tags, BriefDescription: item.BriefDescription, Keywords: item.Keywords,
				Content: item.Content, UpdatedAt: item.UpdatedAt,
			}
			if item.Image != nil && strings.HasPrefix(item.Image.ImagePath, "assets/") && portablepath.Validate(item.Image.ImagePath) == nil {
				entry.Image = &platform.AssetRef{Kind: "project", Path: item.Image.ImagePath}
			}
			result = append(result, entry)
		}
		return ctx.Err()
	})
	return result, err
}

func (host Resources) ReadAsset(ctx context.Context, projectID, name string) ([]byte, error) {
	if !strings.HasPrefix(name, "assets/") || portablepath.Validate(name) != nil {
		return nil, fmt.Errorf("asset path must be a portable path inside assets/")
	}
	operation, err := host.host.AcquireProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer operation.Release()
	if err := operation.Context().Err(); err != nil {
		return nil, err
	}
	return platform.ReadRasterAsset(operation.Layout().ContentRoot, name)
}

func (host Resources) GenerateImage(ctx context.Context, projectID, profileID string, request platform.ImageRequest) (platform.ImageBytes, error) {
	operation, err := host.host.AcquireProject(ctx, projectID)
	if err != nil {
		return platform.ImageBytes{}, err
	}
	defer operation.Release()
	layout := operation.Layout()
	cfg, err := appsettings.RefreshProject(host.host.ImageConfigSnapshot(), layout.ContentRoot, layout.StoreRoot)
	if err != nil {
		return platform.ImageBytes{}, fmt.Errorf("resolve extension image Project settings: %w", err)
	}
	cfg.ProjectID = projectID
	// The shared provider service handles image profile resolution, credentials,
	// provider defaults and downloads. Assets belong to the extension, so this
	// operation does not require a Book Project or mutate library bindings.
	result, err := imagegen.NewService().Generate(operation.Context(), &cfg, imagegen.GenerateRequest{
		Images: request.Inputs, Mask: request.InputMask, Selection: request.Selection,
		ProfileID: profileID, Prompt: request.Prompt, N: 1, Size: request.Size,
		AspectRatio: request.AspectRatio, Resolution: request.Resolution,
		Quality: request.Quality, OutputFormat: request.OutputFormat,
	})
	if errors.Is(err, imagegen.ErrUnsupportedImageInput) {
		return platform.ImageBytes{}, &platform.Error{Code: "UNSUPPORTED", MessageKey: "platform.errors.UNSUPPORTED", Diagnostic: "Selected image model does not support these image inputs"}
	}
	if err != nil {
		return platform.ImageBytes{}, err
	}
	if err := operation.Context().Err(); err != nil {
		return platform.ImageBytes{}, err
	}
	if len(result.Images) == 0 {
		return platform.ImageBytes{}, fmt.Errorf("image provider returned no images")
	}
	return platform.ImageBytes{Data: result.Images[0].Data, RevisedPrompt: result.Images[0].RevisedPrompt}, nil
}

// Project content stays on the application's existing editor/version boundary.
// Leases prevent a Project from disappearing during reads or adoption.
type projectFilesHost interface{ ProjectFiles() *projectfiles.Service }

func (host Resources) ReadProjectContent(ctx context.Context, projectID, path string) (platform.FileSnapshot, error) {
	operation, err := host.host.AcquireProject(ctx, projectID)
	if err != nil {
		return platform.FileSnapshot{}, err
	}
	defer operation.Release()
	files, ok := host.host.(projectFilesHost)
	if !ok {
		return platform.FileSnapshot{}, fmt.Errorf("Project file service is unavailable")
	}
	document, err := files.ProjectFiles().ReadFile(operation.Context(), projectID, path)
	if err != nil {
		return platform.FileSnapshot{}, err
	}
	if !document.Editable || document.Size > platform.MaxDefinitionBytes/2 {
		return platform.FileSnapshot{}, fmt.Errorf("Project content must be bounded text")
	}
	return platform.FileSnapshot{Content: document.Content, Revision: document.Revision}, nil
}
func (host Resources) SaveProjectContent(ctx context.Context, projectID string, input platform.ContentDocument) (string, error) {
	operation, err := host.host.AcquireProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	defer operation.Release()
	files, ok := host.host.(projectFilesHost)
	if !ok {
		return "", fmt.Errorf("Project file service is unavailable")
	}
	revision := "missing"
	if input.ExpectedRevision != nil {
		revision = *input.ExpectedRevision
	}
	saved, err := files.ProjectFiles().SaveFile(operation.Context(), projectID, projectfiles.SaveRequest{Path: input.Path, Content: input.Content, BaseRevision: revision})
	return saved.Revision, err
}
func (host Resources) CreateProjectAsset(ctx context.Context, projectID, path string, data []byte) error {
	operation, err := host.host.AcquireProject(ctx, projectID)
	if err != nil {
		return err
	}
	defer operation.Release()
	files, ok := host.host.(projectFilesHost)
	if !ok {
		return fmt.Errorf("Project file service is unavailable")
	}
	_, err = files.ProjectFiles().CreateAsset(operation.Context(), projectID, path, data)
	return err
}

func (host Resources) ReadProjectAsset(ctx context.Context, projectID, path string) ([]byte, error) {
	operation, err := host.host.AcquireProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer operation.Release()
	if err := operation.Context().Err(); err != nil {
		return nil, err
	}
	return platform.ReadRasterAsset(operation.Layout().ContentRoot, path)
}
