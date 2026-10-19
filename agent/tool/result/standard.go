// Package toolresult provides the reusable lossless result processor for
// public Agent Definitions. Product hosts supply storage; this package owns
// result bounds, artifact materialization, recovery hints, and safe receipts.
package result

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

const (
	DefaultMaxBytes               = 128 * 1024
	ProtectedArgumentsMaxBytes    = 4 * 1024
	ProtectedOutcomeMaxBytes      = 8 * 1024
	standardReceiptSchema         = "agent.tool_result.receipt.v1"
	toolResultArtifactContentType = "text/plain; charset=utf-8"
	redactedValue                 = "[redacted from retained tool context]"
)

// Policy is semantic processor configuration and therefore participates in
// Definition behavior identity. MaxBytes bounds one result and the aggregate
// content of one assistant tool batch. Zero selects DefaultMaxBytes.
type Policy struct {
	MaxBytes            int
	ContextWindowTokens int
}

type standardProcessor struct {
	policy   Policy
	identity agentschema.CapabilityIdentity
}

// Standard returns the built-in lossless result processor. It is suitable for
// standalone coding Agents as well as product-composed Definitions.
func Standard(policy Policy) ToolResultProcessor {
	policy.MaxBytes = normalizeLimit(policy.MaxBytes)
	policy.ContextWindowTokens = max(0, policy.ContextWindowTokens)
	encoded, _ := json.Marshal(policy)
	hash := sha256.Sum256(encoded)
	return &standardProcessor{
		policy: policy,
		identity: agentschema.CapabilityIdentity{
			Kind: "tool_result_processor.standard", Version: 1,
			ConfigHash: hex.EncodeToString(hash[:]),
		},
	}
}

// BatchTokenLimit is the complete next-tool-batch reserve used by both the
// result processor and the checkpoint planner. It counts protocol envelopes as
// well as content, and adapts to the actual model's context window.
func (policy Policy) BatchTokenLimit() int {
	limit := max(1, normalizeLimit(policy.MaxBytes)/3)
	if policy.ContextWindowTokens > 0 {
		limit = min(limit, max(1, policy.ContextWindowTokens/10))
	}
	return limit
}

func (processor *standardProcessor) Identity() agentschema.CapabilityIdentity {
	if processor == nil {
		return agentschema.CapabilityIdentity{}
	}
	return processor.identity
}

