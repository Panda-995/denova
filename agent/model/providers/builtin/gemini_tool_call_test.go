package builtin_test

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
	"time"

	"github.com/alfredxw/denova/agent"
	"github.com/alfredxw/denova/agent/model/providers"
	"github.com/alfredxw/denova/agent/model/providers/builtin"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	sessionfile "github.com/alfredxw/denova/agent/session/file"
	agenttool "github.com/alfredxw/denova/agent/tool"
	"github.com/alfredxw/denova/agent/tool/permission"
	"google.golang.org/genai"
)

// Run real Native tools through the Google preset, then reopen the disk-backed
// Session mid-turn. Only provider generation and signature validation are mocked.
func TestGeminiToolLoopPreservesSignaturesAcrossReopen(t *testing.T) {
	for _, protocol := range []providers.ProtocolID{providers.ProtocolOpenAIChatCompletions, providers.ProtocolGoogleGenerativeAI} {
		for _, suspend := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/suspend=%t", protocol, suspend), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				paused := make(chan struct{})
				var pauseNext atomic.Bool
				pauseNext.Store(suspend)
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					var body struct {
						Messages []struct {
							Role       string `json:"role"`
							ToolCallID string `json:"tool_call_id"`
							ToolCalls  []struct {
								ID           string         `json:"id"`
								ExtraContent map[string]any `json:"extra_content"`
							} `json:"tool_calls"`
						} `json:"messages"`
					}
					data, _ := io.ReadAll(request.Body)
					if protocol == providers.ProtocolGoogleGenerativeAI {
						if request.URL.Path != "/v1beta/models/gemini-3.1-pro-preview:streamGenerateContent" {
							t.Errorf("native path = %s", request.URL.Path)
						}
						var native struct{ Contents []*genai.Content }
						if err := json.Unmarshal(data, &native); err != nil {
							t.Error(err)
							writer.WriteHeader(400)
							return
						}
						messages := []any{}
						for _, content := range native.Contents {
							for _, part := range content.Parts {
								if call := part.FunctionCall; call != nil {
									var extra any
									if len(part.ThoughtSignature) > 0 {
										extra = map[string]any{"google": map[string]any{"thought_signature": string(part.ThoughtSignature)}}
									}
									messages = append(messages, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": call.ID, "extra_content": extra}}})
								}
								if part.FunctionResponse != nil {
									messages = append(messages, map[string]any{"role": "tool", "tool_call_id": part.FunctionResponse.ID})
								}
							}
						}
						data, _ = json.Marshal(map[string]any{"messages": messages})
					}
					if err := json.Unmarshal(data, &body); err != nil {
						t.Error(err)
						writer.WriteHeader(http.StatusBadRequest)
						return
					}
					calls, results := 0, 0
					for _, message := range body.Messages {
						if message.Role == "tool" {
							results++
						}
						for _, call := range message.ToolCalls {
							calls++
							var expected map[string]any
							if call.ID != "parallel" {
								expected = map[string]any{"google": map[string]any{"thought_signature": "signature-" + call.ID}}
							}
							if !reflect.DeepEqual(call.ExtraContent, expected) {
								t.Errorf("call %s signature = %#v, want %#v", call.ID, call.ExtraContent, expected)
								writer.Header().Set("Content-Type", "application/json")
								writer.WriteHeader(http.StatusBadRequest)
								_, _ = io.WriteString(writer, `{"error":{"message":"Function call is missing a thought_signature"}}`)
								return
							}
						}
					}
					if calls != results {
						t.Errorf("unpaired history: %d calls, %d results", calls, results)
						writer.WriteHeader(http.StatusBadRequest)
						return
					}
					if calls == 2 && pauseNext.CompareAndSwap(true, false) {
						close(paused)
						<-request.Context().Done()
						return
					}
					writer.Header().Set("Content-Type", "text/event-stream")
					if calls == 3 {
						if protocol == providers.ProtocolGoogleGenerativeAI {
							_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Verified all values.\"}]},\"finishReason\":\"STOP\"}]}\n\n")
							return
						}
						_, _ = io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Verified all values.\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
						return
					}
					ids := []string{"first", "parallel"}
					if calls == 2 {
						ids = []string{"next"}
					}
					if protocol == providers.ProtocolGoogleGenerativeAI {
						parts := []*genai.Part{}
						for _, id := range ids {
							part := &genai.Part{FunctionCall: &genai.FunctionCall{ID: id, Name: "lookup", Args: map[string]any{}}}
							if id != "parallel" {
								part.ThoughtSignature = []byte("signature-" + id)
							}
							parts = append(parts, part)
						}
						frame, _ := json.Marshal(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Role: "model", Parts: parts}, FinishReason: genai.FinishReasonStop}}})
						_, _ = fmt.Fprintf(writer, "data: %s\n\n", frame)
						return
					}
					toolCalls, signatures := []any{}, []any{}
					for index, id := range ids {
						toolCalls = append(toolCalls, map[string]any{
							"index": index, "id": id, "type": "function",
							"function": map[string]any{"name": "lookup", "arguments": `{}`},
						})
						if id != "parallel" {
							signatures = append(signatures, map[string]any{
								"index": index, "extra_content": map[string]any{"google": map[string]any{"thought_signature": "signature-" + id}},
							})
						}
					}
					for _, delta := range []map[string]any{{"role": "assistant", "tool_calls": toolCalls}, {"tool_calls": signatures}} {
						frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
						_, _ = fmt.Fprintf(writer, "data: %s\n\n", frame)
					}
					_, _ = io.WriteString(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
				}))
				defer server.Close()
				var executions atomic.Int32
				tool, err := agenttool.InferTool("lookup", "Read a value", func(context.Context, struct{}) (string, error) {
					executions.Add(1)
					return "42", nil
				})
				if err != nil {
					t.Fatal(err)
				}
				tools, err := agenttool.StaticTools(agenttool.ToolDefinition{Tool: tool, Descriptor: agenttool.ToolDescriptor{
					Source: agenttool.ToolSourceRead, Execution: agenttool.ToolExecutionParallelRead,
					MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone,
					Recovery: agenttool.ToolRecoveryReadOnly, ResultProjection: agentschema.ToolResultBoundedModelContext,
					ResultRetention: agentschema.ToolResultProtected, Steering: agenttool.SteeringFinishCurrent, MaxResultBytes: 1024,
				}})
				if err != nil {
					t.Fatal(err)
				}
				config := providers.ModelConfig{Provider: providers.ProviderGoogle, Protocol: protocol, APIKey: "test-key", Model: "gemini-3.1-pro-preview", BaseURL: server.URL, HTTPClient: server.Client()}
				definition := agent.Definition{Name: "gemini", Model: builtin.Model(config), Tools: tools, Permission: permission.FullAccess()}
				root := t.TempDir()
				store, err := sessionfile.New(root)
				if err != nil {
					t.Fatal(err)
				}
				owner, err := agent.New(ctx, definition, agent.WithSessionStore(store))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = owner.Close(context.Background()) }()
				conversation, err := owner.Session(ctx, agentsession.Named("signed-tool-turn"))
				if err != nil {
					t.Fatal(err)
				}
				run, err := conversation.Run(ctx, agent.Text("Look up both values, then verify them."))
				if err != nil {
					t.Fatal(err)
				}
				if suspend {
					select {
					case <-paused:
					case <-ctx.Done():
						t.Fatal("tool loop did not reach the continuation request")
					}
					if _, err := conversation.SuspendAndClose(ctx, agent.SuspendRequest{RunID: run.ID(), IdempotencyKey: "pause"}); err != nil {
						t.Fatal(err)
					}
					if err := owner.Close(ctx); err != nil {
						t.Fatal(err)
					}
					store, err = sessionfile.New(root)
					if err != nil {
						t.Fatal(err)
					}
					definition.Model = builtin.Model(config)
					owner, err = agent.New(ctx, definition, agent.WithSessionStore(store))
					if err != nil {
						t.Fatal(err)
					}
					conversation, err = owner.Session(ctx, agentsession.Named("signed-tool-turn"))
					if err != nil {
						t.Fatal(err)
					}
					run, err = conversation.ResumeRun(ctx, agent.ResumeRequest{RunID: run.ID(), IdempotencyKey: "resume"})
					if err != nil {
						t.Fatal(err)
					}
				}
				result, err := run.Wait(ctx)
				if err != nil || result.Status != agentschema.ResultCompleted {
					t.Fatalf("run result = %+v, error = %v", result, err)
				}
				if got := executions.Load(); got != 3 {
					t.Fatalf("tool executions = %d, want 3 without replaying completed tools; result=%+v", got, result)
				}
			})
		}
	}
}
