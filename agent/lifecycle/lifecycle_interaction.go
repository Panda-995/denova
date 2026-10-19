package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

const (
	turnInteractionRecord         = "turn.interaction"
	turnInteractionResponseRecord = "turn.interaction_response"
)

type persistedInteraction struct {
	RunID         string          `json:"run_id"`
	Cycle         int             `json:"cycle"`
	InteractionID string          `json:"interaction_id"`
	ToolCallID    string          `json:"tool_call_id"`
	Request       json.RawMessage `json:"request"`
}

type persistedInteractionResponse struct {
	RunID         string          `json:"run_id"`
	InteractionID string          `json:"interaction_id"`
	Hash          string          `json:"hash"`
	Response      json.RawMessage `json:"response"`
	Resolution    json.RawMessage `json:"resolution"`
	State         string          `json:"state"`
	// Rebuilt from the preceding request record, without duplicating it on disk.
	request agentinteraction.InteractionRequest
}

func (stored persistedInteraction) pending(request agentinteraction.InteractionRequest) pendingInteraction {
	return pendingInteraction{request: request, snapshot: agentengine.InteractionSnapshot{ID: stored.InteractionID,
		OperationID: agentengine.OperationID(stored.RunID), Cycle: stored.Cycle, ToolCallID: stored.ToolCallID, Request: append(json.RawMessage(nil), stored.Request...)}}
}

func (run *Run) recordInteraction(event agentengine.InteractionRequested) (agentinteraction.InteractionRequest, error) {
	var request agentinteraction.InteractionRequest
	if err := json.Unmarshal(event.Request, &request); err != nil {
		return request, err
	}
	stored := persistedInteraction{RunID: run.id, Cycle: run.cycleValue(), InteractionID: event.ID, ToolCallID: event.ToolCallID, Request: append(json.RawMessage(nil), event.Request...)}
	run.session.mu.Lock()
	defer run.session.mu.Unlock()
	if err := run.session.appendRecordLocked(context.Background(), turnInteractionRecord, stored); err != nil {
		return request, err
	}
	run.mu.Lock()
	run.interactions[event.ID] = stored.pending(request)
	run.mu.Unlock()
	return request, nil
}

func (session *Session) replayInteraction(record agentsession.Record) error {
	if record.Kind == turnInteractionRecord {
		var stored persistedInteraction
		if err := json.Unmarshal(record.Data, &stored); err != nil {
			return err
		}
		run := session.runs[stored.RunID]
		var request agentinteraction.InteractionRequest
		if err := json.Unmarshal(stored.Request, &request); err != nil {
			return err
		}
		if run == nil || stored.InteractionID == "" || request.ID != stored.InteractionID {
			return errors.New("invalid persisted interaction")
		}
		run.interactions[stored.InteractionID] = stored.pending(request)
		return nil
	}
	var stored persistedInteractionResponse
	if err := json.Unmarshal(record.Data, &stored); err != nil {
		return err
	}
	run := session.runs[stored.RunID]
	if run == nil || stored.InteractionID == "" || stored.Hash == "" || !json.Valid(stored.Resolution) {
		return errors.New("invalid persisted interaction response")
	}
	if pending, found := run.interactions[stored.InteractionID]; found {
		stored.request = pending.request
	} else {
		stored.request = run.responses[stored.InteractionID].request
	}
	switch stored.State {
	case "answered":
		delete(run.interactions, stored.InteractionID)
	case "pending":
	default:
		return errors.New("invalid persisted interaction response state")
	}
	run.responses[stored.InteractionID] = stored
	return nil
}

