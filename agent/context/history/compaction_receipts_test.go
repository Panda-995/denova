package history

import (
	"fmt"
	"strings"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func protectedToolMessage(callID, path string, status agentschema.ToolResultStatus) *agentschema.Message {
	return &agentschema.Message{
		Role: agentschema.ToolRole, Content: "FULL RAW TOOL BODY MUST NOT ENTER SUMMARY", ToolCallID: callID, ToolName: "write",
		ToolResult: &agentschema.ToolResultSummary{
			Status: status, ResultRetention: agentschema.ToolResultProtected,
			ProtectedReceipt: &agentschema.ToolResultProtectedReceipt{
				SanitizedArguments: fmt.Sprintf(`{"path":%q}`, path),
				Outcome:            `{"changed":true}`,
			},
			Artifacts: []agentschema.ToolArtifactRef{{
				ID: "private-artifact-id", Purpose: agentschema.ToolArtifactPurposeCompleteToolOutput,
				ReadablePath: path, ContentType: "text/plain", SHA256: strings.Repeat("a", 64),
				EstimatedBytes: 1024, EstimatedTokens: 256, Complete: true,
			}},
		},
	}
}

func TestCompactionPreservesBoundedProtectedReceiptsWithoutRawArtifactSecrets(t *testing.T) {
	message := protectedToolMessage("call-one", ".agent/artifacts/call-one.txt", agentschema.ToolResultSuccess)
	summary := MergeProtectedReceiptContext("narrative summary", "", []*agentschema.Message{message}, 64<<10)
	for _, want := range []string{
		"narrative summary", protectedReceiptTitle, "call-one", ".agent/artifacts/call-one.txt", `\"changed\":true`,
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("protected summary missing %q: %s", want, summary)
		}
	}
	for _, forbidden := range []string{"FULL RAW TOOL BODY", "private-artifact-id", strings.Repeat("a", 64), "sha256"} {
		if strings.Contains(summary, forbidden) {
			t.Fatalf("protected summary leaked %q: %s", forbidden, summary)
		}
	}
	if len(summary) > 64<<10 {
		t.Fatalf("protected summary bytes=%d", len(summary))
	}
}

func TestRepeatedCompactionMergesProtectedReceiptsWithoutDuplication(t *testing.T) {
	first := protectedToolMessage("call-one", ".agent/artifacts/one.txt", agentschema.ToolResultSuccess)
	previous := MergeProtectedReceiptContext("first summary", "", []*agentschema.Message{first}, 64<<10)
	second := protectedToolMessage("call-two", ".agent/artifacts/two.txt", agentschema.ToolResultError)
	current := MergeProtectedReceiptContext("second summary", previous, []*agentschema.Message{first, second}, 64<<10)
	if strings.Count(current, `"call_id":"call-one"`) != 1 || strings.Count(current, `"call_id":"call-two"`) != 1 {
		t.Fatalf("repeated protected receipts were duplicated or lost: %s", current)
	}
	_, block := splitProtectedReceipts(current)
	lines := strings.Split(block, "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], `"call_id":"call-two"`) {
		t.Fatalf("unresolved receipt was not retained first: %s", block)
	}
}

func TestProtectedReceiptSelectionIsBoundedAndReportsOmissions(t *testing.T) {
	messages := make([]*agentschema.Message, 0, protectedReceiptLimit+12)
	for index := 0; index < protectedReceiptLimit+12; index++ {
		status := agentschema.ToolResultSuccess
		if index == 0 {
			status = agentschema.ToolResultError
		}
		messages = append(messages, protectedToolMessage(
			fmt.Sprintf("call-%02d", index), fmt.Sprintf(".agent/artifacts/%02d.txt", index), status,
		))
	}
	block := receiptsFromMessages(messages)
	lines := strings.Split(block, "\n")
	if len(lines) > protectedReceiptLimit+1 || len(block) > protectedReceiptBytes {
		t.Fatalf("receipt block lines=%d bytes=%d", len(lines), len(block))
	}
	if !strings.Contains(lines[0], `"call_id":"call-00"`) || !strings.Contains(block, `"omitted_receipts"`) {
		t.Fatalf("bounded receipt selection=%s", block)
	}
}

func TestReceiptOverflowNeverTruncatesCheckpointFacts(t *testing.T) {
	body := strings.Repeat("Preserve corrected budget 72519 and source file proof.md. ", 12)
	messages := []*agentschema.Message{protectedToolMessage("latest", ".agent/artifacts/result.txt", agentschema.ToolResultSuccess)}
	result := MergeProtectedReceiptContext(body, "", messages, len(body)+300)
	if !strings.HasPrefix(result, strings.TrimSpace(body)) || strings.Contains(result, "...[truncated]") || !strings.Contains(result, "session journal") || len(result) > len(body)+300 {
		t.Fatalf("checkpoint facts were truncated or receipt capacity exceeded: %s", result)
	}
}
