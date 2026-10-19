package canonical

import (
	"context"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// EffectApplier accepts idempotent tool effects without owning the Agent's
// conversation journal. It returns one result per request, including failures.
// This lets delegated Agents keep their own transcripts while sharing product
// mutation handling with their parent.
type EffectApplier interface {
	Identity() agentschema.CapabilityIdentity
	ApplyEffects(context.Context, []EffectRequest) ([]EffectResult, error)
}

type EffectApplierFuncs struct {
	CapabilityIdentity agentschema.CapabilityIdentity
	ApplyEffectsFn     func(context.Context, []EffectRequest) ([]EffectResult, error)
}

func (applier EffectApplierFuncs) Identity() agentschema.CapabilityIdentity {
	return applier.CapabilityIdentity
}

func (applier EffectApplierFuncs) ApplyEffects(ctx context.Context, requests []EffectRequest) ([]EffectResult, error) {
	if applier.ApplyEffectsFn == nil {
		return nil, agentschema.ErrCapabilityUnsupported
	}
	return applier.ApplyEffectsFn(ctx, requests)
}