func (processor *standardProcessor) Process(
	ctx context.Context,
	request ToolResultProcessRequest,
) (agentschema.ToolResult, error) {
	if processor == nil {
		return request.Result, fmt.Errorf("standard ToolResultProcessor is nil")
	}
	descriptor := request.Definition.Descriptor
	limit := descriptor.MaxResultBytes
	if limit <= 0 || limit > processor.policy.MaxBytes {
		limit = processor.policy.MaxBytes
	}
	batchSize := max(1, request.BatchSize)
	limit = min(limit, max(1, processor.policy.MaxBytes/batchSize))
	envelope := agentschema.ToolMessage(agentschema.TextToolResult(""), request.ProviderCallID, agentschema.WithToolName(request.ToolName))
	tokenLimit := processor.policy.BatchTokenLimit()/batchSize - agentmodel.EstimateMessageTextTokens(envelope)
	descriptor.MaxResultBytes = limit
	result := request.Result
	result.ModelContent = strings.ToValidUTF8(result.ModelContent, "\uFFFD")
	result.DisplayContent = strings.ToValidUTF8(result.DisplayContent, "\uFFFD")
	result.Artifacts = verifiedArtifacts(ctx, request, result.Artifacts)
	visibleBytes := len(result.ModelContent)
	originalBytes := max(visibleBytes, result.Metadata.OriginalModelBytes)
	result.Metadata.OriginalModelBytes = originalBytes

	artifact := recoverableArtifact(result.Artifacts)
	upstreamLoss := result.Metadata.ModelTruncated && artifact == nil
	oversized := visibleBytes > limit || agentmodel.EstimateTextTokens(result.ModelContent) > tokenLimit
	if oversized && artifact == nil && !upstreamLoss {
		var failure string
		artifact, failure = materialize(ctx, request, result.ModelContent)
		if artifact != nil {
			result.Artifacts = appendArtifact(result.Artifacts, *artifact)
			result.Metadata.ArtifactPersistence = &agentschema.ToolArtifactPersistence{Attempted: true, Complete: true}
		} else {
			result.Metadata.ArtifactPersistence = &agentschema.ToolArtifactPersistence{
				Attempted: true, Complete: false, FailureReason: failure,
			}
			result.ModelContent = headTail(result.ModelContent, limit, "complete output unavailable; failure="+failure)
			result.Metadata.ModelTruncated = true
		}
	}
	if artifact != nil {
		result.Metadata.ArtifactPersistence = &agentschema.ToolArtifactPersistence{Attempted: true, Complete: true}
		applyArtifactRecovery(&result, *artifact, originalBytes, descriptor.ResultRetention)
		if oversized || originalBytes > limit {
			preview, err := boundedPreview(result.ModelContent, limit, tokenLimit,
				"status="+string(result.Status)+"; complete=true; artifact="+artifact.ReadablePath)
			if err != nil {
				return result, agenttool.MarkToolControlError(err)
			}
			result.ModelContent = preview
			result.Metadata.ModelTruncated = true
		}
	} else {
		applyReplayRecovery(&result, request, originalBytes)
	}
	applyProtectedReceipt(&result, request, descriptor, limit)
	normalized, err := agenttool.NormalizeToolResult(result, descriptor)
	if err != nil {
		return result, fmt.Errorf("normalize processed tool result: %w", err)
	}
	if artifact == nil && (oversized || normalized.Metadata.ModelTruncated) {
		failure := agentschema.ToolArtifactFailureStoreUnavailable
		if persistence := normalized.Metadata.ArtifactPersistence; persistence != nil && persistence.FailureReason != "" {
			failure = persistence.FailureReason
		} else {
			normalized.Metadata.ArtifactPersistence = &agentschema.ToolArtifactPersistence{
				Attempted: true, Complete: false, FailureReason: failure,
			}
		}
		applyProtectedReceipt(&normalized, request, descriptor, limit)
		if failed, normalizeErr := agenttool.NormalizeToolResult(normalized, descriptor); normalizeErr == nil {
			normalized = failed
		}
		return normalized, agenttool.MarkToolControlError(fmt.Errorf("persist complete tool result: %s", failure))
	}
	return normalized, nil
}

func verifiedArtifacts(ctx context.Context, request ToolResultProcessRequest, artifacts []agentschema.ToolArtifactRef) []agentschema.ToolArtifactRef {
	if len(artifacts) == 0 {
		return nil
	}
	verifier := agenttool.ToolArtifactVerifierFromContext(ctx)
	callID := effectiveCallID(request)
	result := make([]agentschema.ToolArtifactRef, len(artifacts))
	for index, artifact := range artifacts {
		artifact = canonicalArtifact(artifact)
		if recoverablePurpose(artifact.Purpose) {
			expected := agenttool.ToolArtifactRequest{ToolName: request.ToolName, ToolCallID: callID, Purpose: artifact.Purpose}
			if verifier == nil || verifier.VerifyToolArtifact(ctx, artifact, expected) != nil {
				artifact.Purpose = agentschema.ToolArtifactPurposeAttachment
			}
		}
		result[index] = artifact
	}
	return result
}

func materialize(ctx context.Context, request ToolResultProcessRequest, content string) (*agentschema.ToolArtifactRef, string) {
	store := agenttool.ToolArtifactStoreFromContext(ctx)
	if store == nil {
		return nil, agentschema.ToolArtifactFailureStoreUnavailable
	}
	writer, err := store.BeginToolArtifact(ctx, agenttool.ToolArtifactRequest{
		ToolName: request.ToolName, ToolCallID: effectiveCallID(request),
		Purpose:  agentschema.ToolArtifactPurposeCompleteModelOutput,
		MIMEType: toolResultArtifactContentType, Extension: "log",
		Description: "Complete model-visible output from one tool call",
	})
	if err != nil {
		return nil, agentschema.ToolArtifactFailureBegin
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		_ = writer.Abort()
		return nil, agentschema.ToolArtifactFailureWrite
	}
	reference, err := writer.Commit()
	if err != nil {
		_ = writer.Abort()
		return nil, agentschema.ToolArtifactFailureCommit
	}
	reference = canonicalArtifact(reference)
	if reference.Purpose != agentschema.ToolArtifactPurposeCompleteModelOutput || !reference.Complete ||
		reference.ID == "" || reference.ReadablePath == "" || reference.ContentType == "" {
		return nil, agentschema.ToolArtifactFailureCommit
	}
	return &reference, ""
}

