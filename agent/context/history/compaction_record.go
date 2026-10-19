package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

type CompactionRecord struct {
	// Version identifies the journal schema. Version 2 supports complete
	// assistant steps inside an unfinished Run.
	Version uint16 `json:"version,omitempty"`
	ID      string `json:"id"`
	// Revision increases when a checkpoint is committed or removed. It fences
	// stale requests and keeps checkpoints from before Clear inactive.
	Revision uint64 `json:"revision"`
	// SourceRevision labels the source snapshot for diagnostics; it is not
	// the checkpoint revision or a concurrency check.
	SourceRevision string `json:"source_revision"`
	SourceHash     string `json:"source_hash"`
	Summary        string `json:"summary"`
	// TokenEstimate is the exact post-checkpoint provider-input estimate after
	// stable Context fragments, tool schemas, and reserves are re-applied.
	TokenEstimate int `json:"token_estimate,omitempty"`
	// SummaryTokenEstimate measures only the generated checkpoint body.
	SummaryTokenEstimate int               `json:"summary_token_estimate,omitempty"`
	Metrics              CompactionMetrics `json:"metrics,omitempty"`
	ReplacementFrom      int               `json:"replacement_from"`
	ReplacementTo        int               `json:"replacement_to"`
	// RetainedUserFrom preserves verbatim user instructions from the active Run
	// inside the replaced prefix. Coordinates always refer to the raw journal;
	// completed tool groups may be summarized without erasing the task itself.
	RetainedUserFrom *int      `json:"retained_user_from,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	Removed          bool      `json:"removed,omitempty"`
	// ContextData is optional, product-neutral metadata used by a custom
	// ContextSource to apply this checkpoint to host-owned context. Agent keeps
	// it durable and opaque; it is never injected into the model automatically.
	ContextData *agentschema.HostData `json:"context_data,omitempty"`
}

func CompactionStatePointer(state CompactionRecord, present bool) *CompactionState {
	if !present || state.Removed {
		return nil
	}
	copy := state
	copy.ContextData = agentschema.CloneHostData(state.ContextData)
	if state.RetainedUserFrom != nil {
		index := *state.RetainedUserFrom
		copy.RetainedUserFrom = &index
	}
	return &CompactionState{
		ID: state.ID, Revision: state.Revision, Summary: state.Summary, CreatedAt: state.CreatedAt,
		SourceMessageCount: state.ReplacementTo - state.ReplacementFrom,
		TokensBefore:       state.Metrics.ProjectedTokensBefore, TokensAfter: state.TokenEstimate,
		ContextData: agentschema.CloneHostData(state.ContextData), projection: &copy,
	}
}

func CloneCompactionState(state *CompactionState) *CompactionState {
	if state == nil {
		return nil
	}
	copy := *state
	copy.ContextData = agentschema.CloneHostData(state.ContextData)
	// The private projection is immutable and never exposed to callers.
	return &copy
}

func CompactionStateFrom(states map[string]json.RawMessage) (CompactionRecord, bool, error) {
	raw, present := states[CompactionCapability]
	if !present {
		return CompactionRecord{}, false, nil
	}
	state, err := DecodeCompactionState(raw)
	return state, true, err
}

func DecodeCompactionState(raw json.RawMessage) (CompactionRecord, error) {
	var state CompactionRecord
	if err := json.Unmarshal(raw, &state); err != nil {
		return CompactionRecord{}, fmt.Errorf("decode Compaction state: %w", err)
	}
	if strings.TrimSpace(state.ID) == "" || state.Revision == 0 || state.ReplacementFrom < 0 ||
		state.ReplacementTo <= state.ReplacementFrom || strings.TrimSpace(state.Summary) == "" {
		return CompactionRecord{}, errors.New("durable Compaction state is invalid")
	}
	if state.Version != 0 && state.Version != 2 {
		return CompactionRecord{}, fmt.Errorf("unsupported Compaction state version %d", state.Version)
	}
	if state.RetainedUserFrom != nil && (*state.RetainedUserFrom < state.ReplacementFrom || *state.RetainedUserFrom >= state.ReplacementTo) {
		return CompactionRecord{}, errors.New("durable Compaction retained user boundary is invalid")
	}
	if err := ValidateCompactionContextData(state.ContextData); err != nil {
		return CompactionRecord{}, err
	}
	return state, nil
}

func EffectiveCompactionMessages(messages []*agentschema.Message, state CompactionRecord, present bool, summaryLimit int) ([]*agentschema.Message, error) {
	return (*HistoryArchive)(nil).EffectiveCompactionMessages(messages, state, present, summaryLimit)
}

func (archive *HistoryArchive) EffectiveCompactionMessages(messages []*agentschema.Message, state CompactionRecord, present bool, summaryLimit int) ([]*agentschema.Message, error) {
	if !present || state.Removed || state.ReplacementFrom < 0 || state.ReplacementTo > archive.Count(messages) || state.ReplacementTo <= state.ReplacementFrom {
		return agentschema.CloneMessages(messages), nil
	}
	if summaryLimit <= 0 {
		return nil, errors.New("Compaction summary limit must be positive")
	}
	if len(state.Summary) > summaryLimit {
		return nil, fmt.Errorf("%w: durable Compaction checkpoint is %d bytes and exceeds the target Agent summary limit %d", agentschema.ErrContextLimit, len(state.Summary), summaryLimit)
	}
	result := make([]*agentschema.Message, 0, len(messages)+1)
	result = append(result, agentschema.CloneMessages(messages[:archive.Local(state.ReplacementFrom)])...)
	result = append(result, compactionCheckpointMessage(state, summaryLimit))
	for index := archive.Local(state.ReplacementFrom); index < archive.Local(state.ReplacementTo); index++ {
		if state.RetainedUserFrom != nil && archive.Raw(index) >= *state.RetainedUserFrom && messages[index].Role == agentschema.User && !IsContextStateMessage(messages[index]) {
			result = append(result, messages[index].Clone())
		}
	}
	result = append(result, agentschema.CloneMessages(messages[archive.Local(state.ReplacementTo):])...)
	return result, nil
}

func compactionRetainsUser(messages []*agentschema.Message, state CompactionRecord, index int) bool {
	return state.RetainedUserFrom != nil && index >= *state.RetainedUserFrom &&
		messages[index] != nil && messages[index].Role == agentschema.User && !IsContextStateMessage(messages[index])
}

// compactionMessageIndex maps a surviving raw journal message to the effective
// transcript, including verbatim instructions preserved inside a checkpoint.
func compactionMessageIndex(messages []*agentschema.Message, state CompactionRecord, present bool, index int) int {
	return (*HistoryArchive)(nil).CompactionMessageIndex(messages, state, present, index)
}

func (archive *HistoryArchive) CompactionMessageIndex(messages []*agentschema.Message, state CompactionRecord, present bool, index int) int {
	if !archive.Contains(index) {
		return -1
	}
	if !present || state.Removed || index < state.ReplacementFrom {
		return archive.Local(index)
	}
	projected := archive.Local(state.ReplacementFrom) + 1
	for local := archive.Local(state.ReplacementFrom); local < archive.Local(min(index, state.ReplacementTo)); local++ {
		if state.RetainedUserFrom != nil && archive.Raw(local) >= *state.RetainedUserFrom && messages[local].Role == agentschema.User && !IsContextStateMessage(messages[local]) {
			projected++
		}
	}
	if index < state.ReplacementTo {
		message := messages[archive.Local(index)]
		if state.RetainedUserFrom == nil || index < *state.RetainedUserFrom || message.Role != agentschema.User || IsContextStateMessage(message) {
			return -1
		}
		return projected
	}
	return projected + index - state.ReplacementTo
}

func (archive *HistoryArchive) CompactionIncrementalSource(
	messages []*agentschema.Message,
	plan CompactionExecutionPlan,
	current CompactionRecord,
	present bool,
	summaryLimit int,
) []*agentschema.Message {
	if present && !current.Removed && current.ReplacementFrom == plan.SourceFrom &&
		current.ReplacementTo >= plan.SourceFrom && current.ReplacementTo <= plan.SourceTo {
		result := []*agentschema.Message{compactionCheckpointMessage(current, summaryLimit)}
		return append(result, agentschema.CloneMessages(messages[archive.Local(current.ReplacementTo):archive.Local(plan.SourceTo)])...)
	}
	return agentschema.CloneMessages(messages[archive.Local(plan.SourceFrom):archive.Local(plan.SourceTo)])
}

func compactionCheckpointMessage(state CompactionRecord, summaryLimit int) *agentschema.Message {
	// Historical context stays at its replacement position without becoming
	// system instructions or changing the preceding stable cache prefix.
	return agentschema.UserMessage(RenderContextFragment(agentschema.ContextFragment{
		Source: "agent.compaction", Purpose: "replace compacted conversation history",
		Resource: state.ID, Revision: fmt.Sprintf("%d", state.Revision),
		Stability: agentschema.ContextCheckpoint, Placement: agentschema.ContextCompactionCheckpoint, Content: state.Summary, HardLimit: summaryLimit,
	}))
}
