package agentui

import (
	"bytes"
	"testing"

	agentchat "denova/internal/agents/chat"
	agentconversation "denova/internal/agents/conversation"
	agentrun "denova/internal/agents/run"
	"denova/internal/agents/session"
)

// Exercise the persisted display recorder and wire encoder together: a wire
// segment must have the same complete body as its durable display record.
func TestStreamSegmentsMatchHistoryAcrossInterleavedNotifications(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess, err := store.GetOrCreate("stream-segments")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	encoder := NewStreamEncoder(&out, "segments")
	projector := agentchat.NewPublicEventProjector(agentconversation.NewSessionConversation(sess), agentchat.ChatRequest{}, agentrun.Options{}, func(event agentrun.Event) {
		if err := encoder.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	})
	root := func(content string) map[string]any {
		return map[string]any{"run_id": "run", "agent_name": "root", "content": content}
	}
	child := func(content string) map[string]any {
		return map[string]any{"run_id": "run", "agent_name": "child", "subagent": true, "subagent_session_id": "child", "content": content}
	}
	projector.EmitProduct(agentrun.Event{Type: "chunk", Data: root("Root before. ")})
	projector.EmitProduct(agentrun.Event{Type: "thinking", Data: child("Child before. ")})
	for _, event := range []agentrun.Event{
		{Type: "workspace_change", Data: map[string]any{"id": "change", "project_id": "project", "path": "chapter.md"}},
		{Type: "tool_progress", Data: child("Working")},
		{Type: "tool_started", Data: map[string]any{"id": "unscoped-tool", "name": "read"}},
		{Type: "todo_updated", Data: map[string]any{"id": "todo", "runtime_managed": true}},
		{Type: "interactive_image", Data: map[string]any{"id": "image"}},
		{Type: "rule_roll", Data: map[string]any{"id": "roll"}},
		{Type: "context_compaction", Data: map[string]any{"id": "compaction", "status": "started"}},
	} {
		projector.EmitProduct(event)
		projector.EmitProduct(agentrun.Event{Type: "chunk", Data: root(event.Type + ". ")})
		projector.EmitProduct(agentrun.Event{Type: "thinking", Data: child(event.Type + ". ")})
	}
	tool := child("")
	tool["id"], tool["name"], tool["args"] = "child-tool", "read", `{}`
	projector.EmitProduct(agentrun.Event{Type: "tool_call", Data: tool})
	projector.EmitProduct(agentrun.Event{Type: "chunk", Data: root("Root after child tool. ")})
	projector.EmitProduct(agentrun.Event{Type: "thinking", Data: child("New child segment.")})
	projector.EmitProduct(agentrun.Event{Type: "token_usage", Data: root("")})
	projector.EmitProduct(agentrun.Event{Type: "chunk", Data: root("New root segment.")})
	projector.EmitProduct(agentrun.Event{Type: "done", Data: map[string]any{}})

	chunks, done := parseUIStreamChunks(t, out.String())
	if !done {
		t.Fatal("stream did not finish")
	}
	bodies, starts, ends := map[string]string{}, map[string]int{}, map[string]int{}
	for _, chunk := range chunks {
		id, _ := chunk["id"].(string)
		switch chunk["type"] {
		case "text-start", "reasoning-start":
			starts[id]++
		case "text-end", "reasoning-end":
			ends[id]++
		case "text-delta", "reasoning-delta":
			bodies[id] += chunk["delta"].(string)
		}
	}
	segments := 0
	for _, entry := range sess.History() {
		if entry.Role != "assistant" && entry.Role != "thinking" {
			continue
		}
		segments++
		id := entry.DisplaySegmentID
		if starts[id] != 1 || ends[id] != 1 || bodies[id] != entry.Content {
			t.Errorf("segment %s: starts=%d ends=%d body=%q; durable body=%q", id, starts[id], ends[id], bodies[id], entry.Content)
		}
	}
	if segments != 4 || len(starts) != segments {
		t.Fatalf("wire segments=%d durable segments=%d, want 4", len(starts), segments)
	}
}
