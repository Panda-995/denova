package platform

import (
	"context"

	"denova/config"
	appsettings "denova/internal/app/settings"
	"denova/internal/platform"
)

// ImageModels resolves the same Project configuration used for generation.
// Only explicit public fields cross the extension boundary, never endpoints,
// headers, credentials, provider workflows or prompt instructions.
func (host Resources) ImageModels(ctx context.Context, projectID string) ([]platform.ImageModel, error) {
	operation, err := host.host.AcquireProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer operation.Release()
	layout := operation.Layout()
	cfg, err := appsettings.RefreshProject(host.host.ImageConfigSnapshot(), layout.ContentRoot, layout.StoreRoot)
	if err != nil {
		return nil, err
	}
	ids := []string{config.DefaultImageAPIProfileID}
	for _, profile := range cfg.ImageAPIProfiles {
		if profile.ID != "" && profile.ID != config.DefaultImageAPIProfileID {
			ids = append(ids, profile.ID)
		}
	}
	models := []platform.ImageModel{}
	for _, id := range ids {
		profile, err := config.ResolveImageAPIProfile(&cfg, id)
		if err != nil {
			continue // Incomplete profiles cannot be used for a generation.
		}
		name := profile.Name
		if name == "" {
			name = profile.Model
		}
		models = append(models, platform.ImageModel{ID: profile.ProfileID, Name: name, ImageInput: profile.Protocol != config.ImageProtocolComfyUI})
	}
	return models, operation.Context().Err()
}
