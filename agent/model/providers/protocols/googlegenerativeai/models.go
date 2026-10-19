package googlegenerativeai

import (
	"context"
	"sort"
	"strings"

	"github.com/alfredxw/denova/agent/model/providers"
)

// ListModels lists native Gemini model IDs without requiring a model selection.
func (*Adapter) ListModels(ctx context.Context, config providers.ModelConfig) ([]providers.ModelInfo, error) {
	compatibility, err := resolveCompatibility(config)
	if err != nil {
		return nil, err
	}
	client, err := googleClient(ctx, config, compatibility)
	if err != nil {
		return nil, err
	}
	models := make([]providers.ModelInfo, 0)
	seen := make(map[string]bool)
	for model, err := range client.Models.All(ctx) {
		if err != nil {
			return nil, modelCallError(ctx, err)
		}
		id := strings.TrimPrefix(strings.TrimSpace(model.Name), "models/")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, providers.ModelInfo{ID: id, DisplayName: model.DisplayName, OwnedBy: "google"})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

var _ providers.ModelListingAdapter = (*Adapter)(nil)
