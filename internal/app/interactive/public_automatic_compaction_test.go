package interactiveapp

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/agents/canonicalstore"
	agentchat "denova/internal/agents/chat"
	agentcompaction "denova/internal/agents/context/compaction"
	agentexecution "denova/internal/agents/execution"
	"denova/internal/agents/modelio"
	agentrun "denova/internal/agents/run"
	agenttoolruntime "denova/internal/agents/toolruntime"
	"denova/internal/interactive"
	"denova/internal/project"

	"github.com/alfredxw/denova/agent"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

const automaticGameCheckpoint = "The traveler followed the river through twenty-four rainy nights and promised to return to the village."

// Only model output is simulated: admission, automatic pressure planning,
// checkpoint generation/validation, tool commits, and journal recovery are real.
type automaticGameCheckpointModel struct {
	history      publicGameHistoryModel
	summaryCalls int
	toolPending  bool
}

func (model *automaticGameCheckpointModel) Generate(_ context.Context, messages []*agentschema.Message, _ ...agentmodel.ModelOption) (*agentschema.Message, error) {
	if err := modelio.ValidateInput(config.AgentKindInteractiveStory, providers.ModelConfig{}, messages, nil, 4<<20, 32_000); err != nil {
		return nil, err
	}
	if len(messages) > 0 && strings.HasPrefix(messages[len(messages)-1].Content, "[Runtime context compaction request]") {
		model.summaryCalls++
		return agentschema.AssistantMessage(automaticGameCheckpoint, nil), nil
	}
	response := model.history.response(messages)
	promptTokens := agentmodel.EstimateMessagesTextTokens(messages)
	response.ResponseMeta = &agentschema.ResponseMeta{
		FinishReason: "stop", Usage: &agentschema.TokenUsage{PromptTokens: promptTokens, CompletionTokens: 100, TotalTokens: promptTokens + 100},
	}
	response.ReasoningContent = "Check the river before advancing the story."
	if !model.toolPending {
		model.toolPending = true
		response.Content = "I will inspect the river."
		response.ToolCalls = []agentschema.ToolCall{{ID: "river-evidence", Type: "function", Function: agentschema.FunctionCall{Name: "read_river", Arguments: `{}`}}}
		response.ResponseMeta.FinishReason = "tool_calls"
	} else {
		model.toolPending = false
	}
	return response, nil
}

func (model *automaticGameCheckpointModel) Stream(ctx context.Context, messages []*agentschema.Message, options ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	response, err := model.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return agentstream.StreamReaderFromArray([]*agentschema.Message{response}), nil
}

