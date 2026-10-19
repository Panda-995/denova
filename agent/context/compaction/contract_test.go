package compaction_test

import (
	"context"
	"strings"
	"testing"

	"github.com/alfredxw/denova/agent"
	"github.com/alfredxw/denova/agent/context/compaction"
	"github.com/alfredxw/denova/agent/context/compaction/compactiontest"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestStandardManagerContract(t *testing.T) {
	compactiontest.RunManagerContract(t, func(t testing.TB) compaction.CompactionManager {
		manager := compaction.Standard(compaction.StandardConfig{
			Summarizer: compaction.SummarizerFunc{
				Capability: agentschema.CapabilityIdentity{Kind: "compaction.contract-summary", Version: 1},
				Func: func(context.Context, compaction.SummaryRequest) (compaction.CompactionCheckpoint, error) {
					return compaction.CompactionCheckpoint{Summary: "contract summary"}, nil
				},
			},
			HardLimitBytes: 8 << 20, SummaryLimitBytes: 256 << 10,
		})
		if err := manager.(agent.DefinitionInitializer).InitializeDefinition(context.Background()); err != nil {
			t.Fatal(err)
		}
		return manager
	})
}

func TestStandardCalibratesPlanFromExactPreviousProviderUsage(t *testing.T) {
	manager := compaction.Standard(compaction.StandardConfig{
		Summarizer: compaction.SummarizerFunc{
			Capability: agentschema.CapabilityIdentity{Kind: "compaction.calibration-summary", Version: 1},
			Func: func(context.Context, compaction.SummaryRequest) (compaction.CompactionCheckpoint, error) {
				return compaction.CompactionCheckpoint{Summary: "summary"}, nil
			},
		},
		TriggerBytes: 1024, KeepRecentBytes: 128, HardLimitBytes: 8 << 20, SummaryLimitBytes: 256 << 10,
		ContextWindowTokens: 10_000, TriggerRatio: .85, RecoveryBand: .8,
	})
	identity := agentschema.CapabilityIdentity{Kind: "test.calibration", Version: 1}
	for _, scenario := range []string{"original", "projected_history", "changed_tools", "legacy_usage", "legacy_estimator", "future_estimator", "invalid_estimate", "changed_model", "unidentified_model"} {
		t.Run(scenario, func(t *testing.T) {
			previousPrompt := []*agentschema.Message{agentschema.UserMessage(strings.Repeat("previous input ", 120))}
			originalEstimate := agentmodel.EstimateRequestTextTokens(previousPrompt, nil)
			answer := agentschema.AssistantMessage("previous answer", nil)
			answer.ResponseMeta = &agentschema.ResponseMeta{
				Usage:         &agentschema.TokenUsage{PromptTokens: originalEstimate * 2},
				InputEstimate: &agentschema.ModelInputEstimate{Version: agentmodel.InputEstimateVersion, Tokens: originalEstimate, Model: identity},
			}
			modelIdentity := identity
			var tools []*agentschema.ToolInfo
			trusted := true
			switch scenario {
			case "projected_history":
				previousPrompt = []*agentschema.Message{agentschema.SystemMessage("short checkpoint")}
			case "changed_tools":
				tools = []*agentschema.ToolInfo{{Name: "new_tool", Desc: strings.Repeat("new schema ", 100)}}
			case "legacy_usage":
				answer.ResponseMeta.InputEstimate = nil
				trusted = false
			case "legacy_estimator":
				answer.ResponseMeta.InputEstimate.Version = 0
				trusted = false
			case "future_estimator":
				answer.ResponseMeta.InputEstimate.Version++
				trusted = false
			case "invalid_estimate":
				answer.ResponseMeta.InputEstimate.Tokens = 0
				trusted = false
			case "changed_model":
				modelIdentity.Version++
				trusted = false
			case "unidentified_model":
				modelIdentity = agentschema.CapabilityIdentity{}
				trusted = false
			}
			messages := append(previousPrompt, answer, agentschema.UserMessage(strings.Repeat("new input ", 30)))
			snapshot := (&agentmodel.ModelCall{
				Model: calibrationIdentityModel{identity: modelIdentity}, Messages: messages,
				Options: []agentmodel.ModelOption{agentmodel.WithTools(tools)},
			}).Snapshot()
			plan, err := manager.Plan(context.Background(), compaction.CompactionPlanRequest{
				Groups: []compaction.CompactionGroup{{Messages: messages[:2]}}, ModelSnapshot: snapshot,
				EstimateAfter: func(int) (agentmodel.InputSize, error) { return snapshot.WithMessages(messages[2:]).EstimateInput() },
			})
			if err != nil {
				t.Fatal(err)
			}
			metrics := plan.Metrics
			wantPrompt, wantEstimate := 0, 0
			wantProjected := agentmodel.EstimateRequestTextTokens(messages, snapshot.ResolvedOptions().Tools)
			if trusted {
				wantPrompt, wantEstimate = originalEstimate*2, originalEstimate
				wantProjected *= 2
			}
			if metrics.ObservedPromptTokens != wantPrompt || metrics.ObservedEstimateTokens != wantEstimate || metrics.ProjectedTokensBefore != wantProjected {
				t.Fatalf("calibration=%+v; want original pair %d/%d and projection %d", metrics, wantPrompt, wantEstimate, wantProjected)
			}
		})
	}
}

type calibrationIdentityModel struct {
	agentmodel.BaseChatModel
	identity agentschema.CapabilityIdentity
}

func (model calibrationIdentityModel) ModelIdentity() agentschema.CapabilityIdentity {
	return model.identity
}

func TestStandardIncludesLifecycleSideForkReserveInTriggerAndValidation(t *testing.T) {
	manager := compaction.Standard(compaction.StandardConfig{
		Summarizer: compaction.SummarizerFunc{
			Capability: agentschema.CapabilityIdentity{Kind: "compaction.lifecycle-reserve-summary", Version: 1},
			Func: func(context.Context, compaction.SummaryRequest) (compaction.CompactionCheckpoint, error) {
				return compaction.CompactionCheckpoint{Summary: "summary"}, nil
			},
		},
		TriggerBytes: 1024, KeepRecentBytes: 128, HardLimitBytes: 8 << 20, SummaryLimitBytes: 256 << 10,
		ContextWindowTokens: 2_000, TriggerRatio: .85, RecoveryBand: .8,
	})
	messages := []*agentschema.Message{
		agentschema.UserMessage(strings.Repeat("old request ", 100)),
		agentschema.AssistantMessage("old answer", nil),
		agentschema.UserMessage("current request"),
		agentschema.AssistantMessage("current answer", nil),
	}
	plan, err := manager.Plan(context.Background(), compaction.CompactionPlanRequest{
		Groups:        []compaction.CompactionGroup{{Messages: messages[:2]}},
		ModelSnapshot: (&agentmodel.ModelCall{Messages: messages}).Snapshot(), LifecycleReservedTokens: 1_600,
		EstimateAfter: func(int) (agentmodel.InputSize, error) {
			return (agentmodel.InputEstimator{}).Estimate(messages[2:], nil)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != compaction.CompactionCreate || plan.Validation.ReservedTokens != 1_600 || plan.Metrics.ReservedTokens != 1_600 ||
		plan.Metrics.ProjectedTokensBefore != plan.Metrics.CalibratedTokens(plan.Metrics.EstimatedTokensBefore)+1_600 {
		t.Fatalf("lifecycle-reserved Compaction plan = %#v", plan)
	}
}

func TestStandardUsesCapacityAwareModelOutputReserve(t *testing.T) {
	manager := compaction.Standard(compaction.StandardConfig{
		Summarizer: compaction.SummarizerFunc{
			Capability: agentschema.CapabilityIdentity{Kind: "compaction.output-cap-summary", Version: 1},
			Func: func(context.Context, compaction.SummaryRequest) (compaction.CompactionCheckpoint, error) {
				return compaction.CompactionCheckpoint{Summary: "summary"}, nil
			},
		},
		TriggerBytes: 1024, KeepRecentBytes: 128, HardLimitBytes: 8 << 20, SummaryLimitBytes: 256 << 10,
		ContextWindowTokens: 10_000, ReservedTokens: 1000, TriggerRatio: .85, RecoveryBand: .8,
	})
	messages := []*agentschema.Message{
		agentschema.UserMessage("old request"),
		agentschema.AssistantMessage("old answer", nil),
		agentschema.UserMessage("current request"),
	}
	call := &agentmodel.ModelCall{Messages: messages, Options: []agentmodel.ModelOption{agentmodel.WithMaxTokens(4000)}}
	plan, err := manager.Plan(context.Background(), compaction.CompactionPlanRequest{
		Groups: []compaction.CompactionGroup{{Messages: messages[:2]}}, ModelSnapshot: call.Snapshot(), Force: true,
		EstimateAfter: func(int) (agentmodel.InputSize, error) {
			return call.Snapshot().WithMessages(messages[2:]).EstimateInput()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Validation.ReservedTokens != 2500 || plan.Metrics.ReservedTokens != 2500 {
		t.Fatalf("capacity-aware Compaction reserve = validation:%d metrics:%d, want 2500",
			plan.Validation.ReservedTokens, plan.Metrics.ReservedTokens)
	}
}

func TestStandardPlansOnlyCompleteToolBatchBoundaries(t *testing.T) {
	manager := compaction.Standard(compaction.StandardConfig{
		Summarizer: compaction.SummarizerFunc{
			Capability: agentschema.CapabilityIdentity{Kind: "compaction.atomic-boundary-summary", Version: 1},
			Func: func(context.Context, compaction.SummaryRequest) (compaction.CompactionCheckpoint, error) {
				return compaction.CompactionCheckpoint{Summary: "summary"}, nil
			},
		},
		TriggerBytes: 1024, KeepRecentBytes: 128, KeepRecentGroups: 1,
		HardLimitBytes: 8 << 20, SummaryLimitBytes: 256 << 10,
	})
	messages := []*agentschema.Message{
		agentschema.UserMessage(strings.Repeat("old request ", 200)),
		agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "read-old", Type: "function",
			Function: agentschema.FunctionCall{Name: "read", Arguments: `{"path":"chapter.md"}`},
		}}),
		agentschema.ToolMessage(agentschema.TextToolResult(strings.Repeat("tool evidence ", 400)), "read-old", agentschema.WithToolName("read")),
		agentschema.AssistantMessage("old answer", nil),
		agentschema.UserMessage("current request"),
		agentschema.AssistantMessage("current answer", nil),
	}
	plan, err := manager.Plan(context.Background(), compaction.CompactionPlanRequest{
		Groups:        []compaction.CompactionGroup{{Messages: messages[:2]}},
		ModelSnapshot: (&agentmodel.ModelCall{Messages: messages}).Snapshot(), Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != compaction.CompactionCreate || plan.GroupCount != 1 {
		t.Fatalf("atomic Compaction plan = %#v", plan)
	}
	if messages[4].Role != agentschema.User {
		t.Fatalf("Compaction split a turn/tool batch before %#v", messages[4])
	}
}

func TestStandardSummarizesOversizedOldMessageButKeepsNewestToolGroup(t *testing.T) {
	manager := compaction.Standard(compaction.StandardConfig{
		Summarizer: compaction.SummarizerFunc{Capability: agentschema.CapabilityIdentity{Kind: "test.oversized-old-source", Version: 1}, Func: func(context.Context, compaction.SummaryRequest) (compaction.CompactionCheckpoint, error) {
			return compaction.CompactionCheckpoint{Summary: "summary"}, nil
		}},
		TriggerBytes: 4096, KeepRecentBytes: 1024, HardLimitBytes: 1 << 20, SummaryLimitBytes: 1024,
	})
	messages := []*agentschema.Message{
		agentschema.UserMessage("Historical task"), agentschema.AssistantMessage(strings.Repeat("old evidence ", 1000), nil),
		agentschema.UserMessage("Continue verification"), agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "latest", Type: "function", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`}}}),
		{Role: agentschema.ToolRole, ToolCallID: "latest", Content: "Latest original evidence"},
	}
	plan, err := manager.Plan(t.Context(), compaction.CompactionPlanRequest{Groups: []compaction.CompactionGroup{{Messages: messages[:2]}}, ModelSnapshot: (&agentmodel.ModelCall{Messages: messages}).Snapshot(), Force: true})
	if err != nil || plan.GroupCount != 1 {
		t.Fatalf("old evidence pinned in retained tail: %+v %v", plan, err)
	}
	messages[4].Content = strings.Repeat("large latest evidence ", 1000)
	plan, err = manager.Plan(t.Context(), compaction.CompactionPlanRequest{Groups: []compaction.CompactionGroup{{Messages: messages[:2]}}, ModelSnapshot: (&agentmodel.ModelCall{Messages: messages}).Snapshot(), Force: true})
	if err != nil || plan.GroupCount != 1 {
		t.Fatalf("newest complete tool group split: %+v %v", plan, err)
	}
	messages = append(messages, agentschema.UserMessage("Unconsumed steering: keep the new evidence."))
	plan, err = manager.Plan(t.Context(), compaction.CompactionPlanRequest{Groups: []compaction.CompactionGroup{{Messages: messages[:2]}}, ModelSnapshot: (&agentmodel.ModelCall{Messages: messages}).Snapshot(), Force: true})
	if err != nil || plan.GroupCount != 1 {
		t.Fatalf("unconsumed steering displaced the newest tool group: %+v %v", plan, err)
	}
}
