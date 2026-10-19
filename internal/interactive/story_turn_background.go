package interactive

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var errTurnBackground = errors.New("current background is unavailable")

// UpdateTurnBackgroundRequest edits the latest logical turn. A nil background
// explicitly clears the layer; supplied paths are resolved from Lore server-side.
type UpdateTurnBackgroundRequest struct {
	BranchID   string                `json:"branch_id"`
	TurnID     string                `json:"-"`
	Background *PresentationMaterial `json:"background"`
}

// TurnBackgroundRevisedEvent preserves manual choices in the canonical journal
// without replacing prose, characters, settlement, or turn identity.
type TurnBackgroundRevisedEvent struct {
	V          int                   `json:"v"`
	Type       string                `json:"type"`
	ID         string                `json:"id"`
	ParentID   string                `json:"parent_id"`
	BranchID   string                `json:"branch_id"`
	Ts         string                `json:"ts"`
	TurnID     string                `json:"turn_id"`
	Background *PresentationMaterial `json:"background"`
}

func (s *Store) UpdateTurnBackground(storyID string, req UpdateTurnBackgroundRequest) error {
	if req.Background != nil && !req.Background.Focus.valid() {
		return fmt.Errorf("%w: invalid image focus", errTurnBackground)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.acquireStoryMutationLeaseLocked(storyID)
	if err != nil {
		return err
	}
	defer release()
	meta, lines, err := s.readStoryRecentLocked(storyID, req.BranchID)
	if err != nil {
		return err
	}
	branchID := req.BranchID
	if branchID == "" {
		branchID = meta.CurrentBranch
	}
	if err := requireLatestLogicalTurn(meta, lines, branchID, req.TurnID); err != nil {
		return err
	}
	// Manual choices are independent of the Agent dynamic-selection permission.
	raw, err := json.Marshal(map[string]any{"background": req.Background})
	if err != nil {
		return err
	}
	stage, receipt := ResolvePresentationPatch(s.root, nil, raw, nil)
	if receipt.Ignored > 0 {
		return fmt.Errorf("%w: %v", errTurnBackground, receipt.Reasons)
	}
	if stage.Background != nil {
		stage.Background.Focus = req.Background.Focus
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := TurnBackgroundRevisedEvent{V: schemaVersion, Type: StoryEventTypeTurnBackgroundRevised, ID: newID("tbr"), ParentID: req.TurnID, BranchID: branchID, Ts: now, TurnID: req.TurnID, Background: stage.Background}
	meta.UpdatedAt = now
	if err := s.appendStoryTransactionLocked(storyID, meta, event); err != nil {
		return err
	}
	return s.syncStorySummaryLocked(storyID)
}
