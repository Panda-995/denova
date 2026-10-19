package schema

import (
	"encoding/json"
)

// ToolResultProjection declares how a result may enter model context.
type ToolResultProjection string

const ToolResultBoundedModelContext ToolResultProjection = "bounded_model_context"

// ToolResultRetentionMode declares when a rich result may leave model context.
// The pressure planner remains the only component allowed to apply cleanup.
type ToolResultRetentionMode string

const (
	ToolResultDeferred       ToolResultRetentionMode = "deferred"
	ToolResultEagerCandidate ToolResultRetentionMode = "eager_candidate"
	ToolResultProtected      ToolResultRetentionMode = "protected"
)

// ToolResultStatus is the exhaustive outcome of a tool call.
type ToolResultStatus string

const (
	ToolResultSuccess ToolResultStatus = "success"
	ToolResultError   ToolResultStatus = "error"
	ToolResultBlocked ToolResultStatus = "blocked"
	ToolResultSkipped ToolResultStatus = "skipped"
)

// ToolSyntheticReason identifies why no ordinary tool completion produced a
// result. Empty means the tool really executed.
type ToolSyntheticReason string

const (
	ToolSyntheticUnknownTool         ToolSyntheticReason = "unknown_tool"
	ToolSyntheticInvalidCall         ToolSyntheticReason = "invalid_call"
	ToolSyntheticInvalidArguments    ToolSyntheticReason = "invalid_arguments"
	ToolSyntheticModelIncomplete     ToolSyntheticReason = "model_output_incomplete"
	ToolSyntheticPolicyBlocked       ToolSyntheticReason = "policy_blocked"
	ToolSyntheticSteeringBeforeStart ToolSyntheticReason = "steering_before_start"
	ToolSyntheticSteeringInterrupted ToolSyntheticReason = "steering_interrupted"
	ToolSyntheticEffectUnknown       ToolSyntheticReason = "effect_unknown"
)

// ToolResultMetadata is display/durability metadata and never enters model
// content implicitly.
type ToolResultMetadata struct {
	OriginalModelBytes   int                      `json:"original_model_bytes"`
	ReturnedModelBytes   int                      `json:"returned_model_bytes"`
	OriginalDisplayBytes int                      `json:"original_display_bytes"`
	ReturnedDisplayBytes int                      `json:"returned_display_bytes"`
	ModelTruncated       bool                     `json:"model_truncated"`
	DisplayTruncated     bool                     `json:"display_truncated"`
	Target               string                   `json:"target,omitempty"`
	IdempotencyKey       string                   `json:"idempotency_key,omitempty"`
	ArtifactPersistence  *ToolArtifactPersistence `json:"artifact_persistence,omitempty"`
}

// ToolArtifactPersistence records a bounded outcome for an attempted spill.
// FailureReason is a safe classification and never contains a raw storage
// error, path, credential, or tool output.
type ToolArtifactPersistence struct {
	Attempted     bool   `json:"attempted"`
	Complete      bool   `json:"complete"`
	FailureReason string `json:"failure_reason,omitempty"`
}

const (
	ToolArtifactFailureStoreUnavailable = "store_unavailable"
	ToolArtifactFailureBegin            = "begin_failed"
	ToolArtifactFailureWrite            = "write_failed"
	ToolArtifactFailureCommit           = "commit_failed"
)

// ToolArtifactPurpose identifies what an artifact proves about a tool result.
type ToolArtifactPurpose string

const (
	MaxToolResultArtifacts             = 64
	MaxToolResultArtifactMetadataBytes = 128 * 1024

	// ToolArtifactPurposeCompleteModelOutput means the artifact contains every
	// byte of ModelContent before any inline projection or truncation. Only this
	// purpose can replace an arbitrary rich model projection by itself.
	ToolArtifactPurposeCompleteModelOutput ToolArtifactPurpose = "complete_model_output"
	// ToolArtifactPurposeCompleteToolOutput contains the complete primary byte
	// stream emitted by a tool before Denova adds its bounded result envelope.
	// The retained envelope plus this artifact is a lossless recovery path for
	// streaming tools that cannot buffer an exact ModelContent copy in memory.
	ToolArtifactPurposeCompleteToolOutput ToolArtifactPurpose = "complete_tool_output"
	// ToolArtifactPurposeAttachment is an auxiliary file associated with the
	// result. It may be useful evidence, but it is not a lossless ModelContent
	// replacement and therefore never authorizes context cleanup on its own.
	ToolArtifactPurposeAttachment ToolArtifactPurpose = "attachment"
)

