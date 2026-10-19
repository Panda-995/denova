package context

import (
	"errors"
	"reflect"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestNormalizeModelContextMessagesPreservesValidRichExchange(t *testing.T) {
	call := contextNormalizerTestCall("text_tool_call_stable", "read", `{"path":"chapter.md"}`)
	result := agentschema.ToolMessage(agentschema.ToolResult{
		ModelContent: "complete rich chapter", DisplayContent: "complete rich chapter", Status: agentschema.ToolResultSuccess,
		ResultRetention: agentschema.ToolResultProtected,
		ContextHints: &agentschema.ToolResultContextHints{
			Recovery: agentschema.ToolResultRecoveryHint{
				Kind: agentschema.ToolResultRecoveryRead, Reference: map[string]any{"path": "chapter.md"},
			},
			ContextValue: agentschema.ToolResultContextDiscardable,
		},
	}, call.ID, agentschema.WithToolName("read"))
	input := []*agentschema.Message{
		agentschema.SystemMessage("system"),
		agentschema.AssistantMessage("I will inspect it.", []agentschema.ToolCall{call}),
		result,
		agentschema.UserMessage("continue"),
	}

	normalized, err := NormalizeModelContextMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalized, input) {
		t.Fatalf("valid rich exchange changed:\nwant=%#v\ngot=%#v", input, normalized)
	}
	if normalized[1] == input[1] || normalized[2] == input[2] || normalized[2].ToolResult == input[2].ToolResult {
		t.Fatal("normalizer returned caller-owned message state")
	}
	normalized[2].ToolResult.ContextHints.Recovery.Reference["path"] = "mutated"
	if got := input[2].ToolResult.ContextHints.Recovery.Reference["path"]; got != "chapter.md" {
		t.Fatalf("normalizer aliased rich result metadata: %v", got)
	}
	if normalized[2].ToolResult.ResultRetention != agentschema.ToolResultProtected {
		t.Fatalf("normalizer applied retention policy: %#v", normalized[2].ToolResult)
	}
}

func TestNormalizeModelContextMessagesRepairsUniqueMissingResultDeterministically(t *testing.T) {
	call := contextNormalizerTestCall("text_tool_call_stable", "write", `{"path":"chapter.md"}`)
	input := []*agentschema.Message{
		agentschema.AssistantMessage("", []agentschema.ToolCall{call}),
		agentschema.UserMessage("continue"),
	}

	first, err := NormalizeModelContextMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeModelContextMessages(first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("normalization is not idempotent:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first) != 3 || first[0].ToolCalls[0].ID != call.ID {
		t.Fatalf("missing-result repair changed the stable call identity: %#v", first)
	}
	result := first[1]
	if result.Role != agentschema.ToolRole || result.ToolCallID != call.ID || result.ToolName != call.Function.Name ||
		result.ToolResult == nil || result.ToolResult.Status != agentschema.ToolResultError ||
		result.ToolResult.SyntheticReason != agentschema.ToolSyntheticEffectUnknown || !IsUnknownToolEffectResult(result.Content) {
		t.Fatalf("unexpected effect_unknown completion: %#v", result)
	}
	if result.ToolResult.ResultRetention != "" {
		t.Fatalf("normalizer assigned retention to a recovery result: %#v", result.ToolResult)
	}
}

func TestNormalizeModelContextMessagesDropsAmbiguousHalvesAtomically(t *testing.T) {
	valid := contextNormalizerTestCall("valid", "read", `{}`)
	missing := contextNormalizerTestCall("missing", "write", `{}`)
	invalid := contextNormalizerTestCall("invalid", "read", `{"path":`)
	duplicateA := contextNormalizerTestCall("duplicate-call", "read", `{}`)
	duplicateB := contextNormalizerTestCall("duplicate-call", "write", `{}`)
	duplicateResult := contextNormalizerTestCall("duplicate-result", "read", `{}`)
	validResult := agentschema.ToolMessage(agentschema.TextToolResult("rich valid result"), valid.ID, agentschema.WithToolName("read"))
	validResult.ToolResult.ResultRetention = agentschema.ToolResultProtected
	input := []*agentschema.Message{
		agentschema.AssistantMessage("useful narration", []agentschema.ToolCall{valid, missing, invalid, duplicateA, duplicateB, duplicateResult}),
		validResult,
		agentschema.ToolMessage(agentschema.TextToolResult("invalid result"), invalid.ID),
		agentschema.ToolMessage(agentschema.TextToolResult("ambiguous call result"), duplicateA.ID),
		agentschema.ToolMessage(agentschema.TextToolResult("first duplicate result"), duplicateResult.ID),
		agentschema.ToolMessage(agentschema.TextToolResult("second duplicate result"), duplicateResult.ID),
		agentschema.ToolMessage(agentschema.TextToolResult("orphan result"), "orphan"),
		agentschema.UserMessage("continue"),
	}

	normalized, err := NormalizeModelContextMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized) != 4 || normalized[0].Role != agentschema.Assistant || normalized[3].Role != agentschema.User {
		t.Fatalf("unexpected normalized transcript: %#v", normalized)
	}
	if got := normalized[0].ToolCalls; len(got) != 2 || got[0].ID != valid.ID || got[1].ID != missing.ID {
		t.Fatalf("ambiguous calls were not removed atomically: %#v", got)
	}
	if normalized[1].ToolCallID != missing.ID || !IsUnknownToolEffectResult(normalized[1].Content) {
		t.Fatalf("unique missing result was not repaired: %#v", normalized[1])
	}
	if normalized[2].ToolCallID != valid.ID || normalized[2].Content != validResult.Content ||
		normalized[2].ToolResult.ResultRetention != agentschema.ToolResultProtected {
		t.Fatalf("valid rich result was not preserved: %#v", normalized[2])
	}
}

