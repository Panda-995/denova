package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentsession "github.com/alfredxw/denova/agent/session"
)

// CapabilityIdentity is stable across process restarts. ConfigHash contains
// only canonical behavior configuration; credentials and process addresses
// must never be included.
type CapabilityIdentity struct {
	Kind       string `json:"kind"`
	Version    uint16 `json:"version"`
	ConfigHash string `json:"config_hash,omitempty"`
}

func (identity CapabilityIdentity) Validate(name string) error {
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		return fmt.Errorf("%s capability identity is incomplete", name)
	}
	return nil
}

type ContextPlacement string

const (
	ContextLeadingMessage ContextPlacement = "leading_message"
	// ContextStateMessage appends a durable, model-visible state update to the
	// raw transcript. Agent emits a new message only when the named state
	// changes, is removed, or must be restored after Compaction.
	ContextStateMessage         ContextPlacement = "state_message"
	ContextCompactionCheckpoint ContextPlacement = "compaction_checkpoint"
	ContextFinalUserPrefix      ContextPlacement = "final_user_prefix"
	// ContextFinalUserMessage replaces the model-visible user message for this
	// cycle. The raw Input remains durable and is still passed to Canonical.
	// This placement lets hosts preserve localized, audited context renderers
	// without moving their presentation policy into the Agent module.
	ContextFinalUserMessage ContextPlacement = "final_user_message"
	ContextAuditOnly        ContextPlacement = "audit_only"
)

// ContextStability defines the lifecycle of a model-visible fragment
// independently from where it is rendered. Keeping this contract explicit
// prevents mutable state from accidentally invalidating a stable cache prefix.
type ContextStability string

const (
	ContextStablePrefix ContextStability = "stable_prefix"
	ContextSessionState ContextStability = "session_state"
	ContextTurn         ContextStability = "turn"
	ContextCheckpoint   ContextStability = "checkpoint"
	ContextAudit        ContextStability = "audit"
)

// ContextRendering controls only the model-visible wrapper. Provenance and
// bounds remain mandatory for both modes.
type ContextRendering string

const (
	ContextRenderAttributed ContextRendering = "attributed"
	ContextRenderVerbatim   ContextRendering = "verbatim"
)

// ContextFragment makes the provenance and hard bound of every injected byte
// explicit. Content over HardLimit is rejected instead of silently truncated.
type ContextFragment struct {
	Source   string
	Purpose  string
	Resource string
	Revision string
	// StateID is required for session_state fragments and must remain stable
	// across revisions of the same logical state section.
	StateID   string
	Stability ContextStability
	Placement ContextPlacement
	Rendering ContextRendering
	// Role applies to leading messages. Empty selects System; User is useful
	// for hosts whose stable cache prefix is intentionally represented as a
	// contextual user message.
	Role      RoleType
	Content   string
	HardLimit int
}

// TurnDelivery identifies how the current input entered a Run. Reason describes
// why Source is being called, including interaction and structural cycles that
// do not admit a new input.
type TurnDelivery string

const (
	TurnDeliveryStart    TurnDelivery = "start"
	TurnDeliverySteer    TurnDelivery = "steer"
	TurnDeliveryFollowUp TurnDelivery = "follow_up"
	TurnDeliveryNextTurn TurnDelivery = "next_turn"
)

type SessionView struct {
	Key      agentsession.Key
	Revision uint64
}

type RunView struct {
	ID        string
	CommandID string
	Cycle     int
	// StartedAt is captured once when this cycle starts. Context sources should
	// use it instead of reading the wall clock while assembling one request.
	StartedAt  time.Time
	Delivery   TurnDelivery
	Autonomous bool
}

func HashCanonical(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode agent identity: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}
