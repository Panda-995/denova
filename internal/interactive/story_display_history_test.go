package interactive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestDisplayHistoryDefersExecutionWithoutReducingRecovery(t *testing.T) {
	root := t.TempDir()
	seed := NewStore(root)
	story, err := seed.CreateStory(CreateStoryRequest{Title: "History", StoryTellerID: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := seed.readStoryJournalLocked(story.ID)
	if err != nil {
		t.Fatal(err)
	}
	trace := strings.Repeat("tool evidence ", 4096)
	thinking := strings.Repeat("思考过程", 1024)
	media := `{"url":"/api/assets/image.png"}`
	turns := make([]TurnEvent, 60)
	lines := []any{meta}
	for index := range turns {
		var parent any
		if index > 0 {
			parent = turns[index-1].ID
		}
		turns[index] = TurnEvent{
			V: schemaVersion, Type: StoryEventTypeTurn, ID: fmt.Sprintf("turn-%d", index), ParentID: parent,
			BranchID: "main", Ts: "2026-01-01T00:00:00Z", User: "Continue", Narrative: fmt.Sprintf("Narrative %d", index), Thinking: thinking,
			ModelContextMessages: []ModelContextMessage{{Role: "tool", ToolCallID: "tool", ToolName: "read_file", Content: trace}},
			DisplayEvents: []DisplayEvent{
				{ID: "thinking", Role: "thinking", Content: thinking},
				{ID: "tool", Role: "tool_call", Name: "read_file", Args: trace, Status: "success"},
				{ID: "tool", Role: "tool_result", Result: trace, Status: "success"},
				{ID: "narrative", Role: DisplayEventRoleNarrative},
				{ID: "image", Role: "tool_call", Name: "image", ToolPresentation: &agenttool.ToolPresentation{Call: agenttool.ToolPresentationInteractiveMedia, Result: agenttool.ToolPresentationInteractiveMedia}},
				{ID: "image", Role: "tool_result", Result: media, Status: "success"},
				{ID: "progress", Role: "assistant", Content: trace},
			},
		}
		lines = append(lines, turns[index])
	}
	branch := meta.Branches["main"]
	branch.Head = turns[len(turns)-1].ID
	meta.Branches["main"] = branch
	lines[0] = meta
	if err := writeJSONL(seed.storyPath(story.ID), lines); err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(seed.storyPath(story.ID))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(root)
	snapshot, err := store.DisplaySnapshot(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Turns) != 10 || snapshot.TurnCount != 60 || snapshot.TurnStart != 50 || !snapshot.HasEarlierTurns {
		t.Fatalf("unexpected display window: count=%d total=%d start=%d more=%t", len(snapshot.Turns), snapshot.TurnCount, snapshot.TurnStart, snapshot.HasEarlierTurns)
	}
	if stats := store.LastStoryJournalReplayStats(story.ID); stats.RecordsRead > 11 {
		t.Fatalf("display read scaled with the whole story: %+v", stats)
	}
	for _, turn := range snapshot.Turns {
		if turn.ExecutionCursor == "" || len(turn.ModelContextMessages) != 0 || turn.DisplayEvents[1].Args != "" || turn.DisplayEvents[2].Result != "" || turn.DisplayEvents[5].Result != media {
			t.Fatalf("display did not defer private evidence or lost media: %s", turn.ID)
		}
	}
	detail, err := store.ReadExecutionDetails(story.ID, "main", snapshot.Turns[0].ExecutionCursor)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TurnID != turns[50].ID || detail.Thinking != thinking || !reflect.DeepEqual(detail.DisplayEvents, sanitizeDisplayEvents(turns[50].DisplayEvents)) {
		t.Fatal("execution details did not preserve the original trace")
	}
	if _, err := store.ReadExecutionDetails(story.ID, "other", snapshot.Turns[0].ExecutionCursor); err == nil {
		t.Fatal("accepted details from another branch")
	}
	response, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(response) > 32*1024 {
		t.Fatalf("display payload includes hidden evidence: %d bytes", len(response))
	}
	// A UI read must not install its reduced page in the recovery cache.
	context, err := store.StoryContext(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Snapshot.Turns) != len(turns) || len(context.Snapshot.CurrentTurn.ModelContextMessages) != 1 || context.Snapshot.CurrentTurn.ModelContextMessages[0].Content != trace || context.Snapshot.CurrentTurn.DisplayEvents[1].Args != trace {
		t.Fatal("display read contaminated model recovery")
	}
	all := append([]TurnEvent(nil), snapshot.Turns...)
	for cursor := snapshot.HistoryBeforeCursor; cursor != ""; {
		page, err := store.ReadDisplayHistoryPage(story.ID, "main", cursor, 10)
		if err != nil {
			t.Fatal(err)
		}
		all = append(page.Turns, all...)
		cursor = page.BeforeCursor
	}
	if len(all) != len(turns) {
		t.Fatalf("pagination returned %d of %d turns", len(all), len(turns))
	}
	for index, turn := range all {
		if turn.ID != turns[index].ID || turn.Narrative != turns[index].Narrative {
			t.Fatalf("incorrect paged turn %d", index)
		}
	}
	after, err := os.ReadFile(seed.storyPath(story.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, after) {
		t.Fatal("presentation changed the canonical journal")
	}
}

func TestDisplayHistoryKeepsVersionsAtThePageBoundary(t *testing.T) {
	for _, prefix := range []int{0, 3} {
		t.Run(fmt.Sprintf("preceding-turns-%d", prefix), func(t *testing.T) {
			root := t.TempDir()
			seed := NewStore(root)
			story, err := seed.CreateStory(CreateStoryRequest{Title: "Versions", StoryTellerID: "classic"})
			if err != nil {
				t.Fatal(err)
			}
			meta, _, err := seed.readStoryJournalLocked(story.ID)
			if err != nil {
				t.Fatal(err)
			}
			lines := []any{meta}
			var parent any
			for index := range prefix {
				id := fmt.Sprintf("prefix-%d", index)
				lines = append(lines, TurnEvent{V: schemaVersion, Type: StoryEventTypeTurn, ID: id, ParentID: parent, BranchID: "main", Ts: "2025-12-31T23:59:00Z", Narrative: "Earlier"})
				parent = id
			}
			for index := range 23 {
				lines = append(lines, TurnEvent{V: schemaVersion, Type: StoryEventTypeTurn, ID: fmt.Sprintf("version-%02d", index), ParentID: parent, BranchID: "main", Ts: fmt.Sprintf("2026-01-01T00:00:%02dZ", index), Narrative: "Opening"})
			}
			parent = "version-22"
			if prefix > 0 {
				for index := range 9 {
					id := fmt.Sprintf("suffix-%d", index)
					lines = append(lines, TurnEvent{V: schemaVersion, Type: StoryEventTypeTurn, ID: id, ParentID: parent, BranchID: "main", Ts: "2026-01-01T00:01:00Z", Narrative: "Later"})
					parent = id
				}
			}
			branch := meta.Branches["main"]
			branch.Head = parent.(string)
			meta.Branches["main"] = branch
			lines[0] = meta
			if err := writeJSONL(seed.storyPath(story.ID), lines); err != nil {
				t.Fatal(err)
			}
			snapshot, err := NewStore(root).DisplaySnapshot(story.ID, "main")
			if err != nil {
				t.Fatal(err)
			}
			turn := snapshot.Turns[0]
			if turn.ID != "version-22" || len(turn.Versions) != 23 || turn.VersionIdx != 22 {
				t.Fatalf("display boundary lost earlier versions: %+v", turn)
			}
		})
	}
}

func TestDisplayHistoryPaginatesTurnsInOneTransaction(t *testing.T) {
	store := NewStore(t.TempDir())
	story, err := store.CreateStory(CreateStoryRequest{Title: "Batched", StoryTellerID: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	requests := make([]AppendTurnRequest, 23)
	for index := range requests {
		requests[index] = AppendTurnRequest{User: "Continue", Narrative: fmt.Sprintf("Turn %d", index)}
	}
	appendStoryTurns(t, store, story.ID, "main", requests)
	snapshot, err := store.DisplaySnapshot(story.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	all := snapshot.Turns
	for cursor := snapshot.HistoryBeforeCursor; cursor != ""; {
		page, err := store.ReadDisplayHistoryPage(story.ID, "main", cursor, 10)
		if err != nil {
			t.Fatal(err)
		}
		all = append(page.Turns, all...)
		cursor = page.BeforeCursor
	}
	if len(all) != len(requests) {
		t.Fatalf("pagination returned %d turns", len(all))
	}
	for index, turn := range all {
		if turn.Narrative != requests[index].Narrative {
			t.Fatalf("incorrect batched turn %d", index)
		}
	}
}
