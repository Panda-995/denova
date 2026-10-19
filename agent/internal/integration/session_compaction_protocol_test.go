package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	agentcontext "github.com/alfredxw/denova/agent/context"
	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	"github.com/alfredxw/denova/agent/model/providers/protocols/openaichatcompletions"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type strictCompactionContext struct{}

func (strictCompactionContext) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.strict-compaction-context", Version: 1}
}

func (strictCompactionContext) Materialize(context.Context, agentcontext.ContextRequest) ([]agentschema.ContextFragment, error) {
	return []agentschema.ContextFragment{
		{Source: "project.creator", Purpose: "creative instructions", Resource: "CREATOR.md", Stability: agentschema.ContextStablePrefix, Placement: agentschema.ContextLeadingMessage, Role: agentschema.User, Content: "Preserve the stated budget.", HardLimit: 1024},
		{Source: "project.workspace", Purpose: "stable workspace sources", Resource: "sources", Stability: agentschema.ContextStablePrefix, Placement: agentschema.ContextLeadingMessage, Role: agentschema.User, Content: "Verify sources through the evidence tool.", HardLimit: 1024},
	}, nil
}

// Exercise the real adapter for both streaming turns and non-streaming summary
// forks. The endpoint enforces the single leading system rule of strict templates.
func newStrictCompactionChatModel(t *testing.T, engine *incrementalModel) agentmodel.ToolCallingChatModel {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var input struct {
			Messages []*agentschema.Message `json:"messages"`
			Stream   bool                   `json:"stream"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Errorf("decode Chat Completions request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		for index, message := range input.Messages {
			if message.Role == agentschema.System && index != 0 {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(writer, `{"error":{"message":"System message must be at the beginning.","type":"invalid_request_error"}}`)
				return
			}
		}
		message, err := engine.Generate(request.Context(), input.Messages)
		if err != nil {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]any{"message": err.Error(), "type": "invalid_request_error"}})
			return
		}
		if input.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			chunk, err := json.Marshal(map[string]any{"id": "compaction", "choices": []any{map[string]any{"index": 0, "delta": message}}})
			if err != nil {
				t.Errorf("encode Chat Completions chunk: %v", err)
				return
			}
			_, _ = fmt.Fprintf(writer, "data: %s\n\ndata: [DONE]\n\n", chunk)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"id": "compaction", "choices": []any{map[string]any{"index": 0, "message": message}}})
	}))
	t.Cleanup(server.Close)
	model, err := openaichatcompletions.NewAdapter().New(t.Context(), providers.ModelConfig{
		Provider: providers.ProviderOpenAICompatible, Protocol: providers.ProtocolOpenAIChatCompletions,
		BaseURL: server.URL + "/v1", Model: "strict-test-model", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return model
}
