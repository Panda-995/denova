package result

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"unicode/utf8"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type memoryStorage struct {
	request agenttool.ToolArtifactRequest
	content bytes.Buffer
}

func (storage *memoryStorage) BeginToolArtifact(_ context.Context, request agenttool.ToolArtifactRequest) (agenttool.ToolArtifactWriter, error) {
	storage.request = request
	storage.content.Reset()
	return &memoryWriter{storage: storage}, nil
}

func (storage *memoryStorage) VerifyToolArtifact(_ context.Context, artifact agentschema.ToolArtifactRef, request agenttool.ToolArtifactRequest) error {
	if artifact.ID == "complete" && request.ToolCallID == storage.request.ToolCallID {
		return nil
	}
	return context.Canceled
}

type memoryWriter struct{ storage *memoryStorage }

func (writer *memoryWriter) Write(value []byte) (int, error) {
	return writer.storage.content.Write(value)
}
func (writer *memoryWriter) Abort() error { return nil }
func (writer *memoryWriter) Commit() (agentschema.ToolArtifactRef, error) {
	content := writer.storage.content.Bytes()
	hash := sha256.Sum256(content)
	return agentschema.ToolArtifactRef{
		ID: "complete", Purpose: agentschema.ToolArtifactPurposeCompleteModelOutput,
		ReadablePath: ".agent/artifacts/complete.log", ContentType: "text/plain; charset=utf-8",
		EstimatedBytes: int64(len(content)), Complete: true, SHA256: hex.EncodeToString(hash[:]),
	}, nil
}

func TestStandardMaterializesCompleteOutputAndBuildsRecoveryReceipt(t *testing.T) {
	storage := &memoryStorage{}
	ctx := agenttool.ContextWithToolArtifactBackend(context.Background(), storage)
	content := "HEAD-SENTINEL\n" + strings.Repeat("large result ", 80) + "\nTAIL-SENTINEL"
	descriptor := resultDescriptor(agentschema.ToolResultProtected)
	processed, err := Standard(Policy{MaxBytes: 256}).Process(ctx, ToolResultProcessRequest{
		ToolName: "read", Arguments: `{"path":"chapter.md","api_key":"secret"}`,
		ExecutionID: "execution-1", ProviderCallID: "provider-1",
		Definition: agenttool.ToolDefinitionSnapshot{Descriptor: descriptor},
		Result:     agentschema.TextToolResult(content),
	})
	if err != nil {
		t.Fatal(err)
	}
	if storage.request.ToolCallID != "execution-1" || storage.content.String() != content {
		t.Fatalf("artifact request=%#v content=%q", storage.request, storage.content.String())
	}
	if len(processed.ModelContent) > 256 || !strings.Contains(processed.ModelContent, "HEAD-SENTINEL") ||
		!strings.Contains(processed.ModelContent, "TAIL-SENTINEL") || !processed.Metadata.ModelTruncated {
		t.Fatalf("model preview = %q metadata=%#v", processed.ModelContent, processed.Metadata)
	}
	if len(processed.Artifacts) != 1 || !processed.Artifacts[0].Complete ||
		processed.ContextHints == nil || processed.ContextHints.Recovery.ArtifactPath != ".agent/artifacts/complete.log" {
		t.Fatalf("artifact recovery = %#v %#v", processed.Artifacts, processed.ContextHints)
	}
	if processed.ProtectedReceipt == nil || strings.Contains(processed.ProtectedReceipt.SanitizedArguments, "secret") ||
		!strings.Contains(processed.ProtectedReceipt.SanitizedArguments, redactedValue) {
		t.Fatalf("protected receipt = %#v", processed.ProtectedReceipt)
	}
}

func TestStandardFailsClosedWhenProtectedOutputCannotBeMaterialized(t *testing.T) {
	content := strings.Repeat("protected mutation output", 100)
	processed, err := Standard(Policy{MaxBytes: 128}).Process(context.Background(), ToolResultProcessRequest{
		ToolName: "write", Arguments: `{"path":"chapter.md"}`,
		ExecutionID: "execution-2",
		Definition:  agenttool.ToolDefinitionSnapshot{Descriptor: resultDescriptor(agentschema.ToolResultProtected)},
		Result:      agentschema.TextToolResult(content),
	})
	if err == nil || !agenttool.IsToolControlError(err) {
		t.Fatalf("error = %v, want ToolControlError", err)
	}
	if !processed.Metadata.ModelTruncated || processed.Metadata.ArtifactPersistence == nil ||
		processed.Metadata.ArtifactPersistence.FailureReason != agentschema.ToolArtifactFailureStoreUnavailable {
		t.Fatalf("processed failure projection = %#v", processed)
	}
}

