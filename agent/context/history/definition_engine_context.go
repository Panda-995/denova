package history

import (
	"fmt"
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func RenderContextFragment(fragment agentschema.ContextFragment) string {
	if EffectiveContextRendering(fragment.Rendering) == agentschema.ContextRenderVerbatim {
		return fragment.Content
	}
	provenance := fmt.Sprintf(
		"Source: %s\nPurpose: %s\nResource: %s",
		fragment.Source, fragment.Purpose, fragment.Resource,
	)
	if revision := strings.TrimSpace(fragment.Revision); revision != "" {
		provenance += "\nRevision: " + revision
	}
	return "# Context\n\n" + provenance + "\n\n" + fragment.Content
}
