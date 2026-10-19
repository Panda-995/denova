package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

// RecoveryLog is an optional storage optimization. Recovery returns an owned,
// rebuildable index of the canonical records; ReadRecord reads the original
// record at its logical revision. Neither operation may start Agent work.
// Ordinary Logs remain supported through streaming replay.
type RecoveryLog interface {
	Recovery(context.Context) (*RecoveryIndex, error)
	ReadRecord(context.Context, agentsession.Revision) (agentsession.Record, error)
}

// RecoveryIndex separates unfinished execution facts from historical lookup.
// It is derived entirely from the Session journal. Historical entries contain
// receipts and record revisions, never input, output, or tool-result bodies.
// Hosts may checkpoint this index but must rebuild it after format changes.
type RecoveryIndex struct {
	Recent       []RecoveryRun                  `json:"recent,omitempty"`
	Cursor       agentevent.Cursor              `json:"cursor,omitempty"`
	Children     childTasks                     `json:"children"`
	Revision     agentsession.Revision          `json:"revision"`
	Inputs       map[string]RecoveryInput       `json:"inputs,omitempty"`
	Runs         map[string]RecoveryRun         `json:"runs,omitempty"`
	Interactions map[string]RecoveryInteraction `json:"interactions,omitempty"`
	Records      map[string]RecoveryRecord      `json:"records,omitempty"`
}

type RecoveryInput struct {
	Receipt  agentevent.CommandReceipt `json:"receipt"`
	Kind     sessionInputKind          `json:"kind"`
	Hash     string                    `json:"hash"`
	Revision agentsession.Revision     `json:"revision"`
	Status   inputStatus               `json:"status"`
	RunID    string                    `json:"run_id,omitempty"`
}

type RecoveryRun struct {
	Receipt    agentevent.CommandReceipt `json:"receipt"`
	Started    bool                      `json:"started"`
	Status     agentschema.ResultStatus  `json:"status,omitempty"`
	FinishedAt time.Time                 `json:"finished_at,omitempty"`
	Settlement agentsession.Revision     `json:"settlement,omitempty"`
}

type RecoveryInteraction struct {
	RunID    string                `json:"run_id"`
	Request  agentsession.Revision `json:"request"`
	Response agentsession.Revision `json:"response,omitempty"`
}

type RecoveryRecord struct {
	Record    agentsession.Record `json:"record"`
	RunID     string              `json:"run_id,omitempty"`
	CommandID string              `json:"command_id,omitempty"`
}

// CapabilityRecord returns an owned copy of the latest set or delete record.
// A delete record remains present so hosts can distinguish a tombstone from
// a capability that has never been stored, including during migration.
func (index *RecoveryIndex) CapabilityRecord(capability string) (agentsession.Record, bool) {
	entry, found := index.Records["capability:"+capability]
	record := entry.Record
	record.Data = append(json.RawMessage(nil), record.Data...)
	return record, found
}

