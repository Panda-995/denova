package session

import (
	"context"
	"encoding/json"

	"denova/internal/agents/sessionjournal"
	agentsession "github.com/alfredxw/denova/agent/session"
)

// LoadCapability reads product-owned runtime state without creating an Agent.
func (s *Session) LoadCapability(ctx context.Context, key agentsession.Key, capability string) (json.RawMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := s.refreshCanonicalTailLocked(); err != nil {
		return nil, false, err
	}
	return s.projection.AgentSessions.Capability(key, capability)
}

// UpdateCapabilities belongs to the selected application runtime. It must not
// race a Native Session owning the same capability; switching drains execution.
// All changes share one journal transaction and the supplied format backup.
func (s *Session) UpdateCapabilities(ctx context.Context, key agentsession.Key, upgrade string, update func(sessionjournal.CapabilityReader) (map[string]json.RawMessage, error)) error {
	return s.withCanonicalMutation(ctx, "update runtime capabilities", func() error {
		records, err := s.projection.AgentSessions.CapabilityChanges(key, update)
		if err != nil || len(records) == 0 {
			return err
		}
		payloads := make([]any, len(records))
		for i, record := range records {
			payloads[i] = record
		}
		_, err = s.appendJournalRecordsWithUpgradeLocked(upgrade, payloads...)
		return err
	})
}
