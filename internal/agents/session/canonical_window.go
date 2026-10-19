package session

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"denova/internal/agents/conversationjournal"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

type canonicalAnchor struct {
	Index       int                        `json:"index"`
	Cursor      conversationjournal.Cursor `json:"cursor"`
	RecordIndex int                        `json:"record_index,omitempty"`
	BatchIndex  int                        `json:"batch_index,omitempty"`
}

type canonicalSpan struct{ from, through int }

// ReadCanonicalWindow seeks semantic anchors and visits only selected spans.
// Archived coordinates, context-state order and tool pairing are unchanged.
func (s *Session) ReadCanonicalWindow(ctx context.Context, count int, archive *agenthistory.HistoryArchive) ([]*agentschema.Message, error) {
	if s == nil {
		return nil, fmt.Errorf("session is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshCanonicalTailLocked(); err != nil {
		return nil, err
	}
	actual := s.projection.CanonicalCount
	if s.projection.CanonicalPendingUser {
		actual++
	}
	if actual != count {
		return nil, fmt.Errorf("%w: canonical window count changed (%d != %d)", agentcanonical.ErrInvalidCanonicalMessages, actual, count)
	}
	if count == 0 && archive == nil {
		return []*agentschema.Message{}, nil
	}
	spans := []canonicalSpan{{0, count}}
	if archive != nil {
		if archive.From < 0 || archive.To <= archive.From || archive.To > count {
			return nil, fmt.Errorf("%w: invalid canonical archive", agentcanonical.ErrInvalidCanonicalMessages)
		}
		spans = nil
		if archive.From > 0 {
			spans = append(spans, canonicalSpan{0, archive.From})
		}
		previous := archive.From - 1
		for _, index := range archive.Retained {
			if index <= previous || index >= archive.To {
				return nil, fmt.Errorf("%w: invalid retained canonical coordinate", agentcanonical.ErrInvalidCanonicalMessages)
			}
			spans = append(spans, canonicalSpan{index, index + 1})
			previous = index
		}
		if archive.To < count {
			spans = append(spans, canonicalSpan{archive.To, count})
		}
	}
	var result []*agentschema.Message
	stop := errors.New("canonical span complete")
	for index := 0; index < len(spans); {
		first := spans[index]
		anchorIndex := sort.Search(len(s.projection.CanonicalAnchors), func(i int) bool { return s.projection.CanonicalAnchors[i].Index > first.from }) - 1
		if anchorIndex < 0 {
			return nil, fmt.Errorf("%w: canonical anchor is missing", agentcanonical.ErrInvalidCanonicalMessages)
		}
		anchor := s.projection.CanonicalAnchors[anchorIndex]
		end := index + 1
		for end < len(spans) && (anchorIndex+1 == len(s.projection.CanonicalAnchors) || spans[end].from < s.projection.CanonicalAnchors[anchorIndex+1].Index) {
			end++
		}
		selected := spans[index:end]
		ordinal := anchor.Index
		spanIndex := 0
		emit := func(message *agentschema.Message) error {
			for spanIndex < len(selected) && ordinal >= selected[spanIndex].through {
				spanIndex++
			}
			if spanIndex == len(selected) {
				return stop
			}
			if ordinal >= selected[spanIndex].from {
				result = append(result, message)
			}
			ordinal++
			if ordinal == selected[len(selected)-1].through {
				return stop
			}
			return nil
		}
		var pending *agentschema.Message
		err := s.visitCanonicalMessagesLocked(ctx, &anchor, func(message *agentschema.Message) error {
			state := agenthistory.IsContextStateMessage(message)
			if pending != nil && !state {
				if err := emit(pending); err != nil {
					return err
				}
				pending = nil
			}
			if message.Role == agentschema.User && !state {
				pending = message
				return nil
			}
			return emit(message)
		})
		if err == nil && pending != nil {
			err = emit(pending)
		}
		if err != nil && !errors.Is(err, stop) {
			return nil, err
		}
		if ordinal != selected[len(selected)-1].through {
			return nil, fmt.Errorf("%w: canonical span is incomplete", agentcanonical.ErrInvalidCanonicalMessages)
		}
		index = end
	}
	return result, nil
}
