package context

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// CombineContextSources preserves the declared order of independently owned
// context sources behind the single ContextSource lifecycle seam.
func CombineContextSources(sources ...ContextSource) (ContextSource, error) {
	filtered := make([]ContextSource, 0, len(sources))
	identities := make([]agentschema.CapabilityIdentity, 0, len(sources))
	for index, source := range sources {
		if source == nil {
			continue
		}
		identity := source.Identity()
		if err := identity.Validate("Context"); err != nil {
			return nil, fmt.Errorf("combine context source %d: %w", index, err)
		}
		filtered = append(filtered, source)
		identities = append(identities, identity)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	if len(filtered) == 1 {
		return filtered[0], nil
	}
	encoded, err := json.Marshal(identities)
	if err != nil {
		return nil, fmt.Errorf("encode combined context identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return combinedContextSource{
		sources: filtered,
		identity: agentschema.CapabilityIdentity{
			Kind: "context.combined", Version: 1, ConfigHash: hex.EncodeToString(digest[:]),
		},
	}, nil
}

type combinedContextSource struct {
	identity agentschema.CapabilityIdentity
	sources  []ContextSource
}

func (source combinedContextSource) Identity() agentschema.CapabilityIdentity { return source.identity }

func (source combinedContextSource) Materialize(ctx context.Context, request ContextRequest) ([]agentschema.ContextFragment, error) {
	fragments := make([]agentschema.ContextFragment, 0, len(source.sources))
	for index, child := range source.sources {
		materialized, err := child.Materialize(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("materialize combined context source %d (%s): %w", index, child.Identity().Kind, err)
		}
		fragments = append(fragments, materialized...)
	}
	return fragments, nil
}
