package claude

import (
	"denova/internal/agents/runtime/external"

	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
)

// InputEstimator uses the same resolved model identity as Run. Unrecognized CLI
// aliases use the shared image fallback rather than compressed file size.
func (c *Client) InputEstimator(input external.Input) agentmodel.InputEstimator {
	model := ""
	if input.Selection.Claude != nil {
		model = input.Selection.Claude.Model
	}
	if input.Selection.ModelProfileID() != "" {
		model = c.apiModel
	}
	return (providers.ModelConfig{Model: model}).InputEstimator()
}