func effectiveCallID(request ToolResultProcessRequest) string {
	if value := strings.TrimSpace(request.ExecutionID); value != "" {
		return value
	}
	return strings.TrimSpace(request.ProviderCallID)
}

func canonicalArtifact(artifact agentschema.ToolArtifactRef) agentschema.ToolArtifactRef {
	artifact.ID = strings.TrimSpace(artifact.ID)
	artifact.Purpose = agentschema.ToolArtifactPurpose(strings.TrimSpace(string(artifact.Purpose)))
	artifact.ReadablePath = strings.TrimSpace(strings.ToValidUTF8(artifact.ReadablePath, "\uFFFD"))
	artifact.ContentType = strings.TrimSpace(artifact.ContentType)
	if artifact.EstimatedTokens == 0 && artifact.EstimatedBytes > 0 && !agentschema.IsNativeImageMediaType(artifact.ContentType) {
		artifact.EstimatedTokens = estimatedTokens(artifact.EstimatedBytes)
	}
	return artifact
}

func recoverablePurpose(purpose agentschema.ToolArtifactPurpose) bool {
	return purpose == agentschema.ToolArtifactPurposeCompleteModelOutput || purpose == agentschema.ToolArtifactPurposeCompleteToolOutput
}

func recoverableArtifact(artifacts []agentschema.ToolArtifactRef) *agentschema.ToolArtifactRef {
	for _, artifact := range artifacts {
		artifact = canonicalArtifact(artifact)
		if artifact.Complete && artifact.ReadablePath != "" && artifact.ContentType != "" && recoverablePurpose(artifact.Purpose) {
			return &artifact
		}
	}
	return nil
}

func appendArtifact(artifacts []agentschema.ToolArtifactRef, artifact agentschema.ToolArtifactRef) []agentschema.ToolArtifactRef {
	for index := range artifacts {
		if artifacts[index].ID == artifact.ID || canonicalArtifact(artifacts[index]).ReadablePath == artifact.ReadablePath {
			artifacts[index] = artifact
			return artifacts
		}
	}
	return append(artifacts, artifact)
}

func applyArtifactRecovery(result *agentschema.ToolResult, artifact agentschema.ToolArtifactRef, originalBytes int, retention agentschema.ToolResultRetentionMode) {
	if result.ContextHints == nil {
		result.ContextHints = &agentschema.ToolResultContextHints{}
	}
	result.ContextHints.Recovery = agentschema.ToolResultRecoveryHint{
		Kind: agentschema.ToolResultRecoveryArtifact, ArtifactPath: artifact.ReadablePath,
		EstimatedBytes:  max(artifact.EstimatedBytes, int64(originalBytes)),
		EstimatedTokens: max(artifact.EstimatedTokens, estimatedTokens(int64(originalBytes))),
	}
	if result.ContextHints.ContextValue == "" {
		result.ContextHints.ContextValue = agentschema.ToolResultContextNormal
		if retention == agentschema.ToolResultEagerCandidate {
			result.ContextHints.ContextValue = agentschema.ToolResultContextDiscardable
		}
	}
}

func applyReplayRecovery(result *agentschema.ToolResult, request ToolResultProcessRequest, originalBytes int) {
	if result.Status != agentschema.ToolResultSuccess {
		return
	}
	if result.ContextHints == nil {
		result.ContextHints = &agentschema.ToolResultContextHints{}
	}
	hints := result.ContextHints
	if hints.Recovery.Kind == "" && request.Definition.Descriptor.ResultRecoveryKind != "" {
		if reference := boundedArguments(request.Arguments); len(reference) > 0 {
			hints.Recovery = agentschema.ToolResultRecoveryHint{
				Kind: request.Definition.Descriptor.ResultRecoveryKind, Reference: reference,
				EstimatedBytes: int64(originalBytes), EstimatedTokens: estimatedTokens(int64(originalBytes)),
			}
		}
	}
	if hints.ContextValue == "" {
		hints.ContextValue = agentschema.ToolResultContextNormal
		if request.Definition.Descriptor.ResultRetention == agentschema.ToolResultEagerCandidate {
			hints.ContextValue = agentschema.ToolResultContextDiscardable
		}
	}
	if hints.SupersessionKey == "" && hints.Recovery.Kind != "" {
		hints.SupersessionKey = idempotencyKey(request.ToolName, request.Arguments)
	}
	if hints.Recovery.Kind == "" && hints.SupersessionKey == "" && hints.ContextValue == agentschema.ToolResultContextNormal {
		result.ContextHints = nil
	}
}