func TestStandardDoesNotTrustRecoverableArtifactWithoutExplicitVerifier(t *testing.T) {
	storage := &memoryStorage{}
	ctx := agenttool.ContextWithToolArtifactStore(context.Background(), storage)
	result := agentschema.TextToolResult("bounded output")
	result.Artifacts = []agentschema.ToolArtifactRef{{
		ID: "forged", Purpose: agentschema.ToolArtifactPurposeCompleteModelOutput,
		ReadablePath: ".agent/artifacts/forged.log", ContentType: "text/plain", Complete: true,
	}}
	processed, err := Standard(Policy{MaxBytes: 256}).Process(ctx, ToolResultProcessRequest{
		ToolName: "read", Arguments: `{"path":"chapter.md"}`, ExecutionID: "execution-forged",
		Definition: agenttool.ToolDefinitionSnapshot{Descriptor: resultDescriptor(agentschema.ToolResultDeferred)},
		Result:     result,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(processed.Artifacts) != 1 || processed.Artifacts[0].Purpose != agentschema.ToolArtifactPurposeAttachment ||
		processed.ContextHints == nil || processed.ContextHints.Recovery.Kind == agentschema.ToolResultRecoveryArtifact {
		t.Fatalf("unverified artifact became recoverable: %#v", processed)
	}
}

func resultDescriptor(retention agentschema.ToolResultRetentionMode) agenttool.ToolDescriptor {
	return agenttool.ToolDescriptor{
		Source: agenttool.ToolSourceRead, Execution: agenttool.ToolExecutionParallelRead,
		MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone,
		Recovery: agenttool.ToolRecoveryReadOnly, ResultProjection: agentschema.ToolResultBoundedModelContext,
		ResultRetention: retention, ResultRecoveryKind: agentschema.ToolResultRecoveryRead,
		Steering: agenttool.SteeringFinishCurrent, MaxResultBytes: 1 << 20,
	}
}

func TestStandardBoundsAggregateBatchAndPersistsMultibyteEvidence(t *testing.T) {
	const batchSize = 12
	policy := Policy{MaxBytes: 32 << 10, ContextWindowTokens: 16_000}
	processor := Standard(policy)
	content := "HEAD-SENTINEL\n" + strings.Repeat("é验a证🌏", 2000) + "\nMIDDLE: budget corrected to 72519\n" + strings.Repeat("日本語", 1000) + "\nTAIL-SENTINEL"
	var totalBytes, totalTokens int
	for index := range batchSize {
		storage := &memoryStorage{}
		ctx := agenttool.ContextWithToolArtifactBackend(context.Background(), storage)
		processed, err := processor.Process(ctx, ToolResultProcessRequest{
			ToolName: "read", ProviderCallID: "source", ExecutionID: "source", BatchSize: batchSize,
			Definition: agenttool.ToolDefinitionSnapshot{Descriptor: resultDescriptor(agentschema.ToolResultDeferred)},
			Result:     agentschema.TextToolResult(content),
		})
		if err != nil {
			t.Fatalf("result %d: %v", index, err)
		}
		if storage.content.String() != content {
			t.Fatal("batch projection lost complete source evidence")
		}
		if !utf8.ValidString(processed.ModelContent) || !strings.Contains(processed.ModelContent, storage.content.String()[:13]) || !strings.Contains(processed.ModelContent, "TAIL-SENTINEL") || !strings.Contains(processed.ModelContent, "artifact=.agent/artifacts/complete.log") {
			t.Fatalf("invalid batch preview: %q", processed.ModelContent)
		}
		totalBytes += len(processed.ModelContent)
		totalTokens += agentmodel.EstimateMessageTextTokens(agentschema.ToolMessage(processed, "source", agentschema.WithToolName("read")))
	}
	if totalBytes > policy.MaxBytes || totalTokens > policy.BatchTokenLimit() {
		t.Fatalf("aggregate batch exceeds reserves: bytes=%d/%d tokens=%d/%d", totalBytes, policy.MaxBytes, totalTokens, policy.BatchTokenLimit())
	}
}

func TestStandardStopsWhenBatchCannotFitArtifactReferences(t *testing.T) {
	storage := &memoryStorage{}
	ctx := agenttool.ContextWithToolArtifactBackend(context.Background(), storage)
	content := strings.Repeat("completed mutation receipt; ", 100)
	result, err := Standard(Policy{MaxBytes: 128, ContextWindowTokens: 1000}).Process(ctx, ToolResultProcessRequest{
		ToolName: "write", ExecutionID: "mutation", ProviderCallID: "mutation", BatchSize: 100,
		Definition: agenttool.ToolDefinitionSnapshot{Descriptor: resultDescriptor(agentschema.ToolResultProtected)},
		Result:     agentschema.TextToolResult(content),
	})
	if err == nil || !agenttool.IsToolControlError(err) {
		t.Fatalf("capacity error=%v", err)
	}
	if storage.content.String() != content || len(result.Artifacts) != 1 || !result.Artifacts[0].Complete || result.Status != agentschema.ToolResultSuccess {
		t.Fatalf("capacity failure erased the completed tool outcome: %+v", result)
	}
}
