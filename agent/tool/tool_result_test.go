package tool

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func resultTestDescriptor(limit int) ToolDescriptor {
	return ToolDescriptor{
		Source: ToolSourceRead, Execution: ToolExecutionParallelRead,
		MutationScope: ToolMutationNone, PostCheck: ToolPostCheckNone,
		Recovery: ToolRecoveryReadOnly, ResultRecoveryKind: agentschema.ToolResultRecoveryRead,
		ResultProjection: agentschema.ToolResultBoundedModelContext,
		ResultRetention:  agentschema.ToolResultDeferred,
		Steering:         SteeringFinishCurrent, MaxResultBytes: limit,
	}
}

func TestNormalizeToolResultKeepsModelDisplayAndDetailsIsolated(t *testing.T) {
	model := strings.Repeat("模型🙂", 20)
	display := strings.Repeat("界面✨", 20)
	details := json.RawMessage(`{"receipt":{"revision":"sha256:after"}}`)
	result := agentschema.ToolResult{
		ModelContent: model, DisplayContent: display, Details: details,
		Status:   agentschema.ToolResultSuccess,
		Metadata: agentschema.ToolResultMetadata{Target: "chapters/one.md", IdempotencyKey: "call-1"},
	}

	normalized, err := NormalizeToolResult(result, resultTestDescriptor(48))
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.ModelContent) > 48 || len(normalized.DisplayContent) > 48 {
		t.Fatalf("bounded content exceeded limit: model=%d display=%d", len(normalized.ModelContent), len(normalized.DisplayContent))
	}
	if !utf8.ValidString(normalized.ModelContent) || !utf8.ValidString(normalized.DisplayContent) {
		t.Fatalf("truncation broke UTF-8: model=%q display=%q", normalized.ModelContent, normalized.DisplayContent)
	}
	if !normalized.Metadata.ModelTruncated || !normalized.Metadata.DisplayTruncated ||
		normalized.Metadata.OriginalModelBytes != len(model) || normalized.Metadata.OriginalDisplayBytes != len(display) ||
		normalized.Metadata.ReturnedModelBytes != len(normalized.ModelContent) ||
		normalized.Metadata.ReturnedDisplayBytes != len(normalized.DisplayContent) {
		t.Fatalf("unexpected result metadata: %#v", normalized.Metadata)
	}
	if string(normalized.Details) != string(details) || normalized.Metadata.Target != "chapters/one.md" || normalized.Metadata.IdempotencyKey != "call-1" {
		t.Fatalf("details or durable metadata changed: %#v", normalized)
	}
	message := agentschema.ToolMessage(normalized, "call-1", agentschema.WithToolName("read"))
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sha256:after") || strings.Contains(message.Content, display) {
		t.Fatalf("transcript leaked display/details: %s", encoded)
	}
}

