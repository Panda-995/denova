package engine

import (
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestCanonicalToolBatchAssistantAcceptsFinalizedAgentResponse(t *testing.T) {
	current := agentschema.AssistantMessage("checking", []agentschema.ToolCall{{
		ID: "call-1", Type: "function",
		Function: agentschema.FunctionCall{Name: "write", Arguments: `{path: 'draft.md'}`},
	}})
	current.AgentMeta = &agentschema.AgentMessageMeta{ModelResponseOrdinal: 2}
	candidate := current.Clone()
	candidate.AgentMeta = nil
	candidate.ToolCalls[0].Function.Arguments = `{"path":"draft.md"}`
	candidate.ToolCalls[0].Function.Name = "write_file"

	canonical, err := canonicalToolBatchAssistant(current, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.ToolCalls[0].Function.Name != "write_file" ||
		canonical.ToolCalls[0].Function.Arguments != `{"path":"draft.md"}` ||
		canonical.AgentMeta == nil || canonical.AgentMeta.ModelResponseOrdinal != 2 {
		t.Fatalf("canonical assistant = %#v", canonical)
	}

	mismatched := candidate.Clone()
	mismatched.AgentMeta = &agentschema.AgentMessageMeta{ModelResponseOrdinal: 3}
	if _, err := canonicalToolBatchAssistant(current, mismatched); err == nil {
		t.Fatal("canonical boundary accepted a different model response identity")
	}
}

func TestEngineTranscriptPreservesNextContextSequence(t *testing.T) {
	encoded, err := encodeEngineTranscript(preparedDefinition{contextSequence: 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeEngineTranscript(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if state.ContextSequence != 4 {
		t.Fatalf("context sequence = %d, want 4", state.ContextSequence)
	}
}
