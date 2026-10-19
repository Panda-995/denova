package interactive

import (
	"encoding/json"
	"fmt"
	"time"
)

// UpdateTurnPresentationRequest edits the latest logical turn through the same
// media patch reducer as Agent submissions. Material paths are resolved server-side.
type UpdateTurnPresentationRequest struct {
	BranchID     string          `json:"branch_id"`
	TurnID       string          `json:"-"`
	Presentation json.RawMessage `json:"presentation"`
}

// TurnBackgroundRevisedEvent reads background edits saved by v0.6.1.
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

// TurnPresentationRevisedEvent preserves manual media selections in the owning
// Story journal without replacing prose, settlement, or turn identity.
type TurnPresentationRevisedEvent struct {
	V            int               `json:"v"`
	Type         string            `json:"type"`
	ID           string            `json:"id"`
	ParentID     string            `json:"parent_id"`
	BranchID     string            `json:"branch_id"`
	Ts           string            `json:"ts"`
	TurnID       string            `json:"turn_id"`
	Presentation *TurnPresentation `json:"presentation"`
}

func (s *Store) UpdateTurnPresentation(storyID string, req UpdateTurnPresentationRequest) error {
	var requested struct {
		Background *PresentationMaterial `json:"background"`
	}
	if err := json.Unmarshal(req.Presentation, &requested); err != nil {
		return fmt.Errorf("invalid presentation patch: %w", err)
	}
	if requested.Background != nil && !requested.Background.Focus.valid() {
		return fmt.Errorf("invalid image focus")
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
	snapshot, err := snapshotFromLines(storyID, branchID, meta, lines)
	if err != nil {
		return err
	}
	settings := NormalizeStoryPresentationSettings(meta.PresentationSettings)
	base := &TurnPresentation{Background: settings.DefaultBackground, Ambient: settings.DefaultAmbient}
	if snapshot.CurrentTurn.TurnResult != nil && snapshot.CurrentTurn.TurnResult.Presentation != nil {
		base = snapshot.CurrentTurn.TurnResult.Presentation
	}
	// Explicit creator choices are independent of Agent selection permissions.
	stage, receipt := ResolvePresentationPatch(s.root, base, req.Presentation, &StoryPresentationSettings{Background: true, Characters: true, Ambient: true})
	if receipt == nil || receipt.Applied == 0 || receipt.Ignored > 0 {
		return fmt.Errorf("presentation patch was not applied: %+v", receipt)
	}
	if stage.Background != nil && requested.Background != nil {
		stage.Background.Focus = requested.Background.Focus
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := TurnPresentationRevisedEvent{V: schemaVersion, Type: StoryEventTypeTurnPresentationRevised, ID: newID("tpr"), ParentID: req.TurnID, BranchID: branchID, Ts: now, TurnID: req.TurnID, Presentation: stage}
	meta.UpdatedAt = now
	if err := s.appendStoryTransactionLocked(storyID, meta, event); err != nil {
		return err
	}
	return s.syncStorySummaryLocked(storyID)
}
