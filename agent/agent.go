// Package agent is the entry point for the provider-neutral Native Agent SDK.
// Construct an Agent with New and Definition, open a Session, then submit a Run.
//
// The root package contains composition and lifecycle handles. Model, tool,
// context, and session contracts live in domain packages with their supporting
// capabilities nested below them. Schema contains shared message and input
// values; lifecycle/event contains live payloads; session owns journal storage.
//
// Lifecycle owns admission and journal commits, engine owns model and tool
// execution and opaque checkpoints, and context/history owns pure context
// projection. The engine receives a narrow journal port on each request and
// never depends on concrete Session or Run handles. No package depends on a
// product application, UI, or external runtime selection.
package agent

import (
	"context"

	"github.com/alfredxw/denova/agent/engine"
	"github.com/alfredxw/denova/agent/lifecycle"
	"github.com/alfredxw/denova/agent/lifecycle/trace"
	"github.com/alfredxw/denova/agent/schema"
	"github.com/alfredxw/denova/agent/session"
)

// Agent owns Session handles and task-tree admission. Execution and persistence
// are implemented by the lifecycle and engine packages.
type Agent = lifecycle.Agent

type Option = lifecycle.Option
type RunIDRequest = lifecycle.RunIDRequest
type RunIDGenerator = lifecycle.RunIDGenerator

// New constructs an Agent from a static Definition or dynamic Source.
func New(ctx context.Context, source Source, options ...Option) (*Agent, error) {
	return lifecycle.New(ctx, source, options...)
}

func WithSessionStore(store session.Store, constructionErrors ...error) Option {
	return lifecycle.WithSessionStore(store, constructionErrors...)
}

func WithTrace(sink trace.TraceSink) Option { return lifecycle.WithTrace(sink) }
func WithRunIDGenerator(generate RunIDGenerator) Option {
	return lifecycle.WithRunIDGenerator(generate)
}
func WithCacheKeyGenerator(generate schema.CacheKeyGenerator) Option {
	return lifecycle.WithCacheKeyGenerator(generate)
}

// Definition composes the domain capabilities used by one execution cycle.
// Their contracts live in model, tool, context, and the supporting engine,
// lifecycle, and session subpackages.
type Definition = engine.Definition

type Source = engine.Source
type SourceFunc = engine.SourceFunc
type PrepareRequest = engine.PrepareRequest
type DefinitionInitializer = engine.DefinitionInitializer
type Inspection = engine.Inspection
type TurnReason = engine.TurnReason

const (
	TurnReasonStart        = engine.TurnReasonStart
	TurnReasonSteer        = engine.TurnReasonSteer
	TurnReasonFollowUp     = engine.TurnReasonFollowUp
	TurnReasonNextTurn     = engine.TurnReasonNextTurn
	TurnReasonInteraction  = engine.TurnReasonInteraction
	TurnReasonGoalMutation = engine.TurnReasonGoalMutation
	TurnReasonStructural   = engine.TurnReasonStructural
)

func DefinitionBehaviorIdentity(definition Definition) (string, error) {
	return engine.DefinitionBehaviorIdentity(definition)
}

// Session owns input admission and the canonical journal; Run is one admitted
// execution. Type aliases preserve handle identity without method forwarding.
type Session = lifecycle.Session
type ChildTask = lifecycle.ChildTask
type Run = lifecycle.Run
type RunSnapshot = lifecycle.RunSnapshot
type QueuedInput = lifecycle.QueuedInput
type SuspendRequest = lifecycle.SuspendRequest
type ResumeRequest = lifecycle.ResumeRequest
type Suspension = lifecycle.Suspension

var (
	ErrIdempotencyConflict = lifecycle.ErrIdempotencyConflict
	ErrInputConsumed       = lifecycle.ErrInputConsumed
	ErrInputCancelled      = lifecycle.ErrInputCancelled
	ErrInputQueueFull      = lifecycle.ErrInputQueueFull
)

const ParentSessionAttribute = lifecycle.ParentSessionAttribute

func ChildSessionAttributes(parent session.Key) (map[string]string, error) {
	return lifecycle.ChildSessionAttributes(parent)
}

func ParentSessionKey(child session.Key) (session.Key, error) {
	return lifecycle.ParentSessionKey(child)
}

// RecoveryLog is an optional journal acceleration contract. Its index remains
// rebuildable; canonical records are the only recovery source of truth.
type RecoveryLog = lifecycle.RecoveryLog
type RecoveryIndex = lifecycle.RecoveryIndex
type RecoveryInput = lifecycle.RecoveryInput
type RecoveryRun = lifecycle.RecoveryRun
type RecoveryInteraction = lifecycle.RecoveryInteraction
type RecoveryRecord = lifecycle.RecoveryRecord

type Input = schema.Input
type HostData = schema.HostData
type Result = schema.Result
type ResultStatus = schema.ResultStatus

// Text is the shorthand for a text-only input with no host metadata.
func Text(value string) Input { return schema.Text(value) }
