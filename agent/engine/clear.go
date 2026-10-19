package engine

import (
	"encoding/json"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
)

func applyClearToTranscript(transcript *engineTranscript, capabilities map[string]json.RawMessage) (agenthistory.ClearState, bool, error) {
	clearState, present, err := agenthistory.ClearStateFrom(capabilities)
	if err != nil || !present || transcript == nil {
		return clearState, present, err
	}
	if clearState.Revision > transcript.ClearRevision {
		transcript.Messages = nil
		transcript.Archive = nil
		transcript.ContextState = agenthistory.ContextStateSnapshot{}
		transcript.ClearRevision = clearState.Revision
	}
	return clearState, true, nil
}
