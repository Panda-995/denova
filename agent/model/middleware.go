package model

import (
	"context"
	"errors"

	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ModelCall is the final provider-neutral request immediately before the
// configured model adapter is invoked. At this seam every transcript rewrite,
// tool-schema decision, and per-call option must already be present.
//
// Middleware may replace Messages or Options, but it must preserve Model unless
// it intentionally installs another adapter with equivalent provider semantics.
type ModelCall struct {
	Model     BaseChatModel
	Messages  []*agentschema.Message
	Options   []ModelOption
	Streaming bool
}

// ModelRequestSnapshot is an immutable side-fork handle over one final model
// call. It deliberately keeps the concrete model adapter opaque: executing a
// fork therefore reuses the same provider, model, endpoint, thinking settings,
// cache routing, and provider compatibility wrappers as the primary call.
//
// The current adapter contract does not expose a provider-private serialized
// request object. Snapshot guarantees exact reuse of the final provider-neutral
// inputs; adapters must assemble those inputs deterministically.
type ModelRequestSnapshot struct {
	model                BaseChatModel
	modelIdentity        agentschema.CapabilityIdentity
	inputEstimator       InputEstimator
	messages             []*agentschema.Message
	options              []ModelOption
	streaming            bool
	stablePrefixMessages int
}

// SnapshotMetadata carries runtime-verified request provenance. The engine fills
// it after middleware has finished rewriting the call.
type SnapshotMetadata struct {
	ModelIdentity        agentschema.CapabilityIdentity
	InputEstimator       InputEstimator
	StablePrefixMessages int
}

// Snapshot freezes a detached copy of this final model call.
func (call *ModelCall) Snapshot() *ModelRequestSnapshot {
	return CaptureModelRequest(call, SnapshotMetadata{})
}

// CaptureModelRequest freezes a request and its trusted execution metadata.
// Metadata never changes the model, messages, or provider options.
func CaptureModelRequest(call *ModelCall, metadata SnapshotMetadata) *ModelRequestSnapshot {
	if call == nil {
		return nil
	}
	identity := metadata.ModelIdentity
	if model, ok := call.Model.(DefinitionModel); ok {
		identity = model.ModelIdentity()
	}
	estimator := metadata.InputEstimator
	if model, ok := call.Model.(ModelInputEstimator); ok {
		estimator = model.InputEstimator()
	}
	return &ModelRequestSnapshot{
		model: call.Model, modelIdentity: identity, inputEstimator: estimator, messages: agentschema.CloneMessages(call.Messages),
		options: append([]ModelOption(nil), call.Options...), streaming: call.Streaming,
		stablePrefixMessages: min(max(0, metadata.StablePrefixMessages), len(call.Messages)),
	}
}

// EstimateInput applies the captured model's visual policy to this exact
// request. Side forks and context maintenance share the same counting units.
func (snapshot *ModelRequestSnapshot) EstimateInput() (InputSize, error) {
	if snapshot == nil {
		return InputSize{}, errors.New("model request snapshot is unavailable")
	}
	return snapshot.inputEstimator.Estimate(snapshot.messages, snapshot.ResolvedOptions().Tools)
}

// ModelIdentity is the stable identity of the captured Definition model.
// Middleware wrappers preserve equivalent provider semantics. An unidentified
// custom model returns zero and uses local token estimates without calibration.
func (snapshot *ModelRequestSnapshot) ModelIdentity() agentschema.CapabilityIdentity {
	if snapshot == nil {
		return agentschema.CapabilityIdentity{}
	}
	return snapshot.modelIdentity
}

// Messages returns a detached copy of the snapshot's model-visible messages.
func (snapshot *ModelRequestSnapshot) Messages() []*agentschema.Message {
	if snapshot == nil {
		return nil
	}
	return agentschema.CloneMessages(snapshot.messages)
}

// ResolvedOptions returns a defensive, provider-neutral view of the exact
// call options captured by this snapshot.
func (snapshot *ModelRequestSnapshot) ResolvedOptions() *Options {
	if snapshot == nil {
		return &Options{}
	}
	return GetCommonOptions(&Options{}, snapshot.options...)
}

// StablePrefixMessages reports the lifecycle-authenticated contiguous message
// prefix that may be reused by provider caches. The boundary is captured after
// caller middleware and cannot be supplied through Message content or Extra.
// Tool schemas remain a separate stable prefix component in ResolvedOptions.
func (snapshot *ModelRequestSnapshot) StablePrefixMessages() int {
	if snapshot == nil {
		return 0
	}
	return min(max(0, snapshot.stablePrefixMessages), len(snapshot.messages))
}

// Append returns a detached fork whose only request-input change is an ordered
// message suffix. The primary snapshot is never mutated.
func (snapshot *ModelRequestSnapshot) Append(messages ...*agentschema.Message) *ModelRequestSnapshot {
	if snapshot == nil {
		return nil
	}
	appended := agentschema.CloneMessages(snapshot.messages)
	appended = append(appended, agentschema.CloneMessages(messages)...)
	return &ModelRequestSnapshot{
		model: snapshot.model, modelIdentity: snapshot.modelIdentity, inputEstimator: snapshot.inputEstimator,
		messages: appended,
		options:  append([]ModelOption(nil), snapshot.options...), streaming: snapshot.streaming,
		stablePrefixMessages: snapshot.StablePrefixMessages(),
	}
}

// WithMessages replaces a side call's input while preserving the captured model
// and options. Replacing the prefix explicitly resets cache-prefix accounting.
func (snapshot *ModelRequestSnapshot) WithMessages(messages []*agentschema.Message) *ModelRequestSnapshot {
	if snapshot == nil {
		return nil
	}
	return &ModelRequestSnapshot{model: snapshot.model, modelIdentity: snapshot.modelIdentity, inputEstimator: snapshot.inputEstimator, messages: agentschema.CloneMessages(messages),
		options: append([]ModelOption(nil), snapshot.options...), streaming: snapshot.streaming}
}

// WithOptions returns a detached side fork that preserves the exact model,
// message prefix, cache boundary, and existing options before applying the
// supplied bounded overrides.
func (snapshot *ModelRequestSnapshot) WithOptions(options ...ModelOption) *ModelRequestSnapshot {
	if snapshot == nil {
		return nil
	}
	return &ModelRequestSnapshot{
		model: snapshot.model, modelIdentity: snapshot.modelIdentity, inputEstimator: snapshot.inputEstimator, messages: agentschema.CloneMessages(snapshot.messages),
		options:   append(append([]ModelOption(nil), snapshot.options...), options...),
		streaming: snapshot.streaming, stablePrefixMessages: snapshot.StablePrefixMessages(),
	}
}

// Generate executes exactly one non-streaming model request from the snapshot.
func (snapshot *ModelRequestSnapshot) Generate(ctx context.Context) (*agentschema.Message, error) {
	if snapshot == nil || snapshot.model == nil {
		return nil, errors.New("model request snapshot is unavailable")
	}
	return snapshot.model.Generate(ctx, agentschema.CloneMessages(snapshot.messages), snapshot.options...)
}

// Stream executes exactly one streaming model request from the snapshot.
func (snapshot *ModelRequestSnapshot) Stream(ctx context.Context) (*agentstream.StreamReader[*agentschema.Message], error) {
	if snapshot == nil || snapshot.model == nil {
		return nil, errors.New("model request snapshot is unavailable")
	}
	return snapshot.model.Stream(ctx, agentschema.CloneMessages(snapshot.messages), snapshot.options...)
}

// Streaming reports the primary call mode captured by the snapshot.
func (snapshot *ModelRequestSnapshot) Streaming() bool {
	return snapshot != nil && snapshot.streaming
}
