package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentcontext "github.com/alfredxw/denova/agent/context"
	agentengine "github.com/alfredxw/denova/agent/engine"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	sessionfile "github.com/alfredxw/denova/agent/session/file"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

type permissionContractTools struct{ definitions []agenttool.ToolDefinition }

func (*permissionContractTools) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.permission-contract-tools", Version: 1}
}

func (tools *permissionContractTools) PrepareTools(context.Context, agenttool.ToolRequest) ([]agenttool.ToolDefinition, error) {
	return append([]agenttool.ToolDefinition(nil), tools.definitions...), nil
}

type permissionContractContext struct{ unavailable bool }

func (*permissionContractContext) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.permission-contract-context", Version: 1}
}

func (source *permissionContractContext) Materialize(context.Context, agentcontext.ContextRequest) ([]agentschema.ContextFragment, error) {
	if source.unavailable {
		return nil, errors.New("referenced context is no longer readable")
	}
	return []agentschema.ContextFragment{{Source: "test", Purpose: "referenced document", Resource: "chapter.md", Revision: "1",
		Stability: agentschema.ContextTurn, Placement: agentschema.ContextFinalUserPrefix, Content: "Original chapter", HardLimit: 1024}}, nil
}

func TestPermissionContractSurvivesJournalReopen(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store, err := sessionfile.New(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := testToolDefinition(&functionTool{name: "inspect", run: func(context.Context, string) (string, error) {
		t.Error("answering a suspended approval executed a tool")
		return "unexpected", nil
	}})
	tool.Descriptor.Source = agenttool.ToolSourceShell
	contextSource := &permissionContractContext{}
	model := &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("", []agentschema.ToolCall{{
		ID: "call", Type: "function", Function: agentschema.FunctionCall{Name: "inspect", Arguments: `{}`},
	}})}}
	definition := agentengine.Definition{Name: "test", Model: model, Tools: mustStaticTools(t, tool), Context: contextSource}
	owner, err := New(ctx, definition, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	key := agentsession.Named("permission-reopen")
	sess, err := owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	run, err := sess.Run(ctx, agentschema.Text("inspect"))
	if err != nil {
		t.Fatal(err)
	}
	id := waitPermissionInteractionID(t, run)
	if _, err := sess.SuspendAndClose(ctx, SuspendRequest{RunID: run.ID(), IdempotencyKey: "pause"}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(ctx); err != nil {
		t.Fatal(err)
	}
	store, err = sessionfile.New(root)
	if err != nil {
		t.Fatal(err)
	}
	contextSource.unavailable = true
	owner, err = New(ctx, definition, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	sess, err = owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		request, resolution, err := sess.Respond(ctx, id, agentinteraction.InteractionResponse{Permission: agentinteraction.PermissionAllowOnce})
		if err != nil || resolution.Permission != agentinteraction.PermissionAllowOnce || request.Permission == nil || len(request.Permission.ToolDefinitionHash) != 64 {
			t.Fatalf("request=%#v resolution=%#v error=%v", request, resolution, err)
		}
	}
	if len(model.calls()) != 1 {
		t.Fatal("approval recovery invoked the model")
	}
}

type permissionArgumentRewrite struct{ agentmiddleware.BaseMiddleware }

func (*permissionArgumentRewrite) WrapToolCall(_ context.Context, next agentmiddleware.ToolCallEndpoint, _ *agentmiddleware.ToolContext) (agentmiddleware.ToolCallEndpoint, error) {
	return func(ctx context.Context, _ string, options ...agenttool.ToolOption) (agentschema.ToolResult, error) {
		return next(ctx, `{"value":"changed"}`, options...)
	}, nil
}

func TestPermissionContractDoesNotAuthorizeRewrittenArguments(t *testing.T) {
	policy := &permissionResolutionInvariantPolicy{decision: agentpermission.PermissionResolvedDecision{Allowed: true}}
	run, executions := startPermissionResolutionInvariantRun(t, policy, &permissionArgumentRewrite{})
	id := waitPermissionInteractionID(t, run)
	if err := run.Respond(t.Context(), id, agentinteraction.InteractionResponse{Permission: agentinteraction.PermissionAllowOnce}); err != nil {
		t.Fatal(err)
	}
	if _, err := run.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 0 || policy.resolve.Load() != 1 {
		t.Fatalf("executions=%d resolutions=%d", executions.Load(), policy.resolve.Load())
	}
	for event := range run.Events() {
		if finished, ok := event.Payload.(agentevent.ToolFinished); ok && strings.Contains(finished.Result, agentschema.ErrPermissionArgumentsChanged.Error()) {
			return
		}
	}
	t.Fatal("missing tool argument authorization failure")
}
