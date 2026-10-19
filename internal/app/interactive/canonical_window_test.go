package interactiveapp

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"denova/internal/agents/conversationjournal"
	"denova/internal/interactive"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

func assertGameCanonicalWindow(t *testing.T, conversation *Conversation) {
	t.Helper()
	full, err := conversation.CanonicalMessages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	full = agentcanonical.CanonicalContextStateOrder(full)
	archives := []*agenthistory.HistoryArchive{nil}
	if len(full) > 4 {
		archives = append(archives, &agenthistory.HistoryArchive{From: 1, To: len(full) - 2, Retained: []int{1, len(full) - 3}})
	}
	for _, archive := range archives {
		want, err := archive.SelectMessages(full)
		if err != nil {
			t.Fatal(err)
		}
		got, err := conversation.ReadCanonicalWindow(t.Context(), len(full), archive)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Game window changed canonical projection: archive=%+v err=%v\nwant=%+v\ngot=%+v", archive, err, want, got)
		}
	}
}

func TestGameCanonicalWindowKeepsBranchEditsRegenerationAndRebuiltIndexes(t *testing.T) {
	workspace := t.TempDir()
	store := interactive.NewStore(workspace)
	story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "branch window", StoryTellerID: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	var latest interactive.TurnEvent
	for i := range 24 {
		latest, err = store.AppendTurn(story.ID, interactive.AppendTurnRequest{BranchID: "main", User: fmt.Sprintf("request-%d", i), Narrative: fmt.Sprintf("narrative-%d %s", i, strings.Repeat("evidence", 800))})
		if err != nil {
			t.Fatal(err)
		}
	}
	fork, err := store.CreateBranch(story.ID, interactive.CreateBranchRequest{ParentEventID: latest.ID, Title: "frozen branch"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateTurnNarrative(story.ID, interactive.UpdateTurnNarrativeRequest{BranchID: "main", TurnID: latest.ID, Narrative: "revised main narrative"}); err != nil {
		t.Fatal(err)
	}
	forkTurn, err := store.AppendTurn(story.ID, interactive.AppendTurnRequest{BranchID: fork.ID, User: "fork input", Narrative: "fork result"})
	if err != nil {
		t.Fatal(err)
	}
	replacement, _, err := store.AppendTurnWithState(story.ID, interactive.AppendTurnWithStateRequest{BranchID: fork.ID, ReplaceTurnID: forkTurn.ID, User: "fork input", Narrative: "regenerated fork result"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SwitchTurnVersion(story.ID, interactive.SwitchTurnVersionRequest{BranchID: fork.ID, TurnID: replacement.ID, VersionTurnID: forkTurn.ID}); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("%s/interactive/story/story-%s.jsonl", workspace, story.ID)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, cache := range []string{"valid", "missing", "corrupt"} {
		t.Run(cache, func(t *testing.T) {
			if cache == "missing" {
				if err := os.Remove(conversationjournal.SidecarPath(path)); err != nil {
					t.Fatal(err)
				}
			}
			if cache == "corrupt" {
				if err := os.WriteFile(conversationjournal.SidecarPath(path), []byte(`{"broken":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cold := interactive.NewStore(workspace)
			defer cold.Close()
			for _, branchID := range []string{"main", fork.ID} {
				conversation := NewConversation(cold, "", workspace, story.ID, branchID, "", 0, nil)
				assertGameCanonicalWindow(t, conversation)
				full, err := conversation.CanonicalMessages(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				archive := &agenthistory.HistoryArchive{From: 2, To: len(full) - 2}
				if _, err := conversation.ReadCanonicalWindow(t.Context(), len(full), archive); err != nil {
					t.Fatal(err)
				}
				stats := cold.LastStoryJournalReplayStats(story.ID)
				if stats.BytesRead >= int64(len(canonical))/2 {
					t.Fatalf("small window reread archived bodies: %d of %d bytes", stats.BytesRead, len(canonical))
				}
			}
			assertGameCanonicalWindow(t, NewConversation(cold, "", workspace, story.ID, fork.ID, "", 0, nil).WithRegenerateTarget(forkTurn.ID))
			for _, branchID := range []string{"main", fork.ID} {
				conversation := NewConversation(cold, "", workspace, story.ID, branchID, "", 0, nil)
				full, _ := conversation.CanonicalMessages(t.Context())
				if _, err := conversation.ReadCanonicalWindow(t.Context(), len(full)+1, nil); err == nil {
					t.Fatal("accepted a stale message count")
				}
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, canonical) {
				t.Fatal("recovery rewrote canonical Story JSONL")
			}
		})
	}
}

func TestGameCanonicalWindowPreservesDelayedAndPendingInputOrder(t *testing.T) {
	workspace := t.TempDir()
	store := interactive.NewStore(workspace)
	defer store.Close()
	story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "interrupted window", StoryTellerID: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	accept := func(command string) interactive.DomainCommitIdentity {
		identity := interactive.DomainCommitIdentity{CommandID: command, OperationID: "operation-" + command, Cycle: 1}
		intent, err := interactive.NewPlayerInputIntent(identity, "main", "input-"+command)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CommitPlayerInput(story.ID, intent); err != nil {
			t.Fatal(err)
		}
		batch, err := interactive.NewAgentContextBatchIntent(identity, "main", 0, []interactive.ModelContextMessage{
			{Role: "assistant", ToolCalls: []interactive.ModelContextToolCall{{ID: "same-call-id", Type: "function", Function: interactive.ModelContextFunctionCall{Name: "read", Arguments: `{}`}}}},
			{Role: "tool", ToolCallID: "same-call-id", Content: "evidence-" + command},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AppendModelContextBatch(story.ID, batch); err != nil {
			t.Fatal(err)
		}
		return identity
	}
	accept("first-interrupted")
	if _, err := store.AppendTurn(story.ID, interactive.AppendTurnRequest{BranchID: "main", User: "intervening request", Narrative: "intervening answer"}); err != nil {
		t.Fatal(err)
	}
	accept("second-interrupted")
	assertGameCanonicalWindow(t, NewConversation(store, "", workspace, story.ID, "main", "", 0, nil))
	current := accept("current")
	if _, _, err := store.AppendTurnWithState(story.ID, interactive.AppendTurnWithStateRequest{BranchID: "main", User: "input-current", Narrative: "current answer", AgentCommandID: current.CommandID, AgentOperationID: current.OperationID, AgentCycle: current.Cycle}); err != nil {
		t.Fatal(err)
	}
	assertGameCanonicalWindow(t, NewConversation(store, "", workspace, story.ID, "main", "", 0, nil))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	cold := interactive.NewStore(workspace)
	defer cold.Close()
	assertGameCanonicalWindow(t, NewConversation(cold, "", workspace, story.ID, "main", "", 0, nil))
}