func TestGameAutomaticCompactionSurvivesConsecutiveTurnsAndRestart(t *testing.T) {
	ctx := context.Background()
	workspace, dataDir := t.TempDir(), t.TempDir()
	registry := project.NewRegistry(dataDir)
	record, err := registry.Add(workspace, project.TypeGeneral, "Long game compaction")
	if err != nil {
		t.Fatal(err)
	}
	journalStore, err := canonicalstore.New(dataDir, registry)
	if err != nil {
		t.Fatal(err)
	}
	store := interactive.NewStore(workspace)
	story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "Long game compaction", StoryTellerID: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	for turn := range 24 {
		if _, err := store.AppendTurn(story.ID, interactive.AppendTurnRequest{
			BranchID: "main", User: fmt.Sprintf("Follow the river on night %d", turn+1),
			Narrative: fmt.Sprintf("Historical night %d: %s", turn+1, strings.Repeat("雨", 1024)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := store.Snapshot(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	newRuntime := func() *agentexecution.Runtime {
		runtime, err := agentexecution.NewAgentRuntime(ctx, dataDir,
			agentexecution.WithSessionStore(journalStore),
			agentexecution.WithProfiles(publicGameNoopProfile(workspace, story.ID)),
			agentexecution.WithToolMutationApplier(func(context.Context, agenttoolruntime.CommittedToolMutation) error { return nil }),
		)
		if err != nil {
			t.Fatal(err)
		}
		return runtime
	}
	runtime := newRuntime()
	t.Cleanup(func() { _ = runtime.Close(ctx) })
	cfg := &config.Config{Workspace: workspace, OpenAIContextWindowTokens: 32_000}
	model := &automaticGameCheckpointModel{history: publicGameHistoryModel{narrative: "The traveler reached the next bridge."}}
	identity := agentschema.CapabilityIdentity{Kind: "test.automatic-game-checkpoint", Version: 1}
	manager, err := agentcompaction.NewAgentManagerForModel(cfg, config.AgentKindInteractiveStory, 32_000)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := agenttool.InferTool("read_river", "Read current river conditions", func(context.Context, struct{}) (string, error) {
		return "The river is calm and the bridge is open.", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := agenttool.StaticToolsIdentified(agentschema.CapabilityIdentity{Kind: "tools.test.river-evidence", Version: 1}, agenttool.ToolDefinition{
		Tool: tool, Descriptor: agenttool.ToolDescriptor{
			Source: agenttool.ToolSourceRead, Execution: agenttool.ToolExecutionParallelRead,
			MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone,
			Recovery: agenttool.ToolRecoveryReadOnly, ResultProjection: agentschema.ToolResultBoundedModelContext,
			ResultRetention: agentschema.ToolResultDeferred, Steering: agenttool.SteeringFinishCurrent, MaxResultBytes: 4 << 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	options := publicGameOptions(workspace, story.ID, "main")
	options.ProjectID = record.ID
	var checkpointID string
	for turn := 25; turn <= 29; turn++ {
		if turn == 27 {
			if err := runtime.Close(ctx); err != nil {
				t.Fatal(err)
			}
			runtime = newRuntime()
		}
		input := fmt.Sprintf("Continue to bridge %d", turn)
		conversation := NewConversation(store, "", workspace, story.ID, "main", input, 800, cfg)
		submission := gameSubmissionForTest(t, conversation, input, input)
		operation, err := runtime.Start(ctx, agentexecution.StartRequest{Cycle: agentexecution.Cycle{
			Definition: agent.Definition{
				Key: "automatic-game-checkpoint", Name: "game", Model: model, ModelIdentity: identity,
				Middlewares: []agentmiddleware.Middleware{submission},
				Compaction:  manager, Tools: toolset, Permission: agentpermission.FullAccess(),
			},
			Conversation: conversation, Options: options,
			Request: agentchat.ChatRequest{CommandID: fmt.Sprintf("automatic-game-turn-%d", turn), Message: input},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if outcome := operation.Wait(ctx); outcome.Status != agentrun.OutcomeCompleted {
			t.Fatalf("turn %d failed: %+v", turn, outcome)
		}
		if model.summaryCalls != 1 {
			t.Fatalf("turn %d generated %d checkpoints; expected the initial automatic compaction only", turn, model.summaryCalls)
		}
		messages := model.history.lastInput(t)
		if !containsMessageContent(messages, automaticGameCheckpoint) || containsMessageContent(messages, before.Turns[0].Narrative) {
			t.Fatalf("turn %d lost the checkpoint or replayed compacted history", turn)
		}
		status, err := runtime.RuntimeStatusProjection(ctx, options)
		if err != nil || status.Compaction == nil {
			t.Fatalf("turn %d lost durable compaction: %+v, %v", turn, status.Compaction, err)
		}
		if checkpointID == "" {
			checkpointID = status.Compaction.ID
			recoveryTarget := int(float64(cfg.OpenAIContextWindowTokens) * config.DefaultContextCompactionThreshold * config.DefaultContextCompactionRecoveryBand)
			if status.Compaction.TokensAfter <= 0 || status.Compaction.TokensAfter > recoveryTarget {
				t.Fatalf("automatic compaction did not restore context headroom: projected=%d target=%d", status.Compaction.TokensAfter, recoveryTarget)
			}
			t.Logf("Compacted 24-turn history: projected_tokens_after=%d recovery_target=%d", status.Compaction.TokensAfter, recoveryTarget)
		} else if status.Compaction.ID != checkpointID {
			t.Fatalf("turn %d replaced checkpoint %s with %s", turn, checkpointID, status.Compaction.ID)
		}
	}
	after, err := store.Snapshot(story.ID, "main")
	if err != nil || len(after.Turns) != 29 {
		t.Fatalf("game continuation lost turns: count=%d error=%v", len(after.Turns), err)
	}
	if !reflect.DeepEqual(before.Turns, after.Turns[:24]) {
		t.Fatal("automatic compaction changed canonical story history")
	}
}
