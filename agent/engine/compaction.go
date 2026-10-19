package engine

import (
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
)

type compactionHealthState struct {
	Fingerprint         string `json:"fingerprint"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	FailureCode         string `json:"failure_code,omitempty"`
}

func normalizedAutomaticCompactionFailureLimit(policy agentexecution.ExecutionPolicy) int {
	if policy.MaxAutomaticCompactionFailures > 0 {
		return policy.MaxAutomaticCompactionFailures
	}
	return agenthistory.DefaultMaxAutomaticCompactionFailures
}