func TestNormalizeModelContextMessagesPreservesRecoverableMalformedArguments(t *testing.T) {
	call := contextNormalizerTestCall("invalid-json", "read", `[`)
	result := agentschema.ToolMessage(
		agenttool.SyntheticToolResult(
			agentschema.ToolResultError,
			agentschema.ToolSyntheticInvalidArguments,
			`{"error":{"code":"invalid_arguments"}}`,
		),
		call.ID,
		agentschema.WithToolName(call.Function.Name),
	)
	input := []*agentschema.Message{agentschema.AssistantMessage("", []agentschema.ToolCall{call}), result}

	normalized, err := NormalizeModelContextMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized) != 2 || len(normalized[0].ToolCalls) != 1 ||
		normalized[0].ToolCalls[0].Function.Arguments != `{}` ||
		normalized[1].ToolResult == nil ||
		normalized[1].ToolResult.SyntheticReason != agentschema.ToolSyntheticInvalidArguments {
		t.Fatalf("recoverable invalid argument exchange was not preserved: %#v", normalized)
	}
}

func TestNormalizeModelContextMessagesRepairsMissingCallAndDropsLateOrphan(t *testing.T) {
	call := contextNormalizerTestCall("late-result", "write", `{}`)
	input := []*agentschema.Message{
		agentschema.AssistantMessage("", []agentschema.ToolCall{call}),
		agentschema.UserMessage("new turn"),
		agentschema.ToolMessage(agentschema.TextToolResult("late"), call.ID),
	}

	normalized, err := NormalizeModelContextMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized) != 3 || normalized[0].Role != agentschema.Assistant || normalized[1].ToolCallID != call.ID ||
		!IsUnknownToolEffectResult(normalized[1].Content) || normalized[2].Role != agentschema.User || normalized[2].Content != "new turn" {
		t.Fatalf("missing call and late orphan were not normalized independently: %#v", normalized)
	}
}

func TestNormalizeModelContextMessagesScopesCallIdentityToOneAssistantBatch(t *testing.T) {
	firstCall := contextNormalizerTestCall("provider-local-id", "read", `{"path":"one.md"}`)
	secondCall := contextNormalizerTestCall("provider-local-id", "read", `{"path":"two.md"}`)
	input := []*agentschema.Message{
		agentschema.AssistantMessage("", []agentschema.ToolCall{firstCall}),
		agentschema.ToolMessage(agentschema.TextToolResult("one"), firstCall.ID),
		agentschema.UserMessage("next"),
		agentschema.AssistantMessage("", []agentschema.ToolCall{secondCall}),
	}

	normalized, err := NormalizeModelContextMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized) != 5 || !reflect.DeepEqual(normalized[:4], input) ||
		normalized[4].ToolCallID != secondCall.ID || !IsUnknownToolEffectResult(normalized[4].Content) {
		t.Fatalf("provider-local call id reuse suppressed the later recovery result: %#v", normalized)
	}
}

func TestNormalizeModelContextMessagesRejectsIrreparableProtocol(t *testing.T) {
	_, err := NormalizeModelContextMessages([]*agentschema.Message{{Role: agentschema.RoleType("unsupported"), Content: "unsupported"}})
	if !errors.Is(err, ErrInvalidModelContextProtocol) {
		t.Fatalf("unsupported role error = %v, want %v", err, ErrInvalidModelContextProtocol)
	}
}

func contextNormalizerTestCall(id, name, arguments string) agentschema.ToolCall {
	return agentschema.ToolCall{
		ID: id, Type: "function", Function: agentschema.FunctionCall{Name: name, Arguments: arguments},
	}
}
