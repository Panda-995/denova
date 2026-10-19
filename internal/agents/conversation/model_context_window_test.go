package conversation

import (
	"fmt"
	"testing"

	agentcontext "denova/internal/agents/context"
	agentrun "denova/internal/agents/run"
	"denova/internal/agents/session"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

func TestSessionConversationAssemblesAcceptedInputAfterWindowTrimming(t *testing.T) {
	for _, toolResults := range []int{0, 1, 200} {
		t.Run(fmt.Sprintf("trailing_tool_results_%d", toolResults), func(t *testing.T) {
			directory := t.TempDir()
			store, err := session.NewStore(directory)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			sess, err := store.GetOrCreate("long-writing")
			if err != nil {
				t.Fatal(err)
			}
			var history []*agentschema.Message
			for index := 0; index < 100; index++ {
				history = append(history, agentschema.UserMessage(fmt.Sprintf("old input %d", index)), agentschema.AssistantMessage(fmt.Sprintf("old answer %d", index), nil))
			}
			if err := sess.AppendContextMessages(history...); err != nil {
				t.Fatal(err)
			}
			conversation := NewSessionConversation(sess)
			identity := agentrun.CycleIdentity{CommandID: "current-input", OperationID: "current-run", Cycle: 1}
			conversation.BindAgentCycleIdentity(identity)
			input := agentcontext.ModelContextInput{UserMessage: "continue the current chapter", Budget: conversation.ModelContextBudget()}
			receipt, err := conversation.MaterializeAgentCanonicalInput(t.Context(), input.UserMessage, input.Attachments, input.UserReferences, nil)
			if err != nil {
				t.Fatal(err)
			}
			if toolResults > 0 {
				calls := make([]agentschema.ToolCall, toolResults)
				messages := make([]agentschema.Message, toolResults+1)
				for index := range calls {
					callID := fmt.Sprintf("call-%d", index)
					calls[index] = agentschema.ToolCall{ID: callID, Type: "function", Function: agentschema.FunctionCall{Name: "inspect", Arguments: `{}`}}
					messages[index+1] = *agentschema.ToolMessage(agentschema.TextToolResult("chapter evidence"), callID, agentschema.WithToolName("inspect"))
				}
				messages[0] = *agentschema.AssistantMessage("checking chapters", calls)
				if _, err := conversation.CommitAgentCanonicalContext(t.Context(), agentcanonical.ContextCommitRequest{
					Identity: agentcanonical.CommitIdentity{CommandID: string(identity.CommandID), RunID: string(identity.OperationID), Cycle: identity.Cycle, Stage: agentcanonical.CommitContext},
					Sequence: 0, Messages: messages,
				}); err != nil {
					t.Fatal(err)
				}
			}
			messageCount := sess.MessageCountTotal()
			for _, phase := range []string{"hot", "reopened"} {
				if phase == "reopened" {
					if err := store.Close(); err != nil {
						t.Fatal(err)
					}
					store, err = session.NewStore(directory)
					if err != nil {
						t.Fatal(err)
					}
					sess, err = store.Get("long-writing")
					if err != nil {
						t.Fatal(err)
					}
					conversation = NewSessionConversation(sess)
					conversation.BindAgentCycleIdentity(identity)
				}
				t.Run(phase, func(t *testing.T) {
					assembled, err := conversation.AssembleModelContext(t.Context(), input.UserMessage, input)
					if err != nil {
						t.Fatal(err)
					}
					acceptedInputs := 0
					for _, message := range assembled.Messages {
						if message.Role == agentschema.User && message.Content == input.UserMessage {
							acceptedInputs++
						}
					}
					if acceptedInputs != 1 {
						t.Fatalf("assembled accepted inputs=%d want=1", acceptedInputs)
					}
					if retry, err := conversation.MaterializeAgentCanonicalInput(t.Context(), input.UserMessage, input.Attachments, input.UserReferences, nil); err != nil || retry != receipt {
						t.Fatalf("accepted input retry=%#v want=%#v err=%v", retry, receipt, err)
					}
					if sess.MessageCountTotal() != messageCount {
						t.Fatalf("context assembly duplicated canonical input: count=%d want=%d", sess.MessageCountTotal(), messageCount)
					}
				})
			}
		})
	}
}