func applyProtectedReceipt(result *agentschema.ToolResult, request ToolResultProcessRequest, descriptor agenttool.ToolDescriptor, limit int) {
	protected := descriptor.ResultRetention == agentschema.ToolResultProtected || result.Status != agentschema.ToolResultSuccess ||
		result.SyntheticReason != "" || descriptor.MutationScope != agenttool.ToolMutationNone ||
		result.Metadata.ArtifactPersistence != nil || len(result.Artifacts) > 0
	if !protected {
		result.ProtectedReceipt = nil
		return
	}
	arguments := sanitizedArguments(request.Arguments, min(ProtectedArgumentsMaxBytes, limit))
	outcome := protectedOutcome(request, *result, min(ProtectedOutcomeMaxBytes, limit))
	if arguments == "" && outcome == "" {
		result.ProtectedReceipt = nil
		return
	}
	result.ProtectedReceipt = &agentschema.ToolResultProtectedReceipt{SanitizedArguments: arguments, Outcome: outcome}
}

func protectedOutcome(request ToolResultProcessRequest, result agentschema.ToolResult, limit int) string {
	type artifactReceipt struct {
		Purpose     agentschema.ToolArtifactPurpose `json:"purpose,omitempty"`
		Path        string                          `json:"path"`
		ContentType string                          `json:"content_type,omitempty"`
		Bytes       int64                           `json:"bytes,omitempty"`
	}
	artifacts := make([]artifactReceipt, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		artifact = canonicalArtifact(artifact)
		if !artifact.Complete || artifact.ReadablePath == "" || agentschema.ContainsSensitiveToolContextMaterial(artifact.ReadablePath) {
			continue
		}
		artifacts = append(artifacts, artifactReceipt{
			Purpose: artifact.Purpose, Path: artifact.ReadablePath, ContentType: artifact.ContentType, Bytes: artifact.EstimatedBytes,
		})
	}
	receipt := struct {
		Schema        string                          `json:"schema"`
		Tool          string                          `json:"tool"`
		Status        agentschema.ToolResultStatus    `json:"status"`
		Synthetic     agentschema.ToolSyntheticReason `json:"synthetic_reason,omitempty"`
		Source        agenttool.ToolSource            `json:"source"`
		Mutation      agenttool.ToolMutationScope     `json:"mutation_scope"`
		Recovery      agenttool.ToolRecoveryClass     `json:"recovery"`
		Target        string                          `json:"target,omitempty"`
		OriginalBytes int                             `json:"original_bytes,omitempty"`
		Truncated     bool                            `json:"truncated,omitempty"`
		Artifacts     []artifactReceipt               `json:"artifacts,omitempty"`
		Note          string                          `json:"note"`
	}{
		Schema: standardReceiptSchema, Tool: strings.TrimSpace(request.ToolName), Status: result.Status,
		Synthetic: result.SyntheticReason, Source: request.Definition.Descriptor.Source,
		Mutation: request.Definition.Descriptor.MutationScope, Recovery: request.Definition.Descriptor.Recovery,
		Target: safeTarget(request.Arguments), OriginalBytes: result.Metadata.OriginalModelBytes,
		Truncated: result.Metadata.ModelTruncated, Artifacts: artifacts,
		Note: "The rich result existed in the source turn; use the recovery reference or repeat the call if exact evidence is needed.",
	}
	encoded, err := json.Marshal(receipt)
	if err == nil && len(encoded) <= limit {
		return string(encoded)
	}
	fallback, _ := json.Marshal(map[string]any{
		"schema": standardReceiptSchema, "tool": receipt.Tool, "status": receipt.Status,
		"original_bytes": receipt.OriginalBytes,
	})
	if len(fallback) <= limit {
		return string(fallback)
	}
	return ""
}

func sanitizedArguments(arguments string, limit int) string {
	var value any
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(strings.ToValidUTF8(arguments, "\uFFFD"))))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	encoded, err := json.Marshal(sanitizeValue(value, 0))
	if err == nil && len(encoded) <= limit {
		return string(encoded)
	}
	fallback, _ := json.Marshal(map[string]any{
		"schema": "agent.tool_call.arguments_omitted.v1", "original_bytes": len(arguments),
	})
	if len(fallback) <= limit {
		return string(fallback)
	}
	return ""
}

