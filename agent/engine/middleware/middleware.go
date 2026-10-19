// Package middleware defines model and tool interception points. Its contexts
// carry public request metadata, never private loop or checkpoint state.
package middleware

import (
	"context"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// ToolCallEndpoint is the single middleware seam for structured tool calls.
type ToolCallEndpoint func(context.Context, string, ...agenttool.ToolOption) (agentschema.ToolResult, error)

// ToolContext identifies one concrete tool call.
type ToolContext struct {
	Index          int
	Name           string
	ExecutionID    string
	ProviderCallID string
	ParentCallID   string
	Definition     agenttool.ToolDefinitionSnapshot
}

// ModelContext contains read-only metadata for a model invocation.
type ModelContext struct {
	Tools []*agentschema.ToolInfo
	// Iteration is the zero-based model step within the current Agent run.
	Iteration int
	// Attempt is the zero-based provider attempt for this model step. Every
	// retry re-enters BeforeModelCall and fixed context maintenance.
	Attempt int

	contextNormalization *ContextNormalizationMetrics
}

// ContextNormalizationMetrics is a bounded, provider-neutral report produced
// by a presentation middleware when it repairs the final model request. It
// deliberately contains counts only: repaired message bodies never enter
// lifecycle telemetry.
type ContextNormalizationMetrics struct {
	RepairCount    int
	MessagesBefore int
	MessagesAfter  int
}

// ReportContextNormalization reports that this middleware repaired the model
// request. Multiple middleware reports for one attempt are accumulated and
// bounded by the number of messages they observed.
func (context *ModelContext) ReportContextNormalization(metrics ContextNormalizationMetrics) {
	if context == nil || metrics.RepairCount <= 0 {
		return
	}
	metrics.MessagesBefore = max(0, metrics.MessagesBefore)
	metrics.MessagesAfter = max(0, metrics.MessagesAfter)
	metrics.RepairCount = min(metrics.RepairCount, max(1, metrics.MessagesBefore+metrics.MessagesAfter))
	if context.contextNormalization == nil {
		context.contextNormalization = &metrics
		return
	}
	context.contextNormalization.RepairCount += metrics.RepairCount
	context.contextNormalization.RepairCount = min(
		context.contextNormalization.RepairCount,
		max(1, context.contextNormalization.MessagesBefore+metrics.MessagesAfter),
	)
	context.contextNormalization.MessagesAfter = metrics.MessagesAfter
}

// ContextNormalization returns the bounded repair report for this model
// attempt. Hosts may use it for observability; only Agent lifecycle consumes it
// to publish the canonical event.
func (context *ModelContext) ContextNormalization() (ContextNormalizationMetrics, bool) {
	if context == nil || context.contextNormalization == nil {
		return ContextNormalizationMetrics{}, false
	}
	return *context.contextNormalization, true
}

func (context *ModelContext) TakeContextNormalization() (ContextNormalizationMetrics, bool) {
	metrics, present := context.ContextNormalization()
	if context != nil {
		context.contextNormalization = nil
	}
	return metrics, present
}

type contextMaintenanceCommittedKey struct{}

func ContextWithMaintenanceCommitted(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextMaintenanceCommittedKey{}, true)
}

// ContextMaintenanceCommitted reports that Agent already published a durable
// checkpoint during this native loop. Reversible maintenance middleware must
// leave subsequent model calls unchanged so one run cannot publish competing
// context projections.
func ContextMaintenanceCommitted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	committed, _ := ctx.Value(contextMaintenanceCommittedKey{}).(bool)
	return committed
}

// RunContext is mutable once at the beginning of a run.
type RunContext struct {
	Instruction string
	Tools       []agenttool.ToolDefinition
}

// RunState is the in-memory transcript for one run.
type RunState struct {
	Messages  []*agentschema.Message
	ToolInfos []*agentschema.ToolInfo
	Extra     map[string]any
}

// Middleware customizes the native loop without owning it.
type Middleware interface {
	BeforeAgent(context.Context, *RunContext) (context.Context, *RunContext, error)
	AfterAgent(context.Context, *RunState) (context.Context, error)
	BeforeModelRewriteState(context.Context, *RunState, *ModelContext) (context.Context, *RunState, error)
	AfterModelRewriteState(context.Context, *RunState, *ModelContext) (context.Context, *RunState, error)
	WrapModel(context.Context, agentmodel.BaseChatModel, *ModelContext) (agentmodel.BaseChatModel, error)
	BeforeModelCall(context.Context, *agentmodel.ModelCall, *ModelContext) (context.Context, *agentmodel.ModelCall, error)
	// ReviewModelOutput runs only after a complete successful response, before
	// accepting it or executing its tools. Repair feedback is request-local.
	ReviewModelOutput(context.Context, agentmodel.ModelOutput) (agentmodel.ModelOutputReview, error)
	WrapToolCall(context.Context, ToolCallEndpoint, *ToolContext) (ToolCallEndpoint, error)
}

// BaseMiddleware provides no-op implementations for selective embedding.
type BaseMiddleware struct{}

func (*BaseMiddleware) BeforeAgent(ctx context.Context, run *RunContext) (context.Context, *RunContext, error) {
	return ctx, run, nil
}

func (*BaseMiddleware) AfterAgent(ctx context.Context, _ *RunState) (context.Context, error) {
	return ctx, nil
}

func (*BaseMiddleware) BeforeModelRewriteState(ctx context.Context, state *RunState, _ *ModelContext) (context.Context, *RunState, error) {
	return ctx, state, nil
}

func (*BaseMiddleware) AfterModelRewriteState(ctx context.Context, state *RunState, _ *ModelContext) (context.Context, *RunState, error) {
	return ctx, state, nil
}

func (*BaseMiddleware) WrapModel(_ context.Context, model agentmodel.BaseChatModel, _ *ModelContext) (agentmodel.BaseChatModel, error) {
	return model, nil
}

func (*BaseMiddleware) BeforeModelCall(ctx context.Context, call *agentmodel.ModelCall, _ *ModelContext) (context.Context, *agentmodel.ModelCall, error) {
	return ctx, call, nil
}

func (*BaseMiddleware) ReviewModelOutput(context.Context, agentmodel.ModelOutput) (agentmodel.ModelOutputReview, error) {
	return agentmodel.ModelOutputReview{Action: agentmodel.ModelOutputAccept}, nil
}

func (*BaseMiddleware) WrapToolCall(_ context.Context, endpoint ToolCallEndpoint, _ *ToolContext) (ToolCallEndpoint, error) {
	return endpoint, nil
}
