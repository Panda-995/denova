package engine

import (
	"errors"
	"strings"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func compactionModelRequest(
	prepared preparedDefinition,
	messages []*agentschema.Message,
	currentInput string,
	current agenthistory.CompactionRecord,
	present bool,
) ([]*agentschema.Message, error) {
	result := make([]*agentschema.Message, 0, len(messages)+len(prepared.fragments)+2)
	if prepared.definition.Instructions != "" {
		result = append(result, agentschema.SystemMessage(prepared.definition.Instructions))
	}
	effective, err := agenthistory.EffectiveHistoryMessages(messages, prepared.elision, current, present, prepared.definition.Compaction.SummaryLimitBytes())
	if err != nil {
		// Raw history is retained specifically so an oversized checkpoint can be
		// regenerated after the target Agent's configured limits are lowered.
		if !errors.Is(err, agentschema.ErrContextLimit) {
			return nil, err
		}
		effective, err = agenthistory.ElisionForHistory(prepared.elision, current, present).Project(messages)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(currentInput) == "" {
		result = append(result, leadingContextMessages(prepared.fragments)...)
		result = append(result, effective...)
		return result, nil
	}
	cycle, _, err := assembleCycleMessages(effective, currentInput, nil, prepared.fragments, prepared.definition.AttachmentRoot)
	if err != nil {
		return nil, err
	}
	result = append(result, cycle...)
	return result, nil
}
