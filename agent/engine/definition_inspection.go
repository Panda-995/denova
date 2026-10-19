package engine

import (
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// Inspection is a read-only preview of one prospective model step. The
// Definition identities and maintenance states identify the exact composition
// used to build ModelRequest without exposing Runtime or journal types.
type Inspection struct {
	Session agentschema.SessionView
	// Run is an inspection-only identity used while materializing dynamic
	// capabilities. It is never accepted by Runtime and grants no authority.
	Run                     agentschema.RunView
	DefinitionKey           string
	BehaviorKey             string
	MaterializedFingerprint string
	PrefixFingerprint       string
	ModelIdentity           agentschema.CapabilityIdentity
	Compaction              *agenthistory.CompactionState
	CompactionMetrics       agenthistory.CompactionMetrics
	ElisionMetrics          agenthistory.ElisionMetrics
	// ContextFragments is the exact bounded provenance materialized by the
	// selected Definition before model middleware. ModelRequest remains the
	// sole provider-visible payload; diagnostics use these fragments to explain
	// which source contributed bytes without rebuilding host context.
	ContextFragments []agentschema.ContextFragment
	ModelRequest     agentmodel.ModelRequestInspection
}