// Apply consumes canonical records in revision order. The caller serializes
// updates with journal commits; this reducer has no independent persistence.
func (index *RecoveryIndex) Apply(record agentsession.Record) error {
	if record.Version != sessionRecordVersion {
		return fmt.Errorf("unsupported Agent recovery record version %d", record.Version)
	}
	if err := agentsession.ValidateRecord(record); err != nil {
		return err
	}
	if index.Inputs == nil {
		index.Inputs = make(map[string]RecoveryInput)
	}
	if index.Runs == nil {
		index.Runs = make(map[string]RecoveryRun)
	}
	if index.Interactions == nil {
		index.Interactions = make(map[string]RecoveryInteraction)
	}
	if index.Records == nil {
		index.Records = make(map[string]RecoveryRecord)
	}
	key, runID, commandID := record.Kind, "", ""
	switch record.Kind {
	case sessionTranscriptRecord, sessionMessageCheckpointRecord:
	case sessionCapabilitySetRecord, sessionCapabilityDeleteRecord:
		var value persistedCapability
		if err := json.Unmarshal(record.Data, &value); err != nil {
			return err
		}
		key = "capability:" + value.Capability
	case sessionInputRecord:
		var value persistedInput
		if err := json.Unmarshal(record.Data, &value); err != nil {
			return err
		}
		if value.Receipt.CommandID == "" || value.Hash == "" {
			return fmt.Errorf("invalid Agent input receipt")
		}
		commandID, runID = value.Receipt.CommandID, value.Receipt.RunID
		index.Inputs[commandID] = RecoveryInput{Receipt: value.Receipt, Kind: value.Kind, Hash: value.Hash, Revision: record.Revision, Status: inputPending, RunID: runID}
		index.Cursor = max(index.Cursor, value.Receipt.Cursor)
		if value.Kind == inputRun || value.Kind == inputFollowUp {
			index.Runs[runID] = RecoveryRun{Receipt: value.Receipt}
		}
		key += ":" + commandID
	case sessionInputUpdateRecord:
		var value persistedInputUpdate
		if err := json.Unmarshal(record.Data, &value); err != nil {
			return err
		}
		input, found := index.Inputs[value.CommandID]
		if !found {
			return fmt.Errorf("input update has no accepted command %q", value.CommandID)
		}
		commandID, runID = value.CommandID, value.RunID
		input.Status, input.RunID = value.Status, value.RunID
		index.Inputs[commandID] = input
		if original, ok := index.Records[sessionInputRecord+":"+commandID]; ok {
			original.RunID = runID
			index.Records[sessionInputRecord+":"+commandID] = original
		}
		// Host ownership is assigned before consumption. Preserve that binding even
		// when the last status update contains no HostData.
		if value.HostData != nil {
			index.keep("binding:"+commandID, record, runID, commandID)
		}
		if value.Control != nil {
			index.Cursor = max(index.Cursor, value.Control.Receipt.Cursor)
			// Keep the original update so replay retains its existing control codec.
			index.keep("input-control:"+value.Control.Receipt.CommandID, record, "", "")
		}
		key += ":" + commandID
	case turnStartedRecord, turnFinishedRecord, turnInterruptedRecord:
		var value persistedTurn
		if err := json.Unmarshal(record.Data, &value); err != nil {
			return err
		}
		runID = value.RunID
		run := index.Runs[runID]
		if run.Receipt.CommandID == "" {
			run.Receipt = agentevent.CommandReceipt{CommandID: value.CommandID, RunID: runID}
		}
		if record.Kind == turnStartedRecord {
			run.Started = true
		} else {
			run.Status, run.FinishedAt, run.Settlement = value.Status, value.At, record.Revision
			// RecentRuns is a window of distinct Runs, ordered by their latest
			// settlement. Repeated suspensions must not evict unrelated results.
			recent := index.Recent[:0]
			for _, previous := range index.Recent {
				if previous.Receipt.RunID != runID {
					recent = append(recent, previous)
				}
			}
			index.Recent = append(recent, run)
			if len(index.Recent) > 32 {
				index.Recent = append([]RecoveryRun(nil), index.Recent[len(index.Recent)-32:]...)
			}
		}
		index.Runs[runID] = run
		if record.Kind != turnStartedRecord && value.Status != agentschema.ResultSuspended {
			index.retire(runID)
			index.Revision = record.Revision
			return nil
		}
		key += ":" + runID
	case turnCheckpointRecord:
		var value persistedCycle
		if err := json.Unmarshal(record.Data, &value); err != nil {
			return err
		}
		runID = value.RunID
		key += ":" + runID
	case turnToolRecord:
		var value struct {
			RunID  string `json:"run_id"`
			CallID string `json:"call_id"`
		}
		if err := json.Unmarshal(record.Data, &value); err != nil {
			return err
		}
		runID = value.RunID
		key += ":" + runID + ":" + value.CallID
	case turnInteractionRecord, turnInteractionResponseRecord:
		var value struct {
			RunID string `json:"run_id"`
			ID    string `json:"interaction_id"`
		}
		if err := json.Unmarshal(record.Data, &value); err != nil {
			return err
		}
		runID = value.RunID
		interaction := index.Interactions[value.ID]
		interaction.RunID = runID
		if record.Kind == turnInteractionRecord {
			interaction.Request = record.Revision
		} else {
			interaction.Response = record.Revision
		}
		index.Interactions[value.ID] = interaction
		key += ":" + value.ID
	case sessionControlRecord:
		var value persistedSessionControl
		if err := json.Unmarshal(record.Data, &value); err != nil {
			return err
		}
		index.Cursor = max(index.Cursor, value.Receipt.Cursor)
		key += ":" + value.Receipt.CommandID
	case sessionChildTasksRecord:
		state, err := decodeChildTasks(record.Data)
		if err != nil {
			return err
		}
		index.Children = state
	case sessionTaskCompletionDeliveryRecord:
		key += fmt.Sprint(record.Revision)
	default:
		return fmt.Errorf("unsupported Agent recovery record %q", record.Kind)
	}
	index.keep(key, record, runID, commandID)
	if commandID != "" && index.Inputs[commandID].Status == inputCancelled {
		delete(index.Records, sessionInputRecord+":"+commandID)
		delete(index.Records, "binding:"+commandID)
		delete(index.Records, sessionInputUpdateRecord+":"+commandID)
	}
	index.Revision = record.Revision
	return nil
}

