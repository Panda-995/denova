package lifecycle

import (
	"context"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

// LoadCanonicalMessages refreshes an idle host-backed Session from the
// canonical conversation lane. Host-canonical logs keep only a compact
// checkpoint; standalone logs remain self-contained and persist the imported
// transcript normally.
func (session *Session) LoadCanonicalMessages(ctx context.Context, messages []*agentschema.Message) error {
	return session.loadCanonicalMessages(ctx, messages, agentcanonical.CanonicalHistoryHead{})
}

func (session *Session) loadCanonicalMessages(ctx context.Context, messages []*agentschema.Message, head agentcanonical.CanonicalHistoryHead) error {
	if err := session.usable(); err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.active != nil && !session.active.isSuspended() || session.maintenance {
		return agentschema.ErrSessionBusy
	}
	outputCommitted := false
	if session.active != nil {
		session.active.mu.RLock()
		outputCommitted = session.active.snapshot.OutputCommit != nil
		session.active.mu.RUnlock()
	}
	change, err := agentengine.PrepareCanonicalImport(ctx, agentengine.CanonicalImportRequest{
		Session: session.key, Messages: messages, Head: head, State: session.engineState,
		Checkpoint: session.messageCheckpoint, Capabilities: session.capabilities,
		Active: session.active != nil, OutputCommitted: outputCommitted, Canonical: session.canonicalMessages,
	})
	if err != nil {
		return err
	}
	if change.Persist {
		var records []agentsession.Record
		for _, capability := range change.Invalidated {
			record, err := sessionRecord(sessionCapabilityDeleteRecord, persistedCapability{Capability: capability})
			if err != nil {
				return err
			}
			records = append(records, record)
		}
		var record agentsession.Record
		if session.canonicalMessages {
			record, err = sessionRecord(sessionMessageCheckpointRecord, change.Checkpoint)
		} else {
			record, err = sessionRecord(sessionTranscriptRecord, persistedSessionTranscript{EngineState: change.State})
		}
		if err != nil {
			return err
		}
		records = append(records, record)
		if err := session.appendRecordsLocked(ctx, records...); err != nil {
			return err
		}
	}
	session.engineState = change.State
	if session.canonicalMessages {
		session.messageCheckpoint = change.Checkpoint
	}
	for _, capability := range change.Invalidated {
		delete(session.capabilities, capability)
		delete(session.durableCapabilities, capability)
	}
	for _, id := range change.CompletionIDs {
		session.taskCompletions.delivered[id] = struct{}{}
		delete(session.taskCompletions.pending, id)
	}
	return nil
}
