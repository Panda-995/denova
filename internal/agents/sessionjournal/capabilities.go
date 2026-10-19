package sessionjournal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	agentsession "github.com/alfredxw/denova/agent/session"
)

// CapabilityReader reads one state from the transaction's canonical snapshot.
// It is valid only inside the mutation callback and must not be retained.
type CapabilityReader func(string) (json.RawMessage, bool, error)

// CapabilityChanges prepares related state replacements in one transaction.
// Callbacks are pure and may be retried after CAS. Only changed keys are written;
// nil values leave a key unchanged. The caller must append every returned record
// atomically before applying any of them to the projection.
func (projection *Projection) CapabilityChanges(key agentsession.Key, update func(CapabilityReader) (map[string]json.RawMessage, error)) ([]*Envelope, error) {
	changes, err := update(func(capability string) (json.RawMessage, bool, error) {
		return projection.Capability(key, capability)
	})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(changes))
	for capability := range changes {
		keys = append(keys, capability)
	}
	sort.Strings(keys)
	var records []*Envelope
	for _, capability := range keys {
		record, err := projection.CapabilityUpdate(key, capability, func(json.RawMessage, bool) (json.RawMessage, error) {
			return changes[capability], nil
		})
		if err != nil {
			return nil, err
		}
		if record != nil {
			record.Revision += agentsession.Revision(len(records))
			records = append(records, record)
		}
	}
	return records, nil
}

// Capability reads the latest versioned state from the owning product journal.
// Applications may reuse state schemas across peer runtimes; only the selected
// runtime may mutate a capability, after the previous executor has drained.
func (projection *Projection) Capability(key agentsession.Key, capability string) (json.RawMessage, bool, error) {
	stream, err := projection.stream(key)
	if err != nil || stream == nil {
		return nil, false, err
	}
	record, found := stream.Recovery.CapabilityRecord(capability)
	if !found || record.Kind == capabilityDeleteKind {
		return nil, false, nil
	}
	var value struct {
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(record.Data, &value); err != nil {
		return nil, false, err
	}
	return value.State, true, nil
}

// CapabilityUpdate prepares an atomic state replacement under the product's
// canonical mutation fence. The callback is pure and may be retried after CAS.
// A nil result leaves state unchanged; explicit tombstones use their schema.
func (projection *Projection) CapabilityUpdate(key agentsession.Key, capability string, update func(json.RawMessage, bool) (json.RawMessage, error)) (*Envelope, error) {
	raw, present, err := projection.Capability(key, capability)
	if err != nil {
		return nil, err
	}
	next, err := update(raw, present)
	if err != nil || next == nil || bytes.Equal(next, raw) {
		return nil, err
	}
	if !json.Valid(next) {
		return nil, fmt.Errorf("invalid capability state for %s", capability)
	}
	data, err := json.Marshal(struct {
		Capability string          `json:"capability"`
		State      json.RawMessage `json:"state"`
	}{capability, next})
	if err != nil {
		return nil, err
	}
	revision, err := projection.Revision(key)
	if err != nil {
		return nil, err
	}
	return &Envelope{Type: RecordType, Key: key, Revision: revision + 1, Kind: capabilitySetKind, Version: 1, Data: data}, nil
}