func TestNormalizeToolResultRejectsInvalidStructuredFields(t *testing.T) {
	tests := []struct {
		name   string
		result agentschema.ToolResult
	}{
		{name: "status", result: agentschema.ToolResult{Status: agentschema.ToolResultStatus("future")}},
		{name: "synthetic reason", result: agentschema.ToolResult{Status: agentschema.ToolResultError, SyntheticReason: agentschema.ToolSyntheticReason("future")}},
		{name: "success synthetic", result: agentschema.ToolResult{Status: agentschema.ToolResultSuccess, SyntheticReason: agentschema.ToolSyntheticUnknownTool}},
		{name: "invalid details", result: agentschema.ToolResult{Status: agentschema.ToolResultSuccess, Details: json.RawMessage(`{"broken"`)}},
		{name: "oversized details", result: agentschema.ToolResult{Status: agentschema.ToolResultSuccess, Details: json.RawMessage(`{"value":"0123456789"}`)}},
		{name: "invalid artifact digest", result: agentschema.ToolResult{Status: agentschema.ToolResultSuccess, Artifacts: []agentschema.ToolArtifactRef{{
			ID: "artifact", ReadablePath: "memory://artifact", ContentType: "text/plain", EstimatedBytes: 1, SHA256: "invalid",
		}}}},
		{name: "invalid artifact purpose", result: agentschema.ToolResult{Status: agentschema.ToolResultSuccess, Artifacts: []agentschema.ToolArtifactRef{{
			ID: "artifact", Purpose: agentschema.ToolArtifactPurpose("future"), ReadablePath: ".denova/artifacts/item.log",
			ContentType: "text/plain", Complete: true,
		}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NormalizeToolResult(test.result, resultTestDescriptor(16)); err == nil {
				t.Fatalf("NormalizeToolResult(%#v) succeeded", test.result)
			}
		})
	}
}

func TestNormalizeToolResultBoundsAndRedactsContextHints(t *testing.T) {
	descriptor := resultTestDescriptor(16 * 1024)
	descriptor.ResultRetention = agentschema.ToolResultEagerCandidate
	reference := map[string]any{
		"path":          "chapters/one.md",
		"authorization": "Bearer never-persist-this",
		"headers": map[string]any{
			"X-Custom-Auth": "Bearer custom-header-secret",
		},
		"env": map[string]any{
			"MY_CREDENTIAL": "environment-secret",
			"PRIVATE_KEY":   "private-key-secret",
			"ACCESS_KEY":    "access-key-secret",
			"SESSION_KEY":   "session-key-secret",
		},
		"opaque_header": "Bearer scalar-secret",
		"nested": map[string]any{
			"api_token": "also-secret",
			"query":     strings.Repeat("x", 1024*2),
		},
	}
	result := agentschema.TextToolResult("result")
	result.ContextHints = &agentschema.ToolResultContextHints{
		Recovery: agentschema.ToolResultRecoveryHint{
			Kind: agentschema.ToolResultRecoveryRead, Reference: reference,
			EstimatedBytes: 4096, EstimatedTokens: 1024,
		},
		ContextValue:    agentschema.ToolResultContextDiscardable,
		SupersessionKey: "read:chapters/one.md",
	}

	normalized, err := NormalizeToolResult(result, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.ResultRetention != agentschema.ToolResultEagerCandidate || normalized.ContextHints == nil {
		t.Fatalf("normalized contract = %#v", normalized)
	}
	if got := normalized.ContextHints.Recovery.Reference["authorization"]; got != agentschema.ToolResultHintRedactedValue {
		t.Fatalf("authorization was not redacted: %#v", got)
	}
	nested := normalized.ContextHints.Recovery.Reference["nested"].(map[string]any)
	if got := nested["api_token"]; got != agentschema.ToolResultHintRedactedValue {
		t.Fatalf("api token was not redacted: %#v", got)
	}
	if got := nested["query"].(string); len(got) > 1024 {
		t.Fatalf("hint string exceeded bound: %d", len(got))
	}
	headers := normalized.ContextHints.Recovery.Reference["headers"].(map[string]any)
	environment := normalized.ContextHints.Recovery.Reference["env"].(map[string]any)
	for key, got := range map[string]any{
		"custom auth": headers["X-Custom-Auth"],
		"credential":  environment["MY_CREDENTIAL"],
		"private key": environment["PRIVATE_KEY"],
		"access key":  environment["ACCESS_KEY"],
		"session key": environment["SESSION_KEY"],
		"bare bearer": normalized.ContextHints.Recovery.Reference["opaque_header"],
	} {
		if got != agentschema.ToolResultHintRedactedValue {
			t.Fatalf("%s was not redacted: %#v", key, got)
		}
	}
	if reference["authorization"] != "Bearer never-persist-this" {
		t.Fatal("normalization mutated the caller's recovery map")
	}
	encoded, err := json.Marshal(normalized.ContextHints)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"never-persist-this", "also-secret", "custom-header-secret", "environment-secret",
		"private-key-secret", "access-key-secret", "session-key-secret", "scalar-secret",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("unsafe context hints leaked %q: %s", secret, encoded)
		}
	}
	if len(encoded) > (32 * 1024) {
		t.Fatalf("unsafe context hints: %s", encoded)
	}
}

func TestNormalizeToolResultRejectsUnsafeContextHints(t *testing.T) {
	descriptor := resultTestDescriptor(1024)
	descriptor.ResultRetention = agentschema.ToolResultDeferred
	tests := []agentschema.ToolResultContextHints{
		{Recovery: agentschema.ToolResultRecoveryHint{Kind: agentschema.ToolResultRecoveryKind("future")}},
		{Recovery: agentschema.ToolResultRecoveryHint{Kind: agentschema.ToolResultRecoveryArtifact}},
		{Recovery: agentschema.ToolResultRecoveryHint{Kind: agentschema.ToolResultRecoveryRead, EstimatedBytes: -1}},
		{ContextValue: agentschema.ToolResultContextValue("future")},
	}
	for _, hints := range tests {
		result := agentschema.TextToolResult("result")
		result.ContextHints = &hints
		if _, err := NormalizeToolResult(result, descriptor); err == nil {
			t.Fatalf("accepted unsafe hints: %#v", hints)
		}
	}
}