func (index *RecoveryIndex) keep(key string, record agentsession.Record, runID, commandID string) {
	record.Data = append(json.RawMessage(nil), record.Data...)
	index.Records[key] = RecoveryRecord{Record: record, RunID: runID, CommandID: commandID}
}

func (index *RecoveryIndex) retire(runID string) {
	for key, entry := range index.Records {
		if entry.RunID != runID {
			continue
		}
		if entry.CommandID != "" {
			input := index.Inputs[entry.CommandID]
			if input.Status == inputPending {
				if input.Kind == inputQueue || input.Kind == inputSteer {
					continue
				}
				input.Status = inputCancelled
				index.Inputs[entry.CommandID] = input
			}
		}
		delete(index.Records, key)
	}
}

// ReplayRecords contains only current context/capabilities, command controls,
// and unfinished execution facts. Its records retain their original revisions.
func (index *RecoveryIndex) ReplayRecords() []agentsession.Record {
	unique := make(map[agentsession.Revision]agentsession.Record, len(index.Records))
	for _, entry := range index.Records {
		unique[entry.Record.Revision] = entry.Record
	}
	records := make([]agentsession.Record, 0, len(unique))
	for _, record := range unique {
		record.Data = append(json.RawMessage(nil), record.Data...)
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Revision < records[j].Revision })
	return records
}

// Current returns the bounded observation projection. Exact historical receipts
// stay in the full index and canonical journal; observation never needs them.
func (index *RecoveryIndex) Current() *RecoveryIndex {
	result := &RecoveryIndex{Revision: index.Revision, Cursor: index.Cursor, Children: index.Children.clone(), Inputs: make(map[string]RecoveryInput), Runs: make(map[string]RecoveryRun), Interactions: make(map[string]RecoveryInteraction), Records: make(map[string]RecoveryRecord)}
	var lastControl, lastTree RecoveryRecord
	for key, entry := range index.Records {
		switch entry.Record.Kind {
		case sessionControlRecord:
			var control persistedSessionControl
			_ = json.Unmarshal(entry.Record.Data, &control)
			if control.Kind == "suspend_tree" || control.Kind == "abort_tree" || control.Kind == "resume_tree" || control.Kind == "release_tree" {
				if entry.Record.Revision > lastTree.Record.Revision {
					lastTree = entry
				}
			} else if entry.Record.Revision > lastControl.Record.Revision {
				lastControl = entry
			}
			continue
		case sessionTranscriptRecord, sessionMessageCheckpointRecord, sessionTaskCompletionDeliveryRecord:
			// Model context and delivery history belong to execution. Status
			// reads need neither historical message bodies nor delivery receipts.
			continue
		case sessionInputUpdateRecord:
			if entry.CommandID == "" {
				// Standalone control receipts are exact execution history. The
				// final pending-input update remains above its accepted input.
				continue
			}
		}
		result.Records[key] = entry
		if entry.CommandID != "" {
			result.Inputs[entry.CommandID] = index.Inputs[entry.CommandID]
		}
		if entry.RunID != "" {
			result.Runs[entry.RunID] = index.Runs[entry.RunID]
		}
	}
	for _, entry := range []RecoveryRecord{lastControl, lastTree} {
		if entry.Record.Revision != 0 {
			result.Records[fmt.Sprint(entry.Record.Revision)] = entry
		}
	}
	for _, recent := range index.Recent {
		result.Recent = append(result.Recent, recent)
		result.Runs[recent.Receipt.RunID] = recent
	}
	for id, interaction := range index.Interactions {
		if _, active := result.Runs[interaction.RunID]; active && interaction.Response == 0 {
			result.Interactions[id] = interaction
		}
	}
	return result
}
