package chat

import (
	"context"
	"testing"

	"denova/internal/agents/toolresult"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestModelContextMiddlewaresContainProjectionThenNormalizer(t *testing.T) {
	middlewares := NewModelContextMiddlewares(toolresult.ContextPolicy{Enabled: true})
	if len(middlewares) != 2 {
		t.Fatalf("model context middleware count = %d, want 2", len(middlewares))
	}
	if _, ok := middlewares[0].(*modelHistoryProjectionMiddleware); !ok {
		t.Fatalf("first model context middleware = %T, want history projection", middlewares[0])
	}
	if _, ok := middlewares[1].(*contextNormalizerMiddleware); !ok {
		t.Fatalf("second model context middleware = %T, want normalizer", middlewares[1])
	}
}

func TestDefaultMaxTokensMiddlewareAppliesProfileCapUnlessCallOverridesIt(t *testing.T) {
	middleware := NewDefaultMaxTokensMiddleware(4096)

	_, defaulted, err := middleware.BeforeModelCall(context.Background(), &agentmodel.ModelCall{}, &agentmiddleware.ModelContext{})
	if err != nil {
		t.Fatal(err)
	}
	defaultOptions := agentmodel.GetCommonOptions(&agentmodel.Options{}, defaulted.Options...)
	if defaultOptions.MaxTokens == nil || *defaultOptions.MaxTokens != 4096 {
		t.Fatalf("default max tokens = %#v, want 4096", defaultOptions.MaxTokens)
	}

	_, overridden, err := middleware.BeforeModelCall(context.Background(), &agentmodel.ModelCall{
		Options: []agentmodel.ModelOption{agentmodel.WithMaxTokens(512)},
	}, &agentmiddleware.ModelContext{})
	if err != nil {
		t.Fatal(err)
	}
	overriddenOptions := agentmodel.GetCommonOptions(&agentmodel.Options{}, overridden.Options...)
	if overriddenOptions.MaxTokens == nil || *overriddenOptions.MaxTokens != 512 {
		t.Fatalf("explicit max tokens = %#v, want 512", overriddenOptions.MaxTokens)
	}
}

func TestModelHistoryProjectionHidesDisabledSettledToolsButPreservesProviderReasoning(t *testing.T) {
	call := agentschema.ToolCall{ID: "historical", Type: "function", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`}}
	current := agentschema.ToolCall{ID: "current", Type: "function", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`}}
	middleware := NewModelHistoryProjectionMiddleware(toolresult.ContextPolicy{Enabled: false})
	historicalAnswer := agentschema.AssistantMessage("old answer", nil)
	historicalAnswer.ReasoningContent = "private historical reasoning"
	currentCall := agentschema.AssistantMessage("", []agentschema.ToolCall{current})
	currentCall.ReasoningContent = "current tool reasoning"
	_, projected, err := middleware.BeforeModelCall(context.Background(), &agentmodel.ModelCall{Messages: []*agentschema.Message{
		agentschema.UserMessage("old request"),
		agentschema.AssistantMessage("", []agentschema.ToolCall{call}),
		{Role: agentschema.ToolRole, ToolCallID: "historical", ToolName: "read", Content: "old result"},
		historicalAnswer,
		agentschema.UserMessage("current request"),
		currentCall,
		{Role: agentschema.ToolRole, ToolCallID: "current", ToolName: "read", Content: "current result"},
	}}, &agentmiddleware.ModelContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projected.Messages) != 5 {
		t.Fatalf("projected messages = %#v", projected.Messages)
	}
	for _, message := range projected.Messages {
		if message != nil && message.ToolCallID == "historical" {
			t.Fatalf("historical tool result remained model-visible: %#v", projected.Messages)
		}
	}
	if projected.Messages[1].ReasoningContent != "private historical reasoning" {
		t.Fatalf("historical provider reasoning was filtered: %#v", projected.Messages)
	}
	if currentCall.ReasoningContent != "current tool reasoning" || projected.Messages[len(projected.Messages)-2].ReasoningContent != "current tool reasoning" {
		t.Fatalf("current-cycle reasoning was filtered: %#v", projected.Messages)
	}
	last := projected.Messages[len(projected.Messages)-1]
	if last == nil || last.ToolCallID != "current" || last.Content != "current result" {
		t.Fatalf("current tool batch was filtered: %#v", projected.Messages)
	}
}

func TestContextNormalizerEmitsBoundedRepairMetric(t *testing.T) {
	missing := agentschema.ToolCall{ID: "missing", Type: "function", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`}}
	call := &agentmodel.ModelCall{Messages: []*agentschema.Message{
		agentschema.SystemMessage("stable"),
		agentschema.AssistantMessage("", []agentschema.ToolCall{missing}),
	}}
	middleware := &contextNormalizerMiddleware{BaseMiddleware: &agentmiddleware.BaseMiddleware{}}
	modelContext := &agentmiddleware.ModelContext{}
	_, normalized, err := middleware.BeforeModelCall(context.Background(), call, modelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Messages) != 3 {
		t.Fatalf("normalized messages = %#v", normalized.Messages)
	}
	metrics, ok := modelContext.ContextNormalization()
	if !ok || metrics != (agentmiddleware.ContextNormalizationMetrics{RepairCount: 1, MessagesBefore: 2, MessagesAfter: 3}) {
		t.Fatalf("normalization metrics = %#v, %v", metrics, ok)
	}
}
