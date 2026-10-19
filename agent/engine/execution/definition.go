// Package execution defines execution policy and invocation-scoped host services.
// Runtime state is supplied through narrow callbacks, not lifecycle handles.
package execution

import (
	"time"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type ExecutionPolicy struct {
	Retry *agentmodel.RetryConfig
	// ModelMaxAttempts includes the first provider call and any failure retries
	// or business output repairs for one logical response. Zero means one.
	ModelMaxAttempts int
	// RetryIdentity gives retry behavior a stable, inspectable identity when
	// Retry is non-nil. Function and closure addresses are not identities.
	RetryIdentity   agentschema.CapabilityIdentity
	ToolParallelism int
	MaxIterations   int
	// IdleTimeout limits only a continuous period with no model chunk, tool
	// lifecycle/progress event, or Interaction request. Zero means unlimited;
	// it is never interpreted as a total run deadline.
	IdleTimeout time.Duration
	// MaxAutomaticCompactionFailures opens the Session failure fuse for one
	// unchanged final model request. Zero uses the Agent default. A changed
	// request automatically gets a fresh attempt; explicit Compact calls are
	// never blocked by this policy.
	MaxAutomaticCompactionFailures int
}