func sanitizeValue(value any, depth int) any {
	if depth >= 10 {
		return "[nested value omitted]"
	}
	switch typed := value.(type) {
	case string:
		if agentschema.ContainsSensitiveToolContextMaterial(typed) {
			return redactedValue
		}
		if len(typed) > 4096 {
			return fmt.Sprintf("[string omitted: %d bytes]", len(typed))
		}
		return typed
	case []any:
		limit := min(len(typed), 64)
		result := make([]any, 0, limit+1)
		for _, item := range typed[:limit] {
			result = append(result, sanitizeValue(item, depth+1))
		}
		if limit < len(typed) {
			result = append(result, map[string]any{"_omitted_items": len(typed) - limit})
		}
		return result
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > 64 {
			keys = keys[:64]
		}
		result := make(map[string]any, len(keys))
		for _, key := range keys {
			if agentschema.IsSensitiveToolContextKey(key) {
				result[key] = redactedValue
			} else {
				result[key] = sanitizeValue(typed[key], depth+1)
			}
		}
		return result
	default:
		return value
	}
}

func boundedArguments(arguments string) map[string]any {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" || len(arguments) > 32*1024 {
		return nil
	}
	var result map[string]any
	if json.Unmarshal([]byte(arguments), &result) != nil {
		return nil
	}
	return result
}

func safeTarget(arguments string) string {
	values := boundedArguments(arguments)
	for _, key := range []string{"path", "file_path", "filename", "file", "pattern"} {
		value, _ := values[key].(string)
		value = strings.TrimSpace(strings.ToValidUTF8(value, "\uFFFD"))
		if value == "" {
			continue
		}
		if agentschema.ContainsSensitiveToolContextMaterial(value) {
			return redactedValue
		}
		if len(value) > 4096 {
			return utf8Prefix(value, 4080) + "...[truncated]"
		}
		return value
	}
	return ""
}

func idempotencyKey(toolName, arguments string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(arguments)))
	return strings.ToLower(strings.TrimSpace(toolName)) + ":" + hex.EncodeToString(hash[:8])
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultMaxBytes
	}
	return limit
}

func estimatedTokens(bytes int64) int {
	if bytes <= 0 {
		return 0
	}
	return int((bytes + 3) / 4)
}

func boundedPreview(content string, byteLimit, tokenLimit int, status string) (string, error) {
	notice := fmt.Sprintf("[tool result preview: original_bytes=%d; %s]\n", len(content), status)
	if len(notice) > byteLimit || agentmodel.EstimateTextTokens(notice) > tokenLimit {
		return "", fmt.Errorf("%w: tool batch cannot fit its complete-output references (%d bytes, %d tokens per result)", agentschema.ErrContextLimit, byteLimit, tokenLimit)
	}
	available := byteLimit - len(notice)
	for {
		excerpt := content
		if len(excerpt) > available {
			const separator = "\n...[middle omitted]...\n"
			if available <= len(separator) {
				excerpt = ""
			} else {
				bodyBytes := available - len(separator)
				head := utf8Prefix(content, bodyBytes/2)
				excerpt = head + separator + utf8Suffix(content, bodyBytes-len(head))
			}
		}
		preview := notice + excerpt
		tokens := agentmodel.EstimateTextTokens(preview)
		if tokens <= tokenLimit {
			return preview, nil
		}
		available = max(0, min(available-1, available*tokenLimit/tokens))
	}
}

func headTail(content string, limit int, status string) string {
	if limit <= 0 || len(content) <= limit {
		return content
	}
	note := fmt.Sprintf("\n[tool result truncated]\n[tool result preview: original_bytes=%d; %s]", len(content), strings.TrimSpace(status))
	if len(note) >= limit {
		return utf8Prefix(note, limit)
	}
	separator := "\n...[middle omitted]...\n"
	available := limit - len(note) - len(separator)
	if available <= 0 {
		return utf8Prefix(note, limit)
	}
	head := utf8Prefix(content, available/2)
	tail := utf8Suffix(content, available-len(head))
	return head + separator + tail + note
}

func utf8Prefix(value string, limit int) string {
	if limit >= len(value) {
		return value
	}
	limit = max(0, limit)
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

func utf8Suffix(value string, limit int) string {
	if limit >= len(value) {
		return value
	}
	start := len(value) - max(0, limit)
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

var _ ToolResultProcessor = (*standardProcessor)(nil)
