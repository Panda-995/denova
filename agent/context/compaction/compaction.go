package compaction

import (
	"context"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type CompactionRequest struct {
	Force            bool
	IdempotencyKey   string
	ExpectedID       string
	ExpectedRevision uint64
}

type CompactionRemoveRequest struct {
	ID               string
	ExpectedRevision uint64
	IdempotencyKey   string
}

type CompactionPlanRequest struct {
	Session agentschema.SessionView
	Run     agentschema.RunView
	Groups  []agenthistory.CompactionGroup
	// RetainedBytes measures the protected suffix, for policies retaining more history.
	RetainedBytes int
	// EstimateAfter projects replacing a nonempty prefix of Groups through the
	// active request preparation pipeline, including protected user inputs,
	// context, tool visibility and schemas. The generated summary is excluded;
	// policies must reserve its budget. This callback is valid only during Plan
	// and does not call the model or publish journal state. Final validation
	// rebuilds the request again with the actual checkpoint.
	EstimateAfter           func(groupCount int) (agentmodel.InputSize, error)
	ModelSnapshot           *agentmodel.ModelRequestSnapshot
	LifecycleReservedTokens int
	Force                   bool
	Current                 *agenthistory.CompactionState
}

// CompactionCompactRequest contains only the selected incremental source: the
// previous checkpoint followed by newly covered messages. Hosts with model-only
// visibility policies must apply the same policy to Messages before delegation.
type CompactionCompactRequest struct {
	Session       agentschema.SessionView
	Run           agentschema.RunView
	Messages      []*agentschema.Message
	ModelSnapshot *agentmodel.ModelRequestSnapshot
	Current       *agenthistory.CompactionState
}

// CompactionCheckpoint is the semantic result of either extension point. Agent
// estimates its size and atomically persists ContextData with the checkpoint.
// ContextData requires a type, version, valid JSON and at most 8 MiB; it is never
// automatically injected into the model.
type CompactionCheckpoint struct {
	Summary     string
	ContextData *agentschema.HostData
}

// CompactionManager chooses when and how much to compact, then generates the
// checkpoint. Agent owns grouping, active intent, final request validation,
// journal coverage, revision, cancellation and recovery for every implementation.
type CompactionManager interface {
	Identity() agentschema.CapabilityIdentity
	SummaryLimitBytes() int
	Plan(context.Context, CompactionPlanRequest) (agenthistory.CompactionPlan, error)
	Compact(context.Context, CompactionCompactRequest) (CompactionCheckpoint, error)
}

type CompactionResult struct {
	Changed bool
	State   agenthistory.CompactionState
}
