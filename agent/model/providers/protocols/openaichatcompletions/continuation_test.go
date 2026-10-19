package openaichatcompletions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// Exercise the real SDK transport, stream assembly and durable message shape.
// The endpoint rejects the same missing signatures as Gemini's tool loop.
func TestSignedToolCallsContinueAfterMessageRecovery(t *testing.T) {
	for _, mode := range []string{"generate", "stream", "stream_late_signature"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var body struct {
					Stream   bool             `json:"stream"`
					Messages []map[string]any `json:"messages"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Error(err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				step := int(requests.Add(1))
				var calls []map[string]any
				for _, message := range body.Messages {
					if raw, ok := message["tool_calls"].([]any); ok {
						for _, call := range raw {
							calls = append(calls, call.(map[string]any))
						}
					}
				}
				if step > 1 {
					wantCalls := 2
					if step == 3 {
						wantCalls = 3
					}
					if len(calls) != wantCalls {
						t.Errorf("step %d: calls = %#v", step, calls)
						writer.WriteHeader(http.StatusBadRequest)
						return
					}
					for index, call := range calls {
						var want any
						if index != 1 {
							want = map[string]any{"google": map[string]any{"thought_signature": fmt.Sprintf("signature-%d", index)}}
						}
						if !reflect.DeepEqual(call["extra_content"], want) {
							t.Errorf("step %d: call %d extra_content = %#v, want %#v", step, index, call["extra_content"], want)
							writer.Header().Set("Content-Type", "application/json")
							writer.WriteHeader(http.StatusBadRequest)
							_, _ = io.WriteString(writer, `{"error":{"message":"Function call is missing a thought_signature","type":"invalid_request_error"}}`)
							return
						}
					}
				}
				responseCalls := []map[string]any{}
				if step < 3 {
					indices := []int{0, 1}
					if step == 2 {
						indices = []int{2}
					}
					for _, index := range indices {
						call := map[string]any{
							"id": fmt.Sprintf("call-%d", index), "type": "function",
							"function": map[string]any{"name": "lookup", "arguments": fmt.Sprintf(`{"value":%d}`, index)},
						}
						if index != 1 {
							call["extra_content"] = map[string]any{"google": map[string]any{"thought_signature": fmt.Sprintf("signature-%d", index)}}
						}
						responseCalls = append(responseCalls, call)
					}
				}
				finish, content := "tool_calls", ""
				if step == 3 {
					finish, content = "stop", "All lookups completed."
				}
				if !body.Stream {
					writer.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(writer).Encode(map[string]any{"choices": []any{map[string]any{
						"index": 0, "finish_reason": finish,
						"message": map[string]any{"role": "assistant", "content": content, "tool_calls": responseCalls},
					}}})
					return
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				late := []map[string]any{}
				for index, call := range responseCalls {
					call["index"] = index
					if mode == "stream_late_signature" && call["extra_content"] != nil {
						late = append(late, map[string]any{"index": index, "extra_content": call["extra_content"]})
						delete(call, "extra_content")
					}
				}
				for _, delta := range []map[string]any{
					{"role": "assistant", "content": content, "tool_calls": responseCalls},
					{"tool_calls": late},
				} {
					frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": ""}}})
					_, _ = fmt.Fprintf(writer, "data: %s\n\n", frame)
				}
				_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", finish)
			}))
			defer server.Close()
			model, err := newTestModel(t.Context(), &providers.ModelConfig{
				Provider: providers.ProviderGoogle, Model: "gemini-3.1-pro-preview", BaseURL: server.URL, HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			history := []*agentschema.Message{agentschema.UserMessage("Look up both values, then verify them.")}
			for step := 0; step < 3; step++ {
				var message *agentschema.Message
				if mode == "generate" {
					message, err = model.Generate(t.Context(), history)
				} else {
					var stream *agentstream.StreamReader[*agentschema.Message]
					stream, err = model.Stream(t.Context(), history)
					if err == nil {
						message, err = agentmodel.ConcatMessageStream(stream)
						stream.Close()
					}
				}
				if err != nil {
					t.Fatalf("step %d: %v", step, err)
				}
				// Product journals retain opaque continuation and discard telemetry.
				message.Extra = providers.ContinuationExtra(message.Extra)
				data, err := json.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				var restored agentschema.Message
				if err := json.Unmarshal(data, &restored); err != nil {
					t.Fatal(err)
				}
				history = append(history, &restored)
				for _, call := range restored.ToolCalls {
					if call.Extra != nil {
						t.Fatal("opaque signature leaked into public tool-call metadata")
					}
					history = append(history, agentschema.ToolMessage(agentschema.TextToolResult("lookup completed"), call.ID))
				}
				if step == 2 && restored.Content != "All lookups completed." {
					t.Fatalf("final message = %#v", restored)
				}
			}
		})
	}
}

func TestSignedContinuationRespectsRoutingAndProjectedToolCalls(t *testing.T) {
	requests := make(chan map[string]any, 1)
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if count.Add(1) == 1 {
			_, _ = io.WriteString(writer, `{"choices":[{"index":0,"message":{"role":"assistant","content":"Looking up a value","extra_content":{"google":{"thought_signature":"message-state"}},"tool_calls":[{"id":"signed","type":"function","function":{"name":"lookup","arguments":"{\"value\":1}"},"extra_content":{"google":{"thought_signature":"call-state"}}},{"id":"removed","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`)
			return
		}
		requests <- body
		_, _ = io.WriteString(writer, `{"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	config := providers.ModelConfig{Provider: providers.ProviderGoogle, Model: "gemini-3.1-pro-preview", BaseURL: server.URL, HTTPClient: server.Client()}
	model, err := newTestModel(t.Context(), &config)
	if err != nil {
		t.Fatal(err)
	}
	message, err := model.Generate(t.Context(), []*agentschema.Message{agentschema.UserMessage("Look up a value")})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"projected", "removed", "other_model", "other_provider", "other_endpoint"} {
		t.Run(mode, func(t *testing.T) {
			projected := message.Clone()
			projected.ToolCalls = projected.ToolCalls[:1]
			projected.ToolCalls[0].Function.Arguments = `{"value":2}`
			selected := config
			switch mode {
			case "projected":
			case "removed":
				projected.ToolCalls = nil
			case "other_model":
				selected.Model = "gemini-2.5-pro"
			case "other_provider":
				selected.Provider = providers.ProviderOpenAICompatible
			case "other_endpoint":
				selected.BaseURL += "/other"
			}
			current, err := newTestModel(context.Background(), &selected)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := current.Generate(t.Context(), []*agentschema.Message{projected}); err != nil {
				t.Fatal(err)
			}
			body := <-requests
			assistant := body["messages"].([]any)[0].(map[string]any)
			want := map[string]any{"role": "assistant", "content": "Looking up a value"}
			if mode == "projected" || mode == "removed" {
				want["extra_content"] = map[string]any{"google": map[string]any{"thought_signature": "message-state"}}
			}
			if mode != "removed" {
				call := map[string]any{"id": "signed", "type": "function", "function": map[string]any{"name": "lookup", "arguments": `{"value":2}`}}
				if mode == "projected" {
					call["extra_content"] = map[string]any{"google": map[string]any{"thought_signature": "call-state"}}
				}
				want["tool_calls"] = []any{call}
			}
			if !reflect.DeepEqual(assistant, want) {
				t.Fatalf("projected request = %#v, want %#v", assistant, want)
			}
		})
	}
}
