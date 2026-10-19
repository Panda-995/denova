package execution

import (
	"context"
	"testing"
)

func TestChildInvocationNamespacesCallsAndOwnsResources(t *testing.T) {
	identityCtx := ContextWithInvocationIdentity(context.Background(), InvocationIdentity{
		Scope: "workspace:one", OperationID: "operation-1", Cycle: 1,
	})
	rootCtx, finishRoot, err := BeginRootInvocation(identityCtx, "root")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := finishRoot(); err != nil {
			t.Fatal(err)
		}
	}()
	rootExecutionID := ToolExecutionIDForOrdinal(rootCtx, 1, 0)
	callCtx := ContextWithToolExecution(rootCtx, rootExecutionID, "call-1", "task")
	childCtx, finishChild, err := BeginChildInvocation(callCtx, "researcher")
	if err != nil {
		t.Fatal(err)
	}
	scope, ok := InvocationScopeFromContext(childCtx)
	if !ok || scope.Depth != 1 || len(scope.RunPath) != 2 || scope.RunPath[0] != "root" || scope.RunPath[1] != "researcher" {
		t.Fatalf("child scope = %#v", scope)
	}
	executionID := ToolExecutionIDForOrdinal(childCtx, 1, 0)
	if executionID == "call-1" || executionID == rootExecutionID || executionID != ToolExecutionIDForOrdinal(childCtx, 1, 0) {
		t.Fatalf("child execution id = %q", executionID)
	}
	created := 0
	closed := 0
	resource, err := InvocationResource(childCtx, "test", func(context.Context) (*int, func(context.Context) error, error) {
		created++
		value := 7
		return &value, func(context.Context) error { closed++; return nil }, nil
	})
	if err != nil || resource == nil || *resource != 7 {
		t.Fatalf("resource=%v err=%v", resource, err)
	}
	again, err := InvocationResource(childCtx, "test", func(context.Context) (*int, func(context.Context) error, error) {
		created++
		return nil, nil, nil
	})
	if err != nil || again != resource || created != 1 {
		t.Fatalf("reused resource=%v created=%d err=%v", again, created, err)
	}
	if err := finishChild(); err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("resource cleanup count = %d", closed)
	}
}

func TestExecutionNamespaceReplaysDeterministicallyAndChangesByCycle(t *testing.T) {
	derive := func(cycle int) string {
		ctx := ContextWithInvocationIdentity(context.Background(), InvocationIdentity{
			Scope: "workspace:one", OperationID: "operation-stable", Cycle: cycle,
		})
		ctx, finish, err := BeginRootInvocation(ctx, "root")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = finish() }()
		return ToolExecutionIDForOrdinal(ctx, 3, 4)
	}
	first := derive(1)
	if first == "" || first != derive(1) || first == derive(2) {
		t.Fatalf("execution IDs first=%q replay=%q next-cycle=%q", first, derive(1), derive(2))
	}
}
