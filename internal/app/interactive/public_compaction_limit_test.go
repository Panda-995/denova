package interactiveapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"denova/config"
	agentchat "denova/internal/agents/chat"
	agentcompaction "denova/internal/agents/context/compaction"
	agentstructural "denova/internal/agents/context/structural"
	agentexecution "denova/internal/agents/execution"
	"denova/internal/agents/modelio"
	agentrun "denova/internal/agents/run"
	"denova/internal/interactive"

	"github.com/alfredxw/denova/agent"
	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type guardedGameCheckpointModel struct{ calls int }

func (model *guardedGameCheckpointModel) Generate(_ context.Context, messages []*agentschema.Message, _ ...agentmodel.ModelOption) (*agentschema.Message, error) {
	if err := modelio.ValidateInput(config.AgentKindInteractiveStory, providers.ModelConfig{}, messages, nil, 4<<20, 32_000); err != nil {
		return nil, err
	}
	model.calls++
	return agentschema.AssistantMessage("The player followed the river through the storm and reached the village.", nil), nil
}

func (model *guardedGameCheckpointModel) Stream(ctx context.Context, messages []*agentschema.Message, options ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	message, err := model.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return agentstream.StreamReaderFromArray([]*agentschema.Message{message}), nil
}

func TestGameManualCompactionRecoversHistoryAboveProviderTokenLimit(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	store := interactive.NewStore(workspace)
	story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "Long game recovery", StoryTellerID: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := store.AppendTurn(story.ID, interactive.AppendTurnRequest{
			BranchID: "main", User: "Follow the river", Narrative: strings.Repeat("雨", 12_000),
		}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Workspace: workspace, OpenAIContextWindowTokens: 32_000}
	conversation := NewConversation(store, "", workspace, story.ID, "main", "", 800, cfg)
	history, err := conversation.CanonicalMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var limit *modelio.ProviderInputLimitError
	if err := modelio.ValidateInput(config.AgentKindInteractiveStory, providers.ModelConfig{}, history, nil, 4<<20, 32_000); !errors.As(err, &limit) ||
		limit.Tokens <= limit.MaxTokens || limit.Bytes >= limit.MaxBytes {
		t.Fatalf("expected token-only overflow below the byte limit: %v", err)
	}
	model := &guardedGameCheckpointModel{}
	identity := agentschema.CapabilityIdentity{Kind: "test.guarded-game-checkpoint", Version: 1}
	manager, err := agentcompaction.NewAgentManagerForModel(cfg, config.AgentKindInteractiveStory, 32_000)
	if err != nil {
		t.Fatal(err)
	}
	runtime := agentexecution.NewEphemeralRuntime()
	t.Cleanup(func() { _ = runtime.Close(ctx) })
	cycle := agentexecution.Cycle{
		Definition:   agent.Definition{Key: "long-game-recovery", Name: "game", Model: model, ModelIdentity: identity, Compaction: manager},
		Conversation: conversation, Options: publicGameOptions(workspace, story.ID, "main"),
	}
	result, err := runtime.ExecuteStructuralOperation(ctx, cycle, agentstructural.Spec{
		Action: agentstructural.Compact, CommandID: "compact-overflowed-game", Ref: agentrun.ContextCompactionRef{Force: true},
	})
	if err != nil || !result.Compaction.Triggered || model.calls == 0 {
		t.Fatalf("over-limit game did not compact through bounded model calls: changed=%t calls=%d error=%v", result.Compaction.Triggered, model.calls, err)
	}
	cycle.Conversation = NewConversation(store, "", workspace, story.ID, "main", "Continue", 800, cfg)
	cycle.Request = agentchat.ChatRequest{CommandID: "continue-overflowed-game", Message: "Continue"}
	cycle.Definition.Middlewares = append(cycle.Definition.Middlewares, gameSubmissionForTest(t, cycle.Conversation.(*Conversation), "Continue", "Continue"))
	operation, err := runtime.Start(ctx, agentexecution.StartRequest{Cycle: cycle})
	if err != nil {
		t.Fatal(err)
	}
	if outcome := operation.Wait(ctx); outcome.Status != agentrun.OutcomeCompleted {
		t.Fatalf("game could not continue below the provider limit after compaction: %+v", outcome)
	}
	snapshot, err := store.Snapshot(story.ID, "main")
	if err != nil || len(snapshot.Turns) != 4 {
		t.Fatalf("compaction lost existing turns or prevented continuation: turns=%d error=%v", len(snapshot.Turns), err)
	}
}
