package schema

import (
	"encoding/json"
	"errors"
)

type HostData struct {
	Type    string          `json:"type"`
	Version uint16          `json:"version"`
	Data    json.RawMessage `json:"data"`
}

type Input struct {
	Text           string
	Attachments    []Attachment
	IdempotencyKey string
	Context        []ContextFragment
	Goal           *GoalMutation
	HostData       *HostData
}

func Text(value string) Input { return Input{Text: value} }

type ResultStatus string

const (
	// Model input failures are distinct from text/vision context exhaustion.
	ModelImageInputRejectedReason = "agent_runtime.model_image_input_rejected"
	ModelRequestTooLargeReason    = "agent_runtime.model_request_too_large"

	// Model incomplete reasons are stable terminal codes for partial responses;
	// clients localize them without discarding the provider's raw finish reason.
	ModelOutputTruncatedReason       = "agent_runtime.model_output_truncated"
	ModelContextWindowExceededReason = "agent_runtime.model_context_window_exceeded"
	ModelOutputFilteredReason        = "agent_runtime.model_output_filtered"
	ModelOutputIncompleteReason      = "agent_runtime.model_output_incomplete"

	ResultCompleted  ResultStatus = "completed"
	ResultFailed     ResultStatus = "failed"
	ResultAborted    ResultStatus = "aborted"
	ResultIncomplete ResultStatus = "incomplete"
	ResultBlocked    ResultStatus = "blocked"
	// ResultSuspended ends the current process handle without settling the
	// logical Run. ResumeRun creates a new handle for the same RunID.
	ResultSuspended ResultStatus = "suspended"
)

type Result struct {
	Status ResultStatus
	Reason string
}

var (
	ErrAgentClosed                = errors.New("agent is closed")
	ErrInvalidInput               = errors.New("agent input is invalid")
	ErrSessionBusy                = errors.New("agent session is busy")
	ErrSessionClosed              = errors.New("agent session is closed")
	ErrRunSettled                 = errors.New("agent run is settled")
	ErrNoActiveRun                = errors.New("agent session has no active run")
	ErrDefinitionUnavailable      = errors.New("agent Definition is unavailable")
	ErrDefinitionMismatch         = errors.New("agent Definition does not match the active transcript")
	ErrCursorExpired              = errors.New("agent event cursor expired")
	ErrCapabilityUnsupported      = errors.New("agent capability is unsupported")
	ErrInvalidInteractionResponse = errors.New("invalid agent interaction response")
	ErrInteractionStale           = errors.New("agent interaction is stale")
	ErrPermissionDenied           = errors.New("agent permission denied")
	ErrPermissionArgumentsChanged = errors.New("agent tool arguments changed after authorization")
	ErrContextLimit               = errors.New("agent context limit reached")
	ErrIdleTimeout                = errors.New("agent execution idle timeout")
	ErrCanonicalCommitRejected    = errors.New("agent canonical commit was rejected")
)
