package history

import (
	"reflect"
	"strings"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestCompactionGroupsProtectNewestCompleteStepAndIncompleteSuffix(t *testing.T) {
	old := []*agentschema.Message{agentschema.UserMessage("old task"), agentschema.AssistantMessage(strings.Repeat("old evidence ", 1000), nil)}
	latest := []*agentschema.Message{agentschema.UserMessage("current task"), agentschema.AssistantMessage("", []agentschema.ToolCall{
		{ID: "a", Function: agentschema.FunctionCall{Name: "read"}}, {ID: "b", Function: agentschema.FunctionCall{Name: "read"}},
	}), {Role: agentschema.ToolRole, ToolCallID: "a", Content: "a result"}, {Role: agentschema.ToolRole, ToolCallID: "b", Content: "b result"}}
	for _, suffix := range [][]*agentschema.Message{nil, {agentschema.UserMessage("unconsumed steering")}, {agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "pending", Function: agentschema.FunctionCall{Name: "read"}}})}} {
		messages := append(agentschema.CloneMessages(old), agentschema.CloneMessages(latest)...)
		messages = append(messages, agentschema.CloneMessages(suffix)...)
		groups, ends, _ := compactionGroups(messages, messages, CompactionRecord{}, false)
		if !reflect.DeepEqual(ends, []int{2}) || len(groups) != 1 || !reflect.DeepEqual(groups[0].Messages, old) {
			t.Fatalf("groups=%#v ends=%v", groups, ends)
		}
		groups[0].Messages[0].Content = "changed"
		if messages[0].Content != "old task" {
			t.Fatal("extension mutated journal source")
		}
	}
	incomplete := append(agentschema.CloneMessages(old), latest[:3]...)
	if groups, _, _ := compactionGroups(incomplete, incomplete, CompactionRecord{}, false); len(groups) != 0 {
		t.Fatal("incomplete batch displaced newest complete step")
	}
}

func TestCompactionGroupsOfferOnlyNewDeltaAfterCheckpoint(t *testing.T) {
	messages := []*agentschema.Message{agentschema.UserMessage("old"), agentschema.AssistantMessage("old answer", nil), agentschema.UserMessage("new"), agentschema.AssistantMessage("new answer", nil), agentschema.UserMessage("latest"), agentschema.AssistantMessage("latest answer", nil)}
	current := CompactionRecord{ReplacementTo: 2}
	groups, ends, _ := compactionGroups(messages, messages, current, true)
	if !reflect.DeepEqual(ends, []int{4}) || len(groups) != 1 || !reflect.DeepEqual(groups[0].Messages, messages[2:4]) {
		t.Fatalf("groups=%#v ends=%v", groups, ends)
	}
}

func TestCompactionViewCannotAcquireCoverageThroughJSONOrCallerFields(t *testing.T) {
	state := CompactionStatePointer(CompactionRecord{ID: "checkpoint", Revision: 1, Summary: "truth", ReplacementTo: 2}, true)
	raw := []*agentschema.Message{agentschema.UserMessage("old"), agentschema.AssistantMessage("old answer", nil), agentschema.UserMessage("latest")}
	state.Summary = "caller edit"
	projected, err := state.Project(raw, 1024)
	if err != nil || len(projected) != 2 || !strings.Contains(projected[0].Content, "truth") || strings.Contains(projected[0].Content, "caller edit") {
		t.Fatalf("projection=%#v err=%v", projected, err)
	}
	if _, err := (CompactionState{ID: "forged", Revision: 1, Summary: "forged"}).Project(raw, 1024); err == nil {
		t.Fatal("caller invented projection authority")
	}
}
