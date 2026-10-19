package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageLegacyWireGolden(t *testing.T) {
	for _, legacy := range []string{
		`{"role":"user","content":"legacy"}`,
		`{"role":"assistant","content":"legacy","response_meta":{"usage":{"prompt_tokens":20,"prompt_token_details":{"cached_tokens":0},"completion_tokens":7,"total_tokens":27,"completion_token_details":{}}}}`,
	} {
		var message Message
		if err := json.Unmarshal([]byte(legacy), &message); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(&message)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != legacy {
			t.Fatalf("wire changed:\n got %s\nwant %s", encoded, legacy)
		}
	}
}

func TestMessageFullWireRoundTripAndClone(t *testing.T) {
	index := 1
	message := &Message{
		Role:                     Assistant,
		Content:                  "answer",
		MultiContent:             []json.RawMessage{json.RawMessage(`{"type":"text","text":"old"}`)},
		UserInputMultiContent:    []json.RawMessage{json.RawMessage(`{"type":"image_url","vendor":{"x":1}}`)},
		AssistantGenMultiContent: []json.RawMessage{json.RawMessage(`{"type":"audio_url","unknown":[1,2]}`)},
		Name:                     "writer",
		ToolCalls: []ToolCall{{
			Index: &index,
			ID:    "call-1",
			Type:  "function",
			Function: FunctionCall{
				Name:      "lookup",
				Arguments: `{"q":"x"}`,
			},
			Extra: map[string]any{"provider": "p"},
		}},
		ToolCallID: "parent-call",
		ToolName:   "lookup",
		ToolResult: &ToolResultSummary{
			Status: ToolResultSuccess, ResultRetention: ToolResultEagerCandidate,
			ContextHints: &ToolResultContextHints{
				Recovery:     ToolResultRecoveryHint{Kind: ToolResultRecoveryRead, Reference: map[string]any{"path": "chapter.md"}},
				ContextValue: ToolResultContextDiscardable, SupersessionKey: "read:chapter.md",
			},
			ArtifactPersistence: &ToolArtifactPersistence{Attempted: true, Complete: true},
			Artifacts: []ToolArtifactRef{{
				ID: "artifact-1", ReadablePath: "/workspace/.denova/sessions/artifact.log",
				ContentType: "text/plain", EstimatedBytes: 12, SHA256: strings.Repeat("a", 64),
			}},
		},
		ReasoningContent: "reason",
		ResponseMeta: &ResponseMeta{
			FinishReason: "tool_calls",
			InputEstimate: &ModelInputEstimate{
				Tokens: 8, Model: CapabilityIdentity{Kind: "test.model", Version: 1},
			},
			Usage: &TokenUsage{
				PromptTokens:       10,
				PromptTokenDetails: PromptTokenDetails{CachedTokens: 2},
				CompletionTokens:   3,
				TotalTokens:        13,
				CompletionTokensDetails: CompletionTokensDetails{
					ReasoningTokens: 1,
				},
			},
			LogProbs: &LogProbs{Content: []LogProb{{
				Token: "a", LogProb: -0.1, Bytes: []int64{97},
				TopLogProbs: []TopLogProb{{Token: "a", LogProb: -0.1}},
			}}},
		},
		Extra: map[string]any{"nested": map[string]any{"items": []any{"a", "b"}}},
	}

	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"role":"assistant"`, `"content":"answer"`, `"multi_content"`,
		`"user_input_multi_content"`, `"assistant_output_multi_content"`,
		`"tool_calls"`, `"tool_result"`, `"response_meta"`, `"reasoning_content"`,
	} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("full wire omitted %s: %s", field, encoded)
		}
	}
	var decoded Message
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	reencoded, err := json.Marshal(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(reencoded) {
		t.Fatalf("round trip changed wire:\nfirst  %s\nsecond %s", encoded, reencoded)
	}

	clone := message.Clone()
	clone.MultiContent[0][0] = '['
	clone.ToolCalls[0].Function.Name = "changed"
	clone.ToolCalls[0].Extra["provider"] = "changed"
	clone.ToolResult.Artifacts[0].ReadablePath = "changed"
	clone.ToolResult.ContextHints.Recovery.Reference["path"] = "changed"
	clone.ToolResult.ArtifactPersistence.Complete = false
	clone.Extra["nested"].(map[string]any)["items"].([]any)[0] = "changed"
	clone.ResponseMeta.Usage.TotalTokens = 99
	clone.ResponseMeta.InputEstimate.Tokens = 99
	if message.MultiContent[0][0] == '[' || message.ToolCalls[0].Function.Name != "lookup" ||
		message.ToolCalls[0].Extra["provider"] != "p" ||
		message.ToolResult.Artifacts[0].ReadablePath != "/workspace/.denova/sessions/artifact.log" ||
		message.ToolResult.ContextHints.Recovery.Reference["path"] != "chapter.md" ||
		!message.ToolResult.ArtifactPersistence.Complete ||
		message.Extra["nested"].(map[string]any)["items"].([]any)[0] != "a" ||
		message.ResponseMeta.Usage.TotalTokens != 13 || message.ResponseMeta.InputEstimate.Tokens != 8 {
		t.Fatal("Clone shared mutable storage with the source")
	}
}

func TestConcatMessagesRejectsConflictsWithoutMutatingAssembler(t *testing.T) {
	tests := []struct {
		name   string
		first  ToolCall
		second ToolCall
	}{
		{name: "id", first: ToolCall{ID: "a"}, second: ToolCall{ID: "b"}},
		{name: "type", first: ToolCall{Type: "function"}, second: ToolCall{Type: "other"}},
		{name: "name", first: ToolCall{Function: FunctionCall{Name: "a"}}, second: ToolCall{Function: FunctionCall{Name: "b"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index := 0
			test.first.Index = &index
			test.second.Index = &index
			assembler := NewMessageAssembler()
			if err := assembler.Append(&Message{Role: Assistant, Content: "ok", ToolCalls: []ToolCall{test.first}}); err != nil {
				t.Fatal(err)
			}
			if err := assembler.Append(&Message{ToolCalls: []ToolCall{test.second}}); err == nil {
				t.Fatal("expected conflict")
			}
			message, err := assembler.Message()
			if err != nil {
				t.Fatal(err)
			}
			if message.Content != "ok" || len(message.ToolCalls) != 1 {
				t.Fatalf("failed append mutated assembler: %#v", message)
			}
		})
	}

	if _, err := ConcatMessages([]*Message{{Role: User}, {Role: Assistant}}); err == nil {
		t.Fatal("expected role conflict")
	}
}
