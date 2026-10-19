package schema

import (
	"testing"
)

func TestMessageEffectiveToolResultReadsLegacyAndStructuredHistory(t *testing.T) {
	legacy := (&Message{Role: ToolRole, Content: "legacy", ToolCallID: "old"}).EffectiveToolResult()
	if legacy.Status != ToolResultSuccess || legacy.ModelContent != "legacy" || legacy.DisplayContent != "legacy" {
		t.Fatalf("legacy result = %#v", legacy)
	}
	structured := (&Message{
		Role: ToolRole, Content: "blocked", ToolCallID: "new",
		ToolResult: &ToolResultSummary{Status: ToolResultBlocked, SyntheticReason: ToolSyntheticPolicyBlocked, ModelTruncated: true},
	}).EffectiveToolResult()
	if structured.Status != ToolResultBlocked || structured.SyntheticReason != ToolSyntheticPolicyBlocked || !structured.Metadata.ModelTruncated {
		t.Fatalf("structured result = %#v", structured)
	}
}

func TestToolMessagePersistsNewResultContextContract(t *testing.T) {
	result := TextToolResult("bounded preview")
	result.ResultRetention = ToolResultEagerCandidate
	result.ContextHints = &ToolResultContextHints{
		Recovery: ToolResultRecoveryHint{
			Kind: ToolResultRecoveryArtifact, ArtifactPath: ".denova/artifacts/session/call.log",
			EstimatedBytes: 8192, EstimatedTokens: 2048,
		},
		ContextValue: ToolResultContextDiscardable, SupersessionKey: "read:chapter.md",
	}
	result.Metadata.ArtifactPersistence = &ToolArtifactPersistence{Attempted: true, Complete: true}
	result.Artifacts = []ToolArtifactRef{{
		ID: "artifact", ReadablePath: ".denova/artifacts/session/call.log",
		ContentType: "text/plain", EstimatedBytes: 8192, EstimatedTokens: 2048, Complete: true,
	}}

	message := ToolMessage(result, "call-1", WithToolName("read"))
	result.ContextHints.Recovery.ArtifactPath = "changed"
	result.Metadata.ArtifactPersistence.Complete = false
	restored := message.EffectiveToolResult()
	if restored.ResultRetention != ToolResultEagerCandidate || restored.ContextHints == nil ||
		restored.ContextHints.Recovery.ArtifactPath != ".denova/artifacts/session/call.log" ||
		restored.Metadata.ArtifactPersistence == nil || !restored.Metadata.ArtifactPersistence.Complete ||
		len(restored.Artifacts) != 1 || !restored.Artifacts[0].Complete {
		t.Fatalf("restored result = %#v", restored)
	}
	restored.ContextHints.Recovery.ArtifactPath = "mutated"
	if message.ToolResult.ContextHints.Recovery.ArtifactPath != ".denova/artifacts/session/call.log" {
		t.Fatal("EffectiveToolResult shared context hints with persisted message")
	}
}
