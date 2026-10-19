package lifecycle

import (
	"testing"

	agentchat "denova/internal/agents/chat"
	agentcontext "denova/internal/agents/context"

	sdkcontext "github.com/alfredxw/denova/agent/context"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestProjectConversationContextSelectsUserRequestBeforeProtocolTail(t *testing.T) {
	state := agentschema.UserMessage("workspace state")
	state.Extra = map[string]any{"agent.context_state": "v1"}
	completion := agentschema.UserMessage("research complete")
	completion.TaskCompletion = &agentschema.TaskCompletionMessageMeta{CompletionID: "completion-1", Author: "researcher", Recipient: "writer"}
	prepared := agentchat.AgentContextPreparation{ModelContext: agentcontext.ModelContextResult{
		Messages: []*agentschema.Message{agentschema.UserMessage("rendered current request"), state, completion},
	}}
	request := sdkcontext.ContextRequest{Run: agentschema.RunView{ID: "run-1", CommandID: "command-1", Cycle: 1}}
	fragments, err := projectConversationContext(prepared, request, agentcontext.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 1 || fragments[0].Placement != agentschema.ContextFinalUserMessage || fragments[0].Content != "rendered current request" {
		t.Fatalf("projected turn fragments=%#v", fragments)
	}
	prepared.ModelContext.Messages = []*agentschema.Message{state, completion}
	if _, err := projectConversationContext(prepared, request, agentcontext.DefaultBudget()); err == nil {
		t.Fatal("context without a user request used a protocol message as input")
	}
}
