package canonical

import (
	"strings"
	"testing"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestValidateContextCommitDerivesEveryBatchShape(t *testing.T) {
	state := agenthistory.NewContextStateMessage(
		testContextStateFragment("revision-1", "workspace"),
		strings.Repeat("a", 64),
		"initialize",
		"",
	)
	call := agentschema.AssistantMessage("", []agentschema.ToolCall{{
		ID: "call-1", Type: "function", Function: agentschema.FunctionCall{Name: "inspect", Arguments: `{}`},
	}})
	result := agentschema.ToolMessage(agentschema.TextToolResult("complete"), "call-1", agentschema.WithToolName("inspect"))
	completion := agentschema.UserMessage("child result")
	completion.TaskCompletion = &agentschema.TaskCompletionMessageMeta{
		CompletionID: "completion-1", Author: "researcher", Recipient: "parent",
	}

	valid := []struct {
		name     string
		messages []*agentschema.Message
	}{
		{name: "state", messages: []*agentschema.Message{state}},
		{name: "tool batch", messages: []*agentschema.Message{call, result}},
		{name: "task completion", messages: []*agentschema.Message{completion}},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			request := ContextCommitRequest{
				Identity: CommitIdentity{Stage: CommitContext}, Sequence: 0,
				Messages: messageValues(test.messages),
			}
			if err := ValidateContextCommit(request); err != nil {
				t.Fatalf("valid canonical context rejected: %v", err)
			}
		})
	}

	invalid := []struct {
		name     string
		messages []*agentschema.Message
	}{
		{name: "ordinary user input", messages: []*agentschema.Message{agentschema.UserMessage("request")}},
		{name: "tool batch missing result", messages: []*agentschema.Message{call}},
		{name: "tool batch mismatched result", messages: []*agentschema.Message{call, agentschema.ToolMessage(agentschema.TextToolResult("complete"), "call-2")}},
		{name: "task completion without identity", messages: []*agentschema.Message{agentschema.UserMessage("child result")}},
		{name: "mixed state and completion", messages: []*agentschema.Message{state, completion}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateContextCommitMessages(test.messages); err == nil {
				t.Fatal("invalid canonical context was accepted")
			}
		})
	}
}

func TestValidateContextCommitRejectsInvalidProtocolIdentity(t *testing.T) {
	message := agentschema.UserMessage("child result")
	message.TaskCompletion = &agentschema.TaskCompletionMessageMeta{
		CompletionID: "completion-1", Author: "researcher", Recipient: "parent",
	}
	for _, request := range []ContextCommitRequest{
		{Identity: CommitIdentity{Stage: CommitOutput}, Sequence: 0, Messages: messageValues([]*agentschema.Message{message})},
		{Identity: CommitIdentity{Stage: CommitContext}, Sequence: -1, Messages: messageValues([]*agentschema.Message{message})},
	} {
		if err := ValidateContextCommit(request); err == nil {
			t.Fatal("invalid context commit identity was accepted")
		}
	}
}

func messageValues(messages []*agentschema.Message) []agentschema.Message {
	values := make([]agentschema.Message, len(messages))
	for index, message := range messages {
		values[index] = *message.Clone()
	}
	return values
}
