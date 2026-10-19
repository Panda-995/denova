package model

import (
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestConcatMessagesInterleavedToolCallsAndUsageTail(t *testing.T) {
	zero, one := 0, 1
	estimate := agentschema.ModelInputEstimate{Version: InputEstimateVersion, Tokens: 16, Model: agentschema.CapabilityIdentity{Kind: "test.model", Version: 1}}
	chunks := []*agentschema.Message{
		{
			Role: agentschema.Assistant, Content: "hel", ReasoningContent: "rea",
			ToolCalls: []agentschema.ToolCall{
				{Index: &one, ID: "call-b", Type: "function", Function: agentschema.FunctionCall{Name: "beta", Arguments: `{"b":`}, Extra: map[string]any{"trace": "x"}},
				{Index: &zero, ID: "call-a", Type: "function", Function: agentschema.FunctionCall{Name: "alpha", Arguments: `{"a":`}, Extra: map[string]any{"nested": map[string]any{"n": 1}}},
			},
		},
		{
			Content: "lo", ReasoningContent: "son",
			ToolCalls: []agentschema.ToolCall{
				{Index: &zero, Function: agentschema.FunctionCall{Arguments: `1}`}, Extra: map[string]any{"nested": map[string]any{"m": true}}},
				{Index: &one, Function: agentschema.FunctionCall{Arguments: `2}`}, Extra: map[string]any{"trace": "y"}},
			},
		},
		{ResponseMeta: &agentschema.ResponseMeta{FinishReason: "tool_calls"}},
		{
			ResponseMeta: &agentschema.ResponseMeta{
				InputEstimate: &estimate,
				Usage: &agentschema.TokenUsage{
					PromptTokens:       20,
					PromptTokenDetails: agentschema.PromptTokenDetails{CachedTokens: 5},
					CompletionTokens:   7,
					TotalTokens:        27,
					CompletionTokensDetails: agentschema.CompletionTokensDetails{
						ReasoningTokens: 3,
					},
				},
			},
		},
	}
	message, err := agentschema.ConcatMessages(chunks)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "hello" || message.ReasoningContent != "reason" {
		t.Fatalf("unexpected concatenation: %#v", message)
	}
	if len(message.ToolCalls) != 2 {
		t.Fatalf("got %d tool calls", len(message.ToolCalls))
	}
	if *message.ToolCalls[0].Index != 0 || message.ToolCalls[0].Function.Arguments != `{"a":1}` {
		t.Fatalf("call 0 not source merged: %#v", message.ToolCalls[0])
	}
	if *message.ToolCalls[1].Index != 1 || message.ToolCalls[1].Function.Arguments != `{"b":2}` {
		t.Fatalf("call 1 not source merged: %#v", message.ToolCalls[1])
	}
	if message.ToolCalls[1].Type != "function" || message.ToolCalls[1].Extra["trace"] != "xy" {
		t.Fatalf("tool type/extra lost: %#v", message.ToolCalls[1])
	}
	if message.ResponseMeta == nil || message.ResponseMeta.Usage == nil ||
		message.ResponseMeta.FinishReason != "tool_calls" || message.ResponseMeta.Usage.TotalTokens != 27 ||
		message.ResponseMeta.InputEstimate == nil || *message.ResponseMeta.InputEstimate != estimate {
		t.Fatalf("usage-only tail lost: %#v", message.ResponseMeta)
	}
}