// Respond acknowledges only a durable answer. Repeating the same answer is
// idempotent; a different answer cannot rewrite an already consumed decision.
func (run *Run) Respond(ctx context.Context, interactionID string, response agentinteraction.InteractionResponse) error {
	ctx, err := commandContext(ctx)
	if err != nil {
		return err
	}
	if run == nil || run.session == nil {
		return agentschema.ErrRunSettled
	}
	if err := run.session.usable(); err != nil {
		return err
	}
	interactionID = strings.TrimSpace(interactionID)
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	hash, err := agentschema.HashCanonical(response)
	if err != nil {
		return err
	}
	run.session.mu.RLock()
	previous, answered := run.responses[interactionID]
	settled := run.isSettled()
	owner := run.session.recovery.Interactions[interactionID].RunID
	run.session.mu.RUnlock()
	if settled {
		if owner != run.id {
			return agentschema.ErrInteractionStale
		}
		_, _, err := run.session.historicalResponse(ctx, interactionID, response)
		return err
	}
	if answered {
		if previous.Hash == hash {
			return nil
		}
		if previous.State == "answered" {
			return ErrIdempotencyConflict
		}
	}
	if err := run.usable(); err != nil {
		return err
	}
	run.mu.RLock()
	pending, found := run.interactions[interactionID]
	run.mu.RUnlock()
	if !found {
		return agentschema.ErrInteractionStale
	}
	var resolution json.RawMessage
	if pending.request.Verification != nil {
		value, err := agentinteraction.ResolvePersistedStandardAsk(pending.snapshot.Request, response)
		if err != nil {
			return err
		}
		resolution, err = json.Marshal(value)
		if err != nil {
			return err
		}
	} else {
		resolver, ok := run.session.engine.(agentengine.InteractionResolver)
		if !ok {
			return agentschema.ErrCapabilityUnsupported
		}
		snapshot, err := run.snapshotForCurrentCycle()
		if err != nil {
			return err
		}
		resolution, err = resolver.ResolveInteraction(ctx, agentengine.InteractionResolveRequest{Snapshot: snapshot, Interaction: pending.snapshot, Response: encoded})
		if err != nil {
			return err
		}
	}
	var publicResolution agentinteraction.InteractionResolution
	if err := json.Unmarshal(resolution, &publicResolution); err != nil {
		return err
	}
	state := "answered"
	verificationChoice := ""
	if pending.request.Verification != nil {
		state = "pending"
		if !publicResolution.Cancelled && len(publicResolution.Answers) == 1 && len(publicResolution.Answers[0].Values) == 1 {
			verificationChoice = publicResolution.Answers[0].Values[0]
			if verificationChoice == "executed" || verificationChoice == "not_executed" {
				state = "answered"
			}
		}
	}
	stored := persistedInteractionResponse{RunID: run.id, InteractionID: interactionID, Hash: hash, Response: encoded, Resolution: resolution, State: state, request: pending.request}
	responseRecord, err := sessionRecord(turnInteractionResponseRecord, stored)
	if err != nil {
		return err
	}
	records := []agentsession.Record{responseRecord}
	run.session.mu.Lock()
	if run.session.closing || run.session.closed || run.session.runs[run.id] != run {
		run.session.mu.Unlock()
		return agentschema.ErrSessionClosed
	}
	if previous, found := run.responses[interactionID]; found && previous.State == "answered" {
		run.session.mu.Unlock()
		if previous.Hash == hash {
			return nil
		}
		return ErrIdempotencyConflict
	}
	var confirmed *agentengine.PersistedTool
	if fact, ok := run.tools[pending.snapshot.ToolCallID]; ok && state == "answered" {
		if pending.request.Verification != nil {
			result := agenttool.SyntheticToolResult(agentschema.ToolResultSkipped, agentschema.ToolSyntheticSteeringBeforeStart, "The user verified that this operation did not take effect. No effect was applied by this call.")
			if verificationChoice == "executed" {
				result = agenttool.SyntheticToolResult(agentschema.ToolResultBlocked, agentschema.ToolSyntheticEffectUnknown, "The user verified that this operation took effect. Original result details are unavailable. Do not repeat the operation; inspect its current state if details are needed.")
			}
			fact.Result, fact.Source = &result, "user_verified_"+verificationChoice
			confirmed = &fact
		} else if fact.Descriptor != nil && fact.Descriptor.Capability == "ask" {
			result := agentschema.ToolResult{Status: agentschema.ToolResultSuccess, ModelContent: string(resolution), DisplayContent: string(resolution), Details: append(json.RawMessage(nil), resolution...)}
			fact.Result, fact.Source = &result, "user_response"
			confirmed = &fact
		}
	}
	if confirmed != nil {
		record, err := sessionRecord(turnToolRecord, confirmed)
		if err != nil {
			run.session.mu.Unlock()
			return err
		}
		records = append(records, record)
	}
	if err := run.session.appendRecordsLocked(ctx, records...); err != nil {
		run.session.mu.Unlock()
		return err
	}
	run.responses[interactionID] = stored
	if confirmed != nil {
		run.tools[confirmed.CallID] = *confirmed
	}
	run.mu.Lock()
	if state == "answered" {
		delete(run.interactions, interactionID)
		if confirmed != nil {
			delete(run.openTools, confirmed.CallID)
		}
	}
	suspended := run.result.Status == agentschema.ResultSuspended
	run.mu.Unlock()
	run.session.mu.Unlock()
	if state == "pending" {
		return nil
	}
	run.publish(agentevent.InteractionResolved{ID: interactionID, Resolution: publicResolution})
	if suspended {
		return nil
	}
	select {
	case run.controls <- agentengine.Control{Kind: agentengine.ControlInteractionResolved, InteractionID: interactionID, Response: resolution}:
		return nil
	case <-run.done:
		// The durable answer remains accepted even when completion wins delivery.
		return nil
	case <-ctx.Done():
		return nil
	}
}