// ToolArtifactRef points to immutable tool output held outside model history.
// ReadablePath must remain inside the host's active session/workspace boundary
// so the ordinary read capability can apply its normal range and byte limits.
type ToolArtifactRef struct {
	ID              string              `json:"id"`
	Purpose         ToolArtifactPurpose `json:"purpose,omitempty"`
	ReadablePath    string              `json:"readable_path,omitempty"`
	ContentType     string              `json:"content_type,omitempty"`
	EstimatedBytes  int64               `json:"estimated_bytes"`
	EstimatedTokens int                 `json:"estimated_tokens"`
	Complete        bool                `json:"complete"`
	// SHA256 is optional diagnostic metadata.
	SHA256 string `json:"sha256,omitempty"`
}

// ToolResultRecoveryKind identifies the ordinary capability that can recover
// content after a rich result has left model context.
type ToolResultRecoveryKind string

const (
	ToolResultRecoveryRead     ToolResultRecoveryKind = "read"
	ToolResultRecoveryRefetch  ToolResultRecoveryKind = "refetch"
	ToolResultRecoveryRerun    ToolResultRecoveryKind = "rerun"
	ToolResultRecoveryArtifact ToolResultRecoveryKind = "artifact"
)

// ToolResultRecoveryHint is bounded and redacted by NormalizeToolResult before
// it can be persisted or consumed by context planning. For read, refetch, and
// rerun, Reference is the complete replayable tool-argument object; Cleanup
// rejects the entire hint if normalization had to redact or truncate any
// nested value. Artifact recovery instead authenticates ArtifactPath through
// ToolArtifactVerifier and does not use Reference as proof.
type ToolResultRecoveryHint struct {
	Kind            ToolResultRecoveryKind `json:"kind,omitempty"`
	Reference       map[string]any         `json:"reference,omitempty"`
	ArtifactPath    string                 `json:"artifact_path,omitempty"`
	EstimatedBytes  int64                  `json:"estimated_bytes,omitempty"`
	EstimatedTokens int                    `json:"estimated_tokens,omitempty"`
}

type ToolResultContextValue string

const (
	ToolResultContextNormal      ToolResultContextValue = "normal"
	ToolResultContextDiscardable ToolResultContextValue = "discardable"
)

// ToolResultContextHints contains semantic cleanup signals. It does not grant
// permission to clean a result and does not create a second state machine.
type ToolResultContextHints struct {
	Recovery        ToolResultRecoveryHint `json:"recovery,omitempty"`
	ContextValue    ToolResultContextValue `json:"context_value,omitempty"`
	SupersessionKey string                 `json:"supersession_key,omitempty"`
}

// ToolResultProtectedReceipt is the bounded, redacted continuity projection
// for a protected or unresolved tool outcome. It is carried independently of
// ModelContent so checkpoint compaction can preserve the operation without
// copying the raw result body back into model context.
type ToolResultProtectedReceipt struct {
	SanitizedArguments string `json:"sanitized_arguments,omitempty"`
	Outcome            string `json:"outcome,omitempty"`
}

// ToolResult separates bounded model context from display content and
// structured durability details.
type ToolResult struct {
	ModelContent   string `json:"model_content"`
	DisplayContent string `json:"display_content"`
	// Attachments are native images already saved as immutable, owner-relative
	// copies. Model input includes their pixels independently of text limits.
	Attachments      []Attachment                `json:"attachments,omitempty"`
	Details          json.RawMessage             `json:"details,omitempty"`
	Status           ToolResultStatus            `json:"status"`
	SyntheticReason  ToolSyntheticReason         `json:"synthetic_reason,omitempty"`
	Metadata         ToolResultMetadata          `json:"metadata"`
	ResultRetention  ToolResultRetentionMode     `json:"result_retention"`
	ContextHints     *ToolResultContextHints     `json:"context_hints,omitempty"`
	ProtectedReceipt *ToolResultProtectedReceipt `json:"protected_receipt,omitempty"`
	Artifacts        []ToolArtifactRef           `json:"artifacts,omitempty"`
	Effects          []Effect                    `json:"effects,omitempty"`
}

// TextToolResult constructs the common successful text result.
func TextToolResult(content string) ToolResult {
	return ToolResult{ModelContent: content, DisplayContent: content, Status: ToolResultSuccess}
}

// IsError reports the provider/runtime error bit for this outcome.
func (result ToolResult) IsError() bool { return result.Status != ToolResultSuccess }
