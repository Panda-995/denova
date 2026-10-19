package modelio

import (
	"denova/config"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ModelExecutionPolicy maps the persisted product retry setting to the shared
// per-response budget. Callers add their tool, iteration and idle policies.
func ModelExecutionPolicy(cfg *config.Config) agentexecution.ExecutionPolicy {
	retries := 5
	if cfg != nil && cfg.ModelMaxRetries >= 0 {
		retries = cfg.ModelMaxRetries
	}
	return agentexecution.ExecutionPolicy{
		Retry:            &agentmodel.RetryConfig{Decide: agentmodel.TransientRetry},
		RetryIdentity:    agentschema.CapabilityIdentity{Kind: "denova.retry.transient", Version: 1},
		ModelMaxAttempts: retries + 1,
	}
}
