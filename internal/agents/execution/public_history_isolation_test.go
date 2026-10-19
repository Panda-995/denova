package execution

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	agentchat "denova/internal/agents/chat"
	agentconversation "denova/internal/agents/conversation"
	agentrun "denova/internal/agents/run"
	"denova/internal/agents/session"

	"github.com/alfredxw/denova/agent"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

type invalidHistoryConversation struct {
	agentchat.Conversation
	messages []*agentschema.Message
}

func (conversation invalidHistoryConversation) CanonicalMessages(context.Context) ([]*agentschema.Message, error) {
	return conversation.messages, nil
}

func TestInvalidHistoryIsolatesAdmissionToSessionOrBranch(t *testing.T) {
	for _, kind := range []string{agentrun.AgentKindIDE, agentrun.AgentKindInteractiveStory} {
		t.Run(kind, func(t *testing.T) {
			ctx := t.Context()
			runtime := NewEphemeralRuntime()
			store, err := session.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			broken, err := store.GetOrCreate("broken")
			if err != nil {
				t.Fatal(err)
			}
			healthy, err := store.GetOrCreate("healthy")
			if err != nil {
				t.Fatal(err)
			}
			model := &publicBackendTestModel{}
			options := agentrun.Options{
				AgentKind: kind, ProjectID: "history-project", Workspace: t.TempDir(),
				SessionID: broken.ID, RootAgentName: "root",
			}
			// Drain the runtime and close its journal before TempDir cleanup.
			t.Cleanup(func() { _ = store.Close() })
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })
			if kind == agentrun.AgentKindInteractiveStory {
				options.SessionID = ""
				options.StoryID, options.BranchID = "same-story", "broken-branch"
			}
			messages := make([]*agentschema.Message, 0, 397)
			for range 394 {
				messages = append(messages, agentschema.UserMessage("earlier history"))
			}
			messages = append(messages,
				agentschema.AssistantMessage("", []agentschema.ToolCall{
					{ID: "read-a", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`}},
					{ID: "read-b", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`}},
				}),
				agentschema.ToolMessage(agentschema.TextToolResult("result a"), "read-a"),
				agentschema.UserMessage("continue"),
			)
			original := clonePublicBackendMessages(messages)
			cycle := Cycle{
				Definition: agent.Definition{
					Name: "root", Model: model,
					ModelIdentity: agentschema.CapabilityIdentity{Kind: "model.history-isolation", Version: 1},
				},
				Conversation: invalidHistoryConversation{
					Conversation: agentconversation.NewSessionConversationForAgent(broken, nil, kind),
					messages:     messages,
				},
				Request: agentchatRequest("rejected-command", "continue"), Options: options,
			}
			for range 2 {
				operation, err := runtime.Start(ctx, StartRequest{Cycle: cycle})
				if operation != nil || !errors.Is(err, agentcanonical.ErrInvalidCanonicalMessages) ||
					!strings.Contains(err.Error(), "message 396 splits an incomplete tool-result batch") {
					t.Fatalf("invalid history admission operation=%v error=%v", operation, err)
				}
			}
			if _, err := runtime.Inspect(ctx, cycle); !errors.Is(err, agentcanonical.ErrInvalidCanonicalMessages) {
				t.Fatalf("invalid history inspection error=%v", err)
			}
			if !reflect.DeepEqual(messages, original) || len(model.inputs) != 0 ||
				len(runtime.public.runs) != 0 || len(runtime.public.registrations) != 0 {
				t.Fatal("rejection changed history, called a model, or retained a live run/registration")
			}
			cycle.Conversation = agentconversation.NewSessionConversationForAgent(healthy, nil, kind)
			cycle.Request = agentchatRequest("healthy-command", "continue healthy history")
			if kind == agentrun.AgentKindInteractiveStory {
				cycle.Options.BranchID = "healthy-branch"
			} else {
				cycle.Options.SessionID = healthy.ID
			}
			operation, err := runtime.Start(ctx, StartRequest{Cycle: cycle})
			if err != nil {
				t.Fatalf("unrelated conversation could not start: %v", err)
			}
			waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			outcome := operation.Wait(waitCtx)
			cancel()
			if outcome.Status != agentrun.OutcomeCompleted {
				t.Fatalf("unrelated conversation did not complete: %+v", outcome)
			}
			// Replacing the failed projection with valid canonical history must
			// also let the original identity run without a process restart.
			cycle.Options = options
			cycle.Conversation = agentconversation.NewSessionConversationForAgent(broken, nil, kind)
			cycle.Request = agentchatRequest("recovered-command", "continue valid history")
			operation, err = runtime.Start(ctx, StartRequest{Cycle: cycle})
			if err != nil {
				t.Fatalf("failed identity remained poisoned: %v", err)
			}
			waitCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
			outcome = operation.Wait(waitCtx)
			cancel()
			if outcome.Status != agentrun.OutcomeCompleted {
				t.Fatalf("recovered identity did not complete: %+v", outcome)
			}
		})
	}
}
