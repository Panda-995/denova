package lifecycle

import (
	"encoding/json"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

// These test views pin the serialized journal contract across the package
// refactor. Lifecycle production code never decodes engine checkpoint fields.
type journalTranscript struct {
	HistoryHead             agentcanonical.CanonicalHistoryHead `json:"history_head,omitempty"`
	Version                 uint16                              `json:"version"`
	DefinitionKey           string                              `json:"definition_key"`
	BehaviorKey             string                              `json:"behavior_key"`
	PrefixFingerprint       string                              `json:"prefix_fingerprint"`
	MaterializedFingerprint string                              `json:"materialized_fingerprint,omitempty"`
	DefinitionOperationID   string                              `json:"definition_operation_id,omitempty"`
	DefinitionCommandID     string                              `json:"definition_command_id,omitempty"`
	DefinitionCycle         int                                 `json:"definition_cycle,omitempty"`
	PreparationStage        string                              `json:"preparation_stage,omitempty"`
	PreparedContext         *journalPreparedContext             `json:"prepared_context,omitempty"`
	Archive                 *agenthistory.HistoryArchive        `json:"archive,omitempty"`
	Messages                []*agentschema.Message              `json:"messages,omitempty"`
	ContextState            agenthistory.ContextStateSnapshot   `json:"context_state,omitempty"`
	// ContextSequence is the next idempotency slot for this active cycle. It is
	// checkpointed with the transcript so a resumed run cannot shift sequence
	// numbers when an earlier context-state batch is already present.
	ContextSequence     int `json:"context_sequence,omitempty"`
	LastResponseOrdinal int `json:"last_response_ordinal,omitempty"`
	// ActiveModelUser is the model-only rendering of the accepted raw user
	// message while a tool batch or interaction is still active.
	// Messages remains the canonical raw transcript. Once the cycle settles,
	// this transient projection is discarded so canonical maintenance always
	// addresses stable raw messages.
	ActiveModelUser *agentschema.Message  `json:"active_model_user,omitempty"`
	ActiveUserIndex int                   `json:"active_user_index,omitempty"`
	HostData        *agentschema.HostData `json:"host_data,omitempty"`
	ClearRevision   uint64                `json:"clear_revision,omitempty"`
}
type journalPreparedContext struct {
	Version            uint16                        `json:"version"`
	Fragments          []agentschema.ContextFragment `json:"fragments,omitempty"`
	GoalFragments      []agentschema.ContextFragment `json:"goal_fragments,omitempty"`
	GoalReservedTokens int                           `json:"goal_reserved_tokens,omitempty"`
}

func decodeJournalTranscript(raw json.RawMessage) (journalTranscript, error) {
	state := journalTranscript{Version: 1}
	if len(raw) == 0 {
		return state, nil
	}
	err := json.Unmarshal(raw, &state)
	return state, err
}
