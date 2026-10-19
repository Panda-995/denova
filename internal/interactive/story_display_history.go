package interactive

import (
	"fmt"

	agenttool "github.com/alfredxw/denova/agent/tool"
)

const storyDisplayPageTurns = 10

// Presentation reads share ancestry/overlay handling with model history, but
// must never populate the recovery cache with their reduced records.
type storyHistoryView uint8

const (
	storyHistoryModel storyHistoryView = iota
	storyHistoryDisplay
	storyHistoryExecution
)

// DisplaySnapshot returns a small presentation tail and the complete current
// state. Snapshot and StoryContext retain their existing recovery horizon.
func (s *Store) DisplaySnapshot(storyID, branchID string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, snapshot, err := s.storySnapshotForViewLocked(storyID, branchID, storyDisplayPageTurns, storyHistoryDisplay)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.TokenUsageEvents, err = s.readTokenUsageEventsLocked(storyID, snapshot.BranchID)
	return snapshot, err
}

func (s *Store) ReadDisplayHistoryPage(storyID, branchID, before string, limit int) (StoryHistoryPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = storyDisplayPageTurns
	}
	loaded, err := s.readStoryHistoryForViewLocked(storyID, branchID, before, limit, true, storyHistoryDisplay)
	return loaded.page, err
}

// StoryExecutionDetails contains display evidence only. Its cursor pins the
// same journal generation and boundary as the summary the user expanded.
type StoryExecutionDetails struct {
	TurnID        string         `json:"turn_id"`
	Thinking      string         `json:"thinking,omitempty"`
	DisplayEvents []DisplayEvent `json:"display_events,omitempty"`
}

func (s *Store) ReadExecutionDetails(storyID, branchID, cursor string) (StoryExecutionDetails, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	locator, err := decodeStoryHistoryCursor(cursor)
	if err != nil {
		return StoryExecutionDetails{}, err
	}
	loaded, err := s.readStoryHistoryForViewLocked(storyID, branchID, cursor, 1, true, storyHistoryExecution)
	if err != nil {
		return StoryExecutionDetails{}, err
	}
	if len(loaded.page.Turns) != 1 || loaded.page.Turns[0].ID != locator.TargetID {
		return StoryExecutionDetails{}, fmt.Errorf("execution turn is missing from the story journal")
	}
	turn := loaded.page.Turns[0]
	return StoryExecutionDetails{TurnID: turn.ID, Thinking: turn.Thinking, DisplayEvents: turn.DisplayEvents}, nil
}

// These maps were decoded for this read, so removing private model evidence
// cannot mutate the journal or another reader's cached recovery projection.
func storyDisplayRecords(records []locatedStoryRecord) []locatedStoryRecord {
	result := records[:0]
	for _, item := range records {
		switch item.record.Envelope.Type {
		case StoryEventTypeModelContextBatch, StoryEventTypeModelContextProviderContinuation, StoryEventTypeProviderContinuation:
			continue
		case StoryEventTypeTurn:
			delete(item.record.Raw, "model_context_messages")
			delete(item.record.Raw, "resolved_player_input_contexts")
		}
		result = append(result, item)
	}
	return result
}

func summarizeStoryExecution(turn *TurnEvent) bool {
	deferred := turn.Thinking != ""
	turn.Thinking = storyExecutionPreview(turn.Thinking)
	// Media and interaction payloads contribute visible content outside the
	// disclosure. Preserve both halves when results are separate events.
	visibleCalls := make(map[string]bool)
	for _, event := range turn.DisplayEvents {
		if event.ToolPresentation == nil {
			continue
		}
		for _, kind := range []agenttool.ToolPresentationKind{event.ToolPresentation.Call, event.ToolPresentation.Result} {
			switch kind {
			case agenttool.ToolPresentationImage, agenttool.ToolPresentationInteractiveMedia, agenttool.ToolPresentationInteraction, agenttool.ToolPresentationTodo:
				visibleCalls[event.ID] = true
			}
		}
	}
	for index := range turn.DisplayEvents {
		event := &turn.DisplayEvents[index]
		switch event.Role {
		case "thinking", "assistant":
			deferred = true
			event.Content = storyExecutionPreview(event.Content)
		case "tool_call", "tool_result":
			if visibleCalls[event.ID] {
				continue
			}
			deferred = true
			event.Args, event.Result = "", ""
			if event.Role == "tool_result" {
				event.Content = ""
			}
		}
	}
	return deferred
}

func storyExecutionPreview(value string) string {
	// Only the collapsed UI preview is bounded; expansion returns the original.
	remaining := 120
	for index := range value {
		if remaining == 0 {
			return value[:index]
		}
		remaining--
	}
	return value
}
