package model

import (
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ModelFinishReasonClass is the provider-neutral meaning of a raw model finish
// reason. Unknown reasons remain complete so new provider success states do not
// accidentally turn into failures.
type ModelFinishReasonClass string

const (
	ModelFinishReasonOther         ModelFinishReasonClass = "other"
	ModelFinishReasonOutputLimit   ModelFinishReasonClass = "output_limit"
	ModelFinishReasonContextLimit  ModelFinishReasonClass = "context_limit"
	ModelFinishReasonContentFilter ModelFinishReasonClass = "content_filter"
	ModelFinishReasonIncomplete    ModelFinishReasonClass = "incomplete"
)

// ClassifyModelFinishReason normalizes common provider spellings without
// discarding the raw reason retained in ResponseMeta.
func ClassifyModelFinishReason(reason string) ModelFinishReasonClass {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.Join(strings.Fields(normalized), "_")
	switch normalized {
	case "length", "max_tokens", "max_output_tokens", "token_limit":
		return ModelFinishReasonOutputLimit
	case "model_context_window_exceeded", "context_window_exceeded", "context_length_exceeded":
		return ModelFinishReasonContextLimit
	case "content_filter", "safety", "recitation", "blocklist", "prohibited_content", "spii", "language",
		"image_safety", "image_prohibited_content", "image_recitation":
		return ModelFinishReasonContentFilter
	case "incomplete", "malformed_function_call", "unexpected_tool_call", "too_many_tool_calls", "image_other", "no_image":
		return ModelFinishReasonIncomplete
	default:
		return ModelFinishReasonOther
	}
}

// Incomplete reports whether the response may be only a prefix of the model's
// intended output. Tool execution must be blocked for every such class.
func (class ModelFinishReasonClass) Incomplete() bool {
	switch class {
	case ModelFinishReasonOutputLimit, ModelFinishReasonContextLimit,
		ModelFinishReasonContentFilter, ModelFinishReasonIncomplete:
		return true
	default:
		return false
	}
}

// TerminalReason returns the stable application reason for this incomplete
// class. Complete and unknown classes have no incomplete terminal reason.
func (class ModelFinishReasonClass) TerminalReason() string {
	switch class {
	case ModelFinishReasonOutputLimit:
		return agentschema.ModelOutputTruncatedReason
	case ModelFinishReasonContextLimit:
		return agentschema.ModelContextWindowExceededReason
	case ModelFinishReasonContentFilter:
		return agentschema.ModelOutputFilteredReason
	case ModelFinishReasonIncomplete:
		return agentschema.ModelOutputIncompleteReason
	default:
		return ""
	}
}

func ClassifyResponseFinishReason(meta *agentschema.ResponseMeta) (string, ModelFinishReasonClass) {
	if meta == nil {
		return "", ModelFinishReasonOther
	}
	reason := strings.TrimSpace(meta.FinishReason)
	return reason, ClassifyModelFinishReason(reason)
}

// IsModelIncompleteTerminalReason reports whether a settled Run reason is one
// of the stable model-incomplete codes safe to expose to localized clients.
func IsModelIncompleteTerminalReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case agentschema.ModelOutputTruncatedReason, agentschema.ModelContextWindowExceededReason,
		agentschema.ModelOutputFilteredReason, agentschema.ModelOutputIncompleteReason:
		return true
	default:
		return false
	}
}