func TestNormalizeToolResultMarksTruncatedReferenceCollections(t *testing.T) {
	object := make(map[string]any, 32+2)
	items := make([]any, 32+2)
	for index := range items {
		object["field_"+string(rune(0x100+index))] = index
		items[index] = index
	}
	result := agentschema.TextToolResult("result")
	result.ContextHints = &agentschema.ToolResultContextHints{Recovery: agentschema.ToolResultRecoveryHint{
		Kind: agentschema.ToolResultRecoveryRerun,
		Reference: map[string]any{
			"object": object,
			"items":  items,
		},
	}}
	normalized, err := NormalizeToolResult(result, resultTestDescriptor(1024))
	if err != nil {
		t.Fatal(err)
	}
	reference := normalized.ContextHints.Recovery.Reference
	normalizedObject := reference["object"].(map[string]any)
	normalizedItems := reference["items"].([]any)
	if normalizedObject["_truncated"] != agentschema.ToolResultHintTruncatedValue ||
		len(normalizedItems) != 32+1 ||
		normalizedItems[len(normalizedItems)-1] != agentschema.ToolResultHintTruncatedValue {
		t.Fatalf("truncation markers = object:%#v items:%#v", normalizedObject, normalizedItems)
	}
}

func TestNormalizeToolResultArtifactContractDoesNotRequireDigest(t *testing.T) {
	result := agentschema.TextToolResult("preview")
	result.Artifacts = []agentschema.ToolArtifactRef{{
		ID: "artifact-1", Purpose: agentschema.ToolArtifactPurposeAttachment,
		ReadablePath: ".denova/artifacts/session/call.log",
		ContentType:  "text/plain; charset=utf-8", EstimatedBytes: 4096,
		EstimatedTokens: 1024, Complete: true,
	}}
	normalized, err := NormalizeToolResult(result, resultTestDescriptor(1024))
	if err != nil {
		t.Fatal(err)
	}
	artifact := normalized.Artifacts[0]
	if artifact.Purpose != agentschema.ToolArtifactPurposeAttachment || artifact.ReadablePath == "" ||
		artifact.ContentType == "" || artifact.EstimatedBytes != 4096 || artifact.SHA256 != "" {
		t.Fatalf("artifact was not normalized: %#v", artifact)
	}
}

func TestNormalizeToolResultBoundsArtifactMetadata(t *testing.T) {
	valid := agentschema.ToolArtifactRef{
		ID: "artifact", Purpose: agentschema.ToolArtifactPurposeAttachment,
		ReadablePath: ".denova/artifacts/session/item.log", ContentType: "text/plain", Complete: true,
	}
	tooMany := agentschema.TextToolResult("preview")
	tooMany.Artifacts = make([]agentschema.ToolArtifactRef, agentschema.MaxToolResultArtifacts+1)
	for index := range tooMany.Artifacts {
		tooMany.Artifacts[index] = valid
		tooMany.Artifacts[index].ID += string(rune('a' + index%26))
	}
	if _, err := NormalizeToolResult(tooMany, resultTestDescriptor(1024)); err == nil {
		t.Fatal("unbounded artifact count was accepted")
	}

	oversized := agentschema.TextToolResult("preview")
	oversized.Artifacts = []agentschema.ToolArtifactRef{valid}
	oversized.Artifacts[0].ReadablePath = strings.Repeat("p", agentschema.MaxToolResultArtifactMetadataBytes+1)
	if _, err := NormalizeToolResult(oversized, resultTestDescriptor(1024)); err == nil {
		t.Fatal("unbounded artifact metadata was accepted")
	}

	normal := agentschema.TextToolResult("preview")
	normal.Artifacts = []agentschema.ToolArtifactRef{valid, {
		ID: "raw-output", Purpose: agentschema.ToolArtifactPurposeCompleteToolOutput,
		ReadablePath: ".denova/artifacts/session/raw.log", ContentType: "text/plain", Complete: true,
	}}
	normalized, err := NormalizeToolResult(normal, resultTestDescriptor(1024))
	if err != nil || len(normalized.Artifacts) != 2 || normalized.Artifacts[1].Purpose != agentschema.ToolArtifactPurposeCompleteToolOutput {
		t.Fatalf("normal multi-artifact result failed to round-trip: result=%#v err=%v", normalized, err)
	}
}

func TestNormalizeToolResultRevalidatesAfterMiddlewareMutation(t *testing.T) {
	first, err := NormalizeToolResult(agentschema.TextToolResult(strings.Repeat("a", 32)), resultTestDescriptor(8))
	if err != nil {
		t.Fatal(err)
	}
	first.ModelContent = strings.Repeat("b", 64)
	first.DisplayContent = strings.Repeat("c", 64)
	second, err := NormalizeToolResult(first, resultTestDescriptor(16))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ModelContent) > 16 || len(second.DisplayContent) > 16 ||
		second.Metadata.OriginalModelBytes != 64 || second.Metadata.OriginalDisplayBytes != 64 ||
		!second.Metadata.ModelTruncated || !second.Metadata.DisplayTruncated {
		t.Fatalf("re-normalization was bypassed: %#v", second)
	}
}
