package compaction

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"denova/config"

	agentcompaction "github.com/alfredxw/denova/agent/context/compaction"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestAgentManagerSummaryLimitUsesTightestTargetContextLimit(t *testing.T) {
	enabled := true
	fragmentBytes := 96 << 10
	totalBytes := 80 << 10
	providerBytes := 128 << 10
	cfg := &config.Config{AgentContexts: config.AgentContextSettings{IDE: config.AgentContextOverride{
		CompactionEnabled: &enabled,
		MaxFragmentBytes:  &fragmentBytes, MaxTotalInjectedBytes: &totalBytes,
		MaxProviderInputBytes: &providerBytes,
	}}}
	manager, err := NewAgentManager(cfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.SummaryLimitBytes(); got != totalBytes {
		t.Fatalf("summary limit = %d, want tightest target limit %d", got, totalBytes)
	}
}

func TestAgentManagerForModelSeparatesPolicyKindFromConcreteModelWindow(t *testing.T) {
	cfg := &config.Config{OpenAIContextWindowTokens: 100_000}
	small, err := NewAgentManagerForModel(cfg, config.AgentKindIDE, 12_000)
	if err != nil {
		t.Fatal(err)
	}
	large, err := NewAgentManagerForModel(cfg, config.AgentKindIDE, 24_000)
	if err != nil {
		t.Fatal(err)
	}
	if small.Identity() == large.Identity() {
		t.Fatal("concrete model context window did not change Compaction behavior identity")
	}
	messages := []*agentschema.Message{agentschema.UserMessage(strings.Repeat("history ", 200)), agentschema.AssistantMessage("answer", nil), agentschema.UserMessage("continue")}
	plan, err := small.Plan(context.Background(), agentcompaction.CompactionPlanRequest{
		Groups: []agentcompaction.CompactionGroup{{Messages: messages[:2]}}, ModelSnapshot: (&agentmodel.ModelCall{Messages: messages}).Snapshot(), Force: true,
		EstimateAfter: func(int) (agentmodel.InputSize, error) {
			return (agentmodel.InputEstimator{}).Estimate(messages[2:], nil)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Validation.ContextWindowTokens != 12_000 || plan.Metrics.ContextWindowTokens != 12_000 {
		t.Fatalf("child model window was not applied to plan: %#v", plan)
	}
}

func TestElisionPolicySharesProductControlAndUsesConcreteChildBudget(t *testing.T) {
	threshold := .5
	enabled := false
	cfg := &config.Config{OpenAIContextWindowTokens: 100_000, AgentContexts: config.AgentContextSettings{
		IDE:              config.AgentContextOverride{CompactionThreshold: &threshold},
		InteractiveStory: config.AgentContextOverride{CompactionEnabled: &enabled},
	}}
	policy := NewElisionPolicyForModel(cfg, config.AgentKindIDE, 12_000)
	if policy == nil || policy.ContextWindowTokens != 12_000 || policy.TriggerRatio >= threshold || policy.ReservedTokens <= 0 {
		t.Fatalf("child Elision policy ignored inherited control or actual budget: %+v", policy)
	}
	if got := NewElisionPolicyForModel(cfg, config.AgentKindInteractiveStory, 12_000); got != nil {
		t.Fatalf("disabled automatic maintenance still enabled Elision: %+v", got)
	}
}

func TestCompactionSummarizerIdentityIncludesCheckpointGuidance(t *testing.T) {
	guidance := "Preserve verification evidence."
	base, err := NewAgentManager(&config.Config{OpenAIContextWindowTokens: 100_000}, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := NewAgentManager(&config.Config{OpenAIContextWindowTokens: 100_000, AgentContexts: config.AgentContextSettings{
		IDE: config.AgentContextOverride{CheckpointGuidance: &guidance},
	}}, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	if base.Identity() == configured.Identity() {
		t.Fatal("checkpoint guidance did not change the compaction identity")
	}
}

func TestAgentManagerAdvancesBeforeCacheSafeForkCapacityIsExhausted(t *testing.T) {
	cfg := &config.Config{OpenAIContextWindowTokens: 100_000}
	manager, err := NewAgentManager(cfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	source := []*agentschema.Message{
		agentschema.UserMessage(strings.Repeat("old request ", 6_000)),
		agentschema.AssistantMessage(strings.Repeat("old answer ", 6_000), nil),
		agentschema.UserMessage("current request"),
		agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "latest-evidence", Type: "function", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`},
		}}),
		agentschema.ToolMessage(agentschema.TextToolResult("Latest original evidence"), "latest-evidence", agentschema.WithToolName("read")),
	}
	primary := append([]*agentschema.Message{agentschema.SystemMessage("stable system")}, source...)
	call := &agentmodel.ModelCall{
		Messages: primary,
		Options:  []agentmodel.ModelOption{agentmodel.WithTools(nil), agentmodel.WithMaxTokens(70_000)},
	}
	plan, err := manager.Plan(context.Background(), agentcompaction.CompactionPlanRequest{
		Groups: []agentcompaction.CompactionGroup{{Messages: source[:2]}}, ModelSnapshot: call.Snapshot(),
		EstimateAfter: func(int) (agentmodel.InputSize, error) {
			return call.Snapshot().WithMessages(append(primary[:1:1], source[2:]...)).EstimateInput()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != agentcompaction.CompactionCreate || plan.GroupCount != 1 {
		t.Fatalf("capacity preflight plan = %#v", plan)
	}
}

func TestAgentManagerCompactionForkPreservesFinalModelRequestIdentity(t *testing.T) {
	response := agentschema.AssistantMessage("## Goal\nPreserve the exact task.", nil)
	model := &compactionForkCaptureModel{response: response}
	cfg := &config.Config{OpenAIContextWindowTokens: 100_000}
	manager, err := NewAgentManager(cfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	source := []*agentschema.Message{
		agentschema.UserMessage("old request"),
		agentschema.AssistantMessage("old answer", nil),
	}
	primary := []*agentschema.Message{
		agentschema.SystemMessage("stable system"),
		source[0].Clone(),
		source[1].Clone(),
		agentschema.UserMessage("current request"),
	}
	tools := []*agentschema.ToolInfo{{Name: "read", Desc: "read files"}}
	call := &agentmodel.ModelCall{
		Model: model, Messages: primary,
		Options: []agentmodel.ModelOption{
			agentmodel.WithTools(tools),
			agentmodel.WithMaxTokens(2048),
			agentmodel.WithToolChoice(agentmodel.ToolChoiceAllowed, "read"),
		},
	}
	checkpoint, err := manager.Compact(context.Background(), agentcompaction.CompactionCompactRequest{
		Messages: source, ModelSnapshot: call.Snapshot(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Summary != response.Content || model.requests != 1 {
		t.Fatalf("checkpoint=%#v model requests=%d", checkpoint, model.requests)
	}
	if len(model.inputs[0]) != len(primary)+1 || !reflect.DeepEqual(model.inputs[0][:len(primary)], primary) {
		t.Fatalf("compaction fork changed provider prefix: %#v", model.inputs[0])
	}
	resolved := model.options[0]
	if len(resolved.Tools) != 1 || resolved.Tools[0].Name != "read" || resolved.MaxTokens == nil || *resolved.MaxTokens != 4000 ||
		resolved.ToolChoice == nil || *resolved.ToolChoice != agentmodel.ToolChoiceAllowed ||
		!reflect.DeepEqual(resolved.AllowedToolNames, []string{"read"}) {
		t.Fatalf("compaction fork changed model options: %#v", resolved)
	}
}

func TestAgentManagerCompactionDoesNotSummarizeModelHiddenToolHistory(t *testing.T) {
	disabled := false
	model := &compactionForkCaptureModel{response: agentschema.AssistantMessage("summary without hidden tool body", nil)}
	cfg := &config.Config{
		OpenAIContextWindowTokens: 100_000,
		AgentContexts: config.AgentContextSettings{IDE: config.AgentContextOverride{
			ToolResultContextEnabled: &disabled,
		}},
	}
	manager, err := NewAgentManager(cfg, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	toolCall := agentschema.ToolCall{
		ID: "read-secret", Type: "function",
		Function: agentschema.FunctionCall{Name: "read", Arguments: `{"path":"secret.md"}`},
	}
	raw := []*agentschema.Message{
		agentschema.UserMessage("old request"),
		agentschema.AssistantMessage("", []agentschema.ToolCall{toolCall}),
		{Role: agentschema.ToolRole, ToolCallID: "read-secret", ToolName: "read", Content: "MODEL_HIDDEN_SECRET_BODY"},
		agentschema.AssistantMessage("old answer", nil),
	}
	visible := []*agentschema.Message{raw[0].Clone(), raw[3].Clone(), agentschema.UserMessage("current request")}
	checkpoint, err := manager.Compact(context.Background(), agentcompaction.CompactionCompactRequest{
		Messages:      raw,
		ModelSnapshot: (&agentmodel.ModelCall{Model: model, Messages: visible}).Snapshot(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Summary != "summary without hidden tool body" || model.requests != 1 {
		t.Fatalf("checkpoint=%#v model requests=%d", checkpoint, model.requests)
	}
	for _, message := range model.inputs[0] {
		if message != nil && strings.Contains(message.Content, "MODEL_HIDDEN_SECRET_BODY") {
			t.Fatalf("model-hidden tool body was resurrected in Compaction: %#v", model.inputs[0])
		}
	}
}

func TestAgentManagerIdentityIncludesToolContextVisibilityPolicy(t *testing.T) {
	enabled, disabled := true, false
	visible, err := NewAgentManager(&config.Config{
		OpenAIContextWindowTokens: 100_000,
		AgentContexts: config.AgentContextSettings{IDE: config.AgentContextOverride{
			ToolResultContextEnabled: &enabled,
		}},
	}, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := NewAgentManager(&config.Config{
		OpenAIContextWindowTokens: 100_000,
		AgentContexts: config.AgentContextSettings{IDE: config.AgentContextOverride{
			ToolResultContextEnabled: &disabled,
		}},
	}, config.AgentKindIDE)
	if err != nil {
		t.Fatal(err)
	}
	if visible.Identity() == hidden.Identity() {
		t.Fatal("tool-result visibility policy did not change Compaction behavior identity")
	}
}
