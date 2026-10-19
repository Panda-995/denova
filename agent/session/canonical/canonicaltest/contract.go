// Package canonicaltest provides reusable idempotency and partial-result
// checks for product CanonicalAdapter implementations.
package canonicaltest

import (
	"context"
	"strings"
	"testing"

	"github.com/alfredxw/denova/agent"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

type Factory func(testing.TB) agentcanonical.CanonicalAdapter

func RunAdapterContract(t *testing.T, factory Factory) {
	t.Helper()
	adapter := factory(t)
	identity := adapter.Identity()
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		t.Fatalf("identity = %#v", identity)
	}
	commit := agentcanonical.CommitIdentity{
		Session: agentsession.Named("canonical-contract"), CommandID: "command-1",
		RunID: "run-1", Cycle: 1, Stage: agentcanonical.CommitInput,
	}
	input := agentcanonical.InputCommitRequest{Identity: commit, Hash: "input-hash", Input: agent.Text("input")}
	first, err := adapter.MaterializeInput(context.Background(), input)
	if err != nil || strings.TrimSpace(first.Revision) == "" {
		t.Fatalf("first input receipt = %#v error = %v", first, err)
	}
	replayed, err := adapter.MaterializeInput(context.Background(), input)
	if err != nil || replayed.Revision != first.Revision {
		t.Fatalf("replayed input receipt = %#v error = %v", replayed, err)
	}
	outputIdentity := commit
	outputIdentity.Stage = agentcanonical.CommitOutput
	output := agentcanonical.OutputCommitRequest{
		Identity: outputIdentity, Hash: "output-hash",
		Message: *agentschema.AssistantMessage("output", nil),
	}
	outputReceipt, err := adapter.CommitOutput(context.Background(), output)
	if err != nil || strings.TrimSpace(outputReceipt.Revision) == "" {
		t.Fatalf("output receipt = %#v error = %v", outputReceipt, err)
	}
}

// RunEffectContract verifies the per-item contract independently of journal ownership.
func RunEffectContract(t *testing.T, factory func(testing.TB) agentcanonical.EffectApplier) {
	t.Helper()
	adapter := factory(t)
	outputIdentity := agentcanonical.CommitIdentity{
		Session: agentsession.Named("effect-contract"), CommandID: "command-1",
		RunID: "run-1", Cycle: 1, Stage: agentcanonical.CommitOutput,
	}
	effects := []agentcanonical.EffectRequest{
		{ID: "effect-1", Identity: outputIdentity, CallID: "call", Index: 0, Effect: agentschema.Effect{Kind: "contract", Data: []byte(`{"index":0}`)}},
		{ID: "effect-2", Identity: outputIdentity, CallID: "call", Index: 1, Effect: agentschema.Effect{Kind: "contract", Data: []byte(`{"index":1}`)}},
	}
	results, err := adapter.ApplyEffects(context.Background(), effects)
	if err != nil || len(results) != len(effects) {
		t.Fatalf("effect results = %#v error = %v", results, err)
	}
	for index, result := range results {
		if result.ID != effects[index].ID || result.Error == "" && strings.TrimSpace(result.Revision) == "" {
			t.Fatalf("effect result %d = %#v", index, result)
		}
	}
}
