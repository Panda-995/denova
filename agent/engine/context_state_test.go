package engine

import (
	"strings"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestContextStateValidationRejectsAmbiguousIdentity(t *testing.T) {
	fragment := testContextStateFragment("revision-1", "state")
	duplicate := fragment
	duplicate.Resource = "another-resource"
	if err := validateContextFragments([]agentschema.ContextFragment{fragment, duplicate}); err == nil || !strings.Contains(err.Error(), "reuse StateID") {
		t.Fatalf("duplicate StateID error = %v", err)
	}
	fragment.StateID = ""
	if err := validateContextFragments([]agentschema.ContextFragment{fragment}); err == nil || !strings.Contains(err.Error(), "requires state_message placement and StateID") {
		t.Fatalf("missing StateID error = %v", err)
	}
}

func testContextStateFragment(revision, content string) agentschema.ContextFragment {
	return agentschema.ContextFragment{
		Source: "test.workspace", Purpose: "provide current workspace state", Resource: "workspace",
		Revision: revision, StateID: "workspace", Stability: agentschema.ContextSessionState, Placement: agentschema.ContextStateMessage,
		Rendering: agentschema.ContextRenderVerbatim, Content: content, HardLimit: 64 << 10,
	}
}
