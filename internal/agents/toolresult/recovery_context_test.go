package toolresult_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"denova/config"
	agentcontext "denova/internal/agents/context"
	agentconversation "denova/internal/agents/conversation"
	agentrun "denova/internal/agents/run"
	"denova/internal/agents/session"
	"denova/internal/agents/toolresult"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestIncompleteToolExchangeGetsStableUnknownEffectResult(t *testing.T) {
	t.Parallel()

	messages := []*agentschema.Message{
		agentschema.UserMessage("update the chapter"),
		agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "call-write", Function: agentschema.FunctionCall{Name: "write", Arguments: `{"path":"chapter.md"}`},
		}}),
		agentschema.UserMessage("continue"),
	}
	policy := toolresult.ContextPolicy{Enabled: false}
	first := toolresult.ApplyContextPolicy(messages, policy)
	second := toolresult.ApplyContextPolicy(first, policy)
	if len(first) != 4 || len(second) != len(first) {
		t.Fatalf("recovered model context lengths = %d then %d, want stable four-message exchange", len(first), len(second))
	}
	if first[1].Role != agentschema.Assistant || len(first[1].ToolCalls) != 1 || first[1].ToolCalls[0].ID != "call-write" {
		t.Fatalf("recovered tool call = %#v", first[1])
	}
	result := first[2]
	if result.Role != agentschema.ToolRole || result.ToolCallID != "call-write" || result.ToolName != "write" {
		t.Fatalf("synthetic tool result identity = %#v", result)
	}
	if result.ToolResult == nil || result.ToolResult.Status != agentschema.ToolResultError ||
		result.ToolResult.SyntheticReason != agentschema.ToolSyntheticEffectUnknown {
		t.Fatalf("synthetic tool result summary = %#v", result.ToolResult)
	}
	var payload struct {
		Schema         string `json:"schema"`
		Status         string `json:"status"`
		AutomaticRetry bool   `json:"automatic_retry"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("synthetic tool result is not provider-neutral JSON: %v", err)
	}
	if payload.Schema != "agent.tool_result.recovery.v1" || payload.Status != "effect_unknown" || payload.AutomaticRetry {
		t.Fatalf("synthetic tool recovery payload = %#v", payload)
	}
	if second[2].Content != result.Content {
		t.Fatalf("synthetic recovery result changed across assembly: %q != %q", second[2].Content, result.Content)
	}
}

func TestIncompleteParallelToolCallsCompleteOnlyMissingResults(t *testing.T) {
	t.Parallel()

	messages := []*agentschema.Message{
		agentschema.AssistantMessage("", []agentschema.ToolCall{
			{ID: "call-read", Function: agentschema.FunctionCall{Name: "read", Arguments: `{"path":"a.md"}`}},
			{ID: "call-write", Function: agentschema.FunctionCall{Name: "write", Arguments: `{"path":"b.md"}`}},
		}),
		agentschema.ToolMessage(agentschema.TextToolResult("read result"), "call-read", agentschema.WithToolName("read")),
	}
	got := toolresult.ApplyContextPolicy(messages, toolresult.ContextPolicy{Enabled: true})
	if len(got) != 3 {
		t.Fatalf("completed parallel exchange = %#v", got)
	}
	counts := map[string]int{}
	for _, message := range got {
		if message.Role == agentschema.ToolRole {
			counts[message.ToolCallID]++
		}
	}
	if counts["call-read"] != 1 || counts["call-write"] != 1 {
		t.Fatalf("tool result counts = %#v, want one per call", counts)
	}
}

func TestContextPolicyPreservesRecoverableMalformedArguments(t *testing.T) {
	t.Parallel()

	result := agenttool.SyntheticToolResult(
		agentschema.ToolResultError,
		agentschema.ToolSyntheticInvalidArguments,
		`{"error":{"code":"invalid_arguments","received_arguments":"["}}`,
	)
	messages := []*agentschema.Message{
		agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "malformed-call", Type: "function",
			Function: agentschema.FunctionCall{Name: "read", Arguments: `[`},
		}}),
		agentschema.ToolMessage(result, "malformed-call", agentschema.WithToolName("read")),
	}

	got := toolresult.ApplyContextPolicy(messages, toolresult.ContextPolicy{Enabled: true})
	if len(got) != 2 || len(got[0].ToolCalls) != 1 {
		t.Fatalf("recoverable malformed exchange = %#v", got)
	}
	if arguments := got[0].ToolCalls[0].Function.Arguments; arguments != `{}` {
		t.Fatalf("recoverable malformed arguments = %q, want canonical empty object", arguments)
	}
	if got[1].ToolResult == nil || got[1].ToolResult.SyntheticReason != agentschema.ToolSyntheticInvalidArguments ||
		!strings.Contains(got[1].Content, `"received_arguments":"["`) {
		t.Fatalf("recoverable malformed result = %#v", got[1])
	}
}

func TestIncompleteReusedCallIDCompletesOnlyItsAssistantBatch(t *testing.T) {
	t.Parallel()

	messages := []*agentschema.Message{
		agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "provider-local", Type: "function",
			Function: agentschema.FunctionCall{Name: "write", Arguments: `{"path":"one.md"}`},
		}}),
		agentschema.UserMessage("next turn"),
		agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "provider-local", Type: "function",
			Function: agentschema.FunctionCall{Name: "read", Arguments: `{"path":"two.md"}`},
		}}),
		agentschema.ToolMessage(agentschema.TextToolResult("second result"), "provider-local"),
	}

	got := toolresult.ApplyContextPolicy(messages, toolresult.ContextPolicy{Enabled: true})
	if len(got) != 5 || got[0].Role != agentschema.Assistant || got[1].ToolCallID != "provider-local" ||
		!toolresult.IsUnknownEffectResult(got[1].Content) || got[2].Role != agentschema.User ||
		got[3].Role != agentschema.Assistant || got[4].Content != "second result" {
		t.Fatalf("provider-local ID reuse suppressed the missing-result recovery: %#v", got)
	}
	if got[1].ToolName != "write" || got[4].ToolName != "read" {
		t.Fatalf("reused ID results were paired to the wrong assistant batch: %#v", got)
	}
}

func TestCanonicalSessionHistoryProjectsUnknownToolEffectIntoNextModelContext(t *testing.T) {
	t.Parallel()

	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Create("tool recovery context")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(agentschema.UserMessage("update the chapter")); err != nil {
		t.Fatal(err)
	}
	if err := sess.AppendContextMessage(agentschema.AssistantMessage("", []agentschema.ToolCall{{
		ID: "canonical-call", Function: agentschema.FunctionCall{Name: "write", Arguments: `{"path":"chapter.md"}`},
	}})); err != nil {
		t.Fatal(err)
	}
	conversation := agentconversation.NewSessionConversationForAgent(sess, &config.Config{}, agentrun.AgentKindIDE)
	projection, err := conversation.AssembleModelContext(context.Background(), "", agentcontext.ModelContextInput{
		UserMessage: "next request", Budget: conversation.ModelContextBudget(),
	})
	if err != nil {
		t.Fatal(err)
	}
	messages := projection.Messages[:len(projection.Messages)-1]
	if len(messages) != 3 || messages[1].Role != agentschema.Assistant || len(messages[1].ToolCalls) != 1 ||
		messages[2].Role != agentschema.ToolRole || messages[2].ToolCallID != "canonical-call" ||
		!toolresult.IsUnknownEffectResult(messages[2].Content) {
		t.Fatalf("canonical next model context did not contain a complete unknown-effect exchange: %#v", messages)
	}
}