// Respond answers a known interaction without starting execution. It also
// returns the durable original resolution for repeated answers after restart
// or settlement; an interaction outside this Session remains stale.
func (session *Session) Respond(ctx context.Context, interactionID string, response agentinteraction.InteractionResponse) (agentinteraction.InteractionRequest, agentinteraction.InteractionResolution, error) {
	if err := session.usable(); err != nil {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, err
	}
	interactionID = strings.TrimSpace(interactionID)
	var target *Run
	session.mu.RLock()
	for _, run := range session.runs {
		run.mu.RLock()
		_, pending := run.interactions[interactionID]
		run.mu.RUnlock()
		_, answered := run.responses[interactionID]
		if pending || answered {
			target = run
			break
		}
	}
	session.mu.RUnlock()
	if target == nil {
		return session.historicalResponse(ctx, interactionID, response)
	}
	if err := target.Respond(ctx, interactionID, response); err != nil {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, err
	}
	return session.historicalResponse(ctx, interactionID, response)
}

func (session *Session) historicalResponse(ctx context.Context, id string, response agentinteraction.InteractionResponse) (agentinteraction.InteractionRequest, agentinteraction.InteractionResolution, error) {
	session.mu.RLock()
	entry, found := session.recovery.Interactions[strings.TrimSpace(id)]
	session.mu.RUnlock()
	if !found || entry.Response == 0 {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, agentschema.ErrInteractionStale
	}
	record, err := session.readRecord(ctx, entry.Response)
	if err != nil {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, err
	}
	var stored persistedInteractionResponse
	if err := json.Unmarshal(record.Data, &stored); err != nil {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, err
	}
	hash, err := agentschema.HashCanonical(response)
	if err != nil {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, err
	}
	if stored.Hash != hash {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, ErrIdempotencyConflict
	}
	record, err = session.readRecord(ctx, entry.Request)
	if err != nil {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, err
	}
	var original persistedInteraction
	if err := json.Unmarshal(record.Data, &original); err != nil {
		return agentinteraction.InteractionRequest{}, agentinteraction.InteractionResolution{}, err
	}
	var request agentinteraction.InteractionRequest
	var resolution agentinteraction.InteractionResolution
	if err := json.Unmarshal(original.Request, &request); err != nil {
		return request, resolution, err
	}
	err = json.Unmarshal(stored.Resolution, &resolution)
	return request, resolution, err
}
