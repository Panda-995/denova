package interactive

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const narrativeMentionPageTurns = 20
const narrativeMentionLimit = 20

// NarrativeMentionPage is a bounded, read-only projection of one branch path.
// Its cursors are journal locators, never a second history or recovery source.
type NarrativeMentionPage struct {
	BranchID     string             `json:"branch_id"`
	ScannedTurns int                `json:"scanned_turns"`
	BeforeCursor string             `json:"before_cursor,omitempty"`
	HasMore      bool               `json:"has_more"`
	Hits         []NarrativeMention `json:"hits"`
}

type NarrativeMention struct {
	TurnID         string               `json:"turn_id"`
	Timestamp      string               `json:"timestamp"`
	SourceRevision string               `json:"source_revision"`
	Matches        []NarrativeTextMatch `json:"matches"`
	Truncated      bool                 `json:"truncated"`
}

type NarrativeTextMatch struct {
	Line    int    `json:"line"`
	Preview string `json:"preview"`
}

// NarrativeSource deliberately excludes player input, state and execution data.
type NarrativeSource struct {
	TurnID    string `json:"turn_id"`
	Timestamp string `json:"timestamp"`
	Narrative string `json:"narrative"`
}

// FindNarrativeMentions scans one existing history page, releasing the Store
// lock before matching prose. Continuation never rescans the preceding pages.
func (s *Store) FindNarrativeMentions(ctx context.Context, storyID, branchID, query, before string) (NarrativeMentionPage, error) {
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > 1024 {
		return NarrativeMentionPage{}, errors.New("a non-empty narrative query of at most 1024 characters is required")
	}
	if err := ctx.Err(); err != nil {
		return NarrativeMentionPage{}, err
	}
	matcher, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	if err != nil {
		return NarrativeMentionPage{}, err
	}
	s.mu.Lock()
	loaded, err := s.readStoryHistoryForViewLocked(storyID, branchID, before, narrativeMentionPageTurns, true, storyHistoryDisplay)
	if err != nil {
		s.mu.Unlock()
		return NarrativeMentionPage{}, err
	}
	s.mu.Unlock()
	result := NarrativeMentionPage{
		BranchID: loaded.page.BranchID, ScannedTurns: len(loaded.page.Turns),
		BeforeCursor: loaded.page.BeforeCursor, HasMore: loaded.page.HasMore, Hits: []NarrativeMention{},
	}
	for index := len(loaded.page.Turns) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return NarrativeMentionPage{}, err
		}
		turn := loaded.page.Turns[index]
		positions := matcher.FindAllStringIndex(turn.Narrative, narrativeMentionLimit+1)
		if len(positions) == 0 {
			continue
		}
		hit := NarrativeMention{TurnID: turn.ID, Timestamp: turn.Ts, SourceRevision: TurnNarrativeRevision(turn), Matches: []NarrativeTextMatch{}, Truncated: len(positions) > narrativeMentionLimit}
		line, previous := 1, 0
		for _, position := range positions[:min(len(positions), narrativeMentionLimit)] {
			line += strings.Count(turn.Narrative[previous:position[0]], "\n")
			previous = position[0]
			hit.Matches = append(hit.Matches, NarrativeTextMatch{Line: line, Preview: narrativeMentionExcerpt(turn.Narrative, position[0], position[1])})
		}
		result.Hits = append(result.Hits, hit)
	}
	return result, nil
}

func narrativeMentionExcerpt(text string, start, end int) string {
	left, right := start, end
	for count := 0; left > 0 && count < 64; count++ {
		_, size := utf8.DecodeLastRuneInString(text[:left])
		left -= size
	}
	for count := 0; right < len(text) && count < 96; count++ {
		_, size := utf8.DecodeRuneInString(text[right:])
		right += size
	}
	preview := text[left:right]
	if left > 0 {
		preview = "…" + preview
	}
	if right < len(text) {
		preview += "…"
	}
	return preview
}
