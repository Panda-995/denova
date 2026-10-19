package permission_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alfredxw/denova/agent"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
	"github.com/alfredxw/denova/agent/tool/permission"
)

type rememberStore struct {
	allowed atomic.Bool
}

func (*rememberStore) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "permission.remember-test", Version: 1}
}

func (store *rememberStore) Allowed(context.Context, permission.Rule) (bool, error) {
	return store.allowed.Load(), nil
}

func (store *rememberStore) Remember(context.Context, permission.Rule) error {
	store.allowed.Store(true)
	return nil
}

type permissionModel struct {
	mu        sync.Mutex
	responses []*agentschema.Message
}

func (model *permissionModel) Generate(context.Context, []*agentschema.Message, ...agentmodel.ModelOption) (*agentschema.Message, error) {
	return model.next()
}

func (model *permissionModel) Stream(context.Context, []*agentschema.Message, ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	message, err := model.next()
	if err != nil {
		return nil, err
	}
	return agentstream.StreamReaderFromArray([]*agentschema.Message{message}), nil
}

func (model *permissionModel) next() (*agentschema.Message, error) {
	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.responses) == 0 {
		return nil, errors.New("Permission model exhausted")
	}
	message := model.responses[0]
	model.responses = model.responses[1:]
	return message.Clone(), nil
}

func TestRememberedRuleCommitsBeforeResolutionAndFutureToolExecution(t *testing.T) {
	store := &rememberStore{}
	model := &permissionModel{responses: []*agentschema.Message{
		toolCall("write-1"), agentschema.AssistantMessage("first done", nil),
		toolCall("write-2"), agentschema.AssistantMessage("second done", nil),
	}}
	tool, err := agenttool.InferTool("write_test", "write a test resource", func(context.Context, struct{}) (string, error) {
		if !store.allowed.Load() {
			return "", errors.New("tool ran before remembered permission was durable")
		}
		return "written", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := agenttool.StaticToolsIdentified(agentschema.CapabilityIdentity{Kind: "tools.permission-remember", Version: 1}, agenttool.ToolDefinition{
		Tool: tool, Descriptor: agenttool.ToolDescriptor{
			Source: agenttool.ToolSourceWrite, Execution: agenttool.ToolExecutionWorkspaceExclusive,
			MutationScope: agenttool.ToolMutationWorkspace, PostCheck: agenttool.ToolPostCheckWorkspaceChange,
			Recovery: agenttool.ToolRecoveryIdempotent, ResultProjection: agentschema.ToolResultBoundedModelContext,
			ResultRetention: agentschema.ToolResultProtected, Steering: agenttool.SteeringFinishCurrent,
			MaxResultBytes: 64 << 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := permission.CodingWithRules(store)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := agent.New(context.Background(), agent.Definition{
		Model: model, Tools: toolset, Permission: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err := owner.Session(context.Background(), agentsession.Named("permission-remember"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := session.Run(context.Background(), agent.Text("first write"))
	if err != nil {
		t.Fatal(err)
	}
	var interactionID string
	for event := range first.Events() {
		if request, ok := event.Payload.(agentevent.InteractionRequested); ok {
			interactionID = request.Request.ID
			break
		}
	}
	if interactionID == "" {
		t.Fatal("write did not request permission")
	}
	if err := first.Respond(context.Background(), interactionID, agentinteraction.InteractionResponse{Permission: agentinteraction.PermissionRemember}); err != nil {
		t.Fatal(err)
	}
	for event := range first.Events() {
		if _, ok := event.Payload.(agentevent.InteractionResolved); ok && !store.allowed.Load() {
			t.Fatal("InteractionResolved was published before RuleStore.Remember")
		}
	}
	if result, err := first.Wait(context.Background()); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("first result = %#v error = %v", result, err)
	}
	second, err := session.Run(context.Background(), agent.Text("second write"))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := second.Wait(context.Background()); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("second result = %#v error = %v", result, err)
	}
	for event := range second.Events() {
		if _, ok := event.Payload.(agentevent.InteractionRequested); ok {
			t.Fatal("remembered rule requested permission again")
		}
	}
}

func toolCall(id string) *agentschema.Message {
	return agentschema.AssistantMessage("", []agentschema.ToolCall{{
		ID: id, Type: "function", Function: agentschema.FunctionCall{Name: "write_test", Arguments: `{}`},
	}})
}
