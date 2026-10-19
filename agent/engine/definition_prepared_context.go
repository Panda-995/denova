package engine

import (
	"context"
	"errors"
	"fmt"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// preparedContext preserves the bounded model context of an unfinished cycle
// in its existing journal checkpoint. Executable tools and policy identities
// are still materialized and checked on every resume. A new cycle reads fresh
// context; resuming accepted work must not depend on mutable source files.
type preparedContext struct {
	Version            uint16                        `json:"version"`
	Fragments          []agentschema.ContextFragment `json:"fragments,omitempty"`
	GoalFragments      []agentschema.ContextFragment `json:"goal_fragments,omitempty"`
	GoalReservedTokens int                           `json:"goal_reserved_tokens,omitempty"`
}

func snapshotPreparedContext(prepared preparedDefinition) *preparedContext {
	if prepared.preparationStage != enginePreparationMaterialized {
		return nil
	}
	return &preparedContext{
		Version:            1,
		Fragments:          append([]agentschema.ContextFragment(nil), prepared.fragments[:len(prepared.fragments)-len(prepared.goalFragments)]...),
		GoalFragments:      append([]agentschema.ContextFragment(nil), prepared.goalFragments...),
		GoalReservedTokens: prepared.goalReservedTokens,
	}
}

func (snapshot *preparedContext) validate() error {
	if snapshot.Version != 1 || snapshot.GoalReservedTokens < 0 {
		return errors.New("invalid prepared Agent context checkpoint")
	}
	fragments := append(append([]agentschema.ContextFragment(nil), snapshot.Fragments...), snapshot.GoalFragments...)
	return validateContextFragments(fragments)
}

func (engine *Engine) materializeCycleCapabilities(ctx context.Context, request PrepareRequest, snapshot TurnSnapshot, saved *preparedContext, prepared *preparedDefinition) error {
	if saved == nil {
		// Released checkpoints did not retain their prepared context. Their
		// original fingerprint must still match before accepting a new snapshot.
		if err := materializeDefinitionCapabilities(ctx, request, prepared); err != nil {
			return err
		}
		return engine.applyGoalPreparation(ctx, Request{Snapshot: snapshot}, prepared)
	}
	if err := saved.validate(); err != nil {
		return fmt.Errorf("restore prepared Agent context: %w", err)
	}
	if err := materializeDefinitionTools(ctx, request, prepared); err != nil {
		return err
	}
	if err := engine.applyGoalPreparation(ctx, Request{Snapshot: snapshot}, prepared); err != nil {
		return err
	}
	prepared.fragments = append(append([]agentschema.ContextFragment(nil), saved.Fragments...), saved.GoalFragments...)
	prepared.goalFragments = append([]agentschema.ContextFragment(nil), saved.GoalFragments...)
	prepared.goalReservedTokens = saved.GoalReservedTokens
	return updatePreparedPrefixFingerprint(prepared)
}
