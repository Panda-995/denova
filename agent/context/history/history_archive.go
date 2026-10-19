// Package history computes active context, message coordinates, compaction, and
// elision from detached values. It owns no running handles, goroutines, or stores.
package history

import (
	"errors"
	"sort"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// historyArchive omits one compacted interval from the active transcript while
// preserving canonical message coordinates. Retained contains only original
// user instructions and current context-state records still needed for recovery.
// The host journal remains the source of omitted bodies.
type HistoryArchive struct {
	From     int   `json:"from"`
	To       int   `json:"to"`
	Retained []int `json:"retained,omitempty"`
}

func (a *HistoryArchive) Local(index int) int {
	if a == nil || index < a.From {
		return index
	}
	if index < a.To {
		return a.From + sort.SearchInts(a.Retained, index)
	}
	return index - (a.To - a.From - len(a.Retained))
}

func (a *HistoryArchive) Raw(index int) int {
	if a == nil || index < a.From {
		return index
	}
	if index < a.From+len(a.Retained) {
		return a.Retained[index-a.From]
	}
	return index + a.To - a.From - len(a.Retained)
}

func (a *HistoryArchive) Count(messages []*agentschema.Message) int { return a.Raw(len(messages)) }

func (a *HistoryArchive) Contains(index int) bool {
	if a == nil || index < a.From || index >= a.To {
		return true
	}
	i := sort.SearchInts(a.Retained, index)
	return i < len(a.Retained) && a.Retained[i] == index
}

func (a *HistoryArchive) Validate(messages []*agentschema.Message) error {
	if a == nil {
		return nil
	}
	if a.From < 0 || a.To <= a.From || a.From+len(a.Retained) > len(messages) {
		return errors.New("invalid archived history interval")
	}
	previous := a.From - 1
	for _, index := range a.Retained {
		if index <= previous || index >= a.To {
			return errors.New("invalid retained history coordinate")
		}
		previous = index
	}
	return nil
}

func (a *HistoryArchive) SelectMessages(full []*agentschema.Message) ([]*agentschema.Message, error) {
	if a == nil {
		return full, nil
	}
	if a.To > len(full) {
		return nil, errors.New("archived history is outside canonical messages")
	}
	result := make([]*agentschema.Message, 0, len(full)-(a.To-a.From)+len(a.Retained))
	result = append(result, full[:a.From]...)
	for _, index := range a.Retained {
		result = append(result, full[index])
	}
	return append(result, full[a.To:]...), nil
}

func ArchiveHistory(messages []*agentschema.Message, previous *HistoryArchive, compact CompactionRecord, state ContextStateSnapshot) ([]*agentschema.Message, *HistoryArchive) {
	if compact.Removed || compact.ReplacementTo <= compact.ReplacementFrom || compact.ReplacementTo > previous.Count(messages) {
		return messages, previous
	}
	keep := make(map[int]bool, len(state.Sections))
	for _, section := range state.Sections {
		keep[section.MessageIndex] = true
	}
	next := &HistoryArchive{From: compact.ReplacementFrom, To: compact.ReplacementTo}
	// Do not reserve the size of the archive: its pointer array can itself be
	// large and would stay resident even after the bodies were released.
	var result []*agentschema.Message
	for local, message := range messages {
		index := previous.Raw(local)
		if index >= next.From && index < next.To {
			if !keep[index] && !(compact.RetainedUserFrom != nil && index >= *compact.RetainedUserFrom && message.Role == agentschema.User && !IsContextStateMessage(message)) {
				continue
			}
			next.Retained = append(next.Retained, index)
		}
		result = append(result, message)
	}
	return result, next
}
