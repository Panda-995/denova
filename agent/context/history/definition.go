package history

import (
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type ContextFragmentIdentity struct {
	Source, Purpose, Resource, Revision string
	StateID                             string
	Stability                           agentschema.ContextStability
	Placement                           agentschema.ContextPlacement
	Rendering                           agentschema.ContextRendering
	Role                                agentschema.RoleType
	ContentHash                         string
	Bytes                               int
}

func EffectiveContextRendering(rendering agentschema.ContextRendering) agentschema.ContextRendering {
	if rendering == "" {
		return agentschema.ContextRenderAttributed
	}
	return rendering
}
