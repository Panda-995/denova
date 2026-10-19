// Package engine owns Native Agent Definition preparation, model/tool execution,
// and checkpoint construction. Per-cycle requests carry narrow journal ports;
// this package never recovers concrete lifecycle handles from context.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcontext "github.com/alfredxw/denova/agent/context"
	agentcompaction "github.com/alfredxw/denova/agent/context/compaction"
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentgoal "github.com/alfredxw/denova/agent/engine/goal"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
	agenttoolresult "github.com/alfredxw/denova/agent/tool/result"
)

type TurnReason string

const (
	TurnReasonStart        TurnReason = "start"
	TurnReasonSteer        TurnReason = "steer"
	TurnReasonFollowUp     TurnReason = "follow_up"
	TurnReasonNextTurn     TurnReason = "next_turn"
	TurnReasonInteraction  TurnReason = "interaction"
	TurnReasonGoalMutation TurnReason = "goal_mutation"
	TurnReasonStructural   TurnReason = "structural"
)

type PrepareRequest struct {
	Session agentschema.SessionView
	Run     agentschema.RunView
	Input   agentschema.Input
	Reason  TurnReason

	DefinitionKey string
	BehaviorKey   string
	HostData      *agentschema.HostData
	Compaction    *agenthistory.CompactionState
}

// Source prepares a complete immutable Definition for one cycle. CanonicalInput
// is the deliberately narrow, provider-free admission phase: Agent invokes it
// after accepting the Input and before any model/context/tool preparation.
// Implementations must resolve only the CanonicalAdapter needed to persist the
// accepted user input; it must not assemble model context or call a provider.
type Source interface {
	Prepare(context.Context, PrepareRequest) (Definition, error)
	CanonicalInput(context.Context, PrepareRequest) (agentcanonical.CanonicalAdapter, error)
}

type SourceFunc func(context.Context, PrepareRequest) (Definition, error)

func (prepare SourceFunc) Prepare(ctx context.Context, request PrepareRequest) (Definition, error) {
	if prepare == nil {
		return Definition{}, errors.New("agent Definition Source is nil")
	}
	return prepare(ctx, request)
}

// CanonicalInput makes SourceFunc the simple no-product-canonical composition
// helper. Dynamic hosts that return Definition.Canonical must implement Source
// directly so accepted input can cross the canonical barrier before Prepare.
func (SourceFunc) CanonicalInput(context.Context, PrepareRequest) (agentcanonical.CanonicalAdapter, error) {
	return nil, nil
}

// DefinitionInitializer is implemented by declarative capabilities whose
// construction can fail. Agent initializes every such capability before it
// accepts a static Definition; dynamic Sources receive the same guarantee when
// their Definition is prepared. Implementations must be idempotent and safe to
// call more than once.
type DefinitionInitializer interface {
	InitializeDefinition(context.Context) error
}

// Definition is the complete composition root for one Agent cycle.
type Definition struct {
	// Key lets a dynamic Source find the same business configuration again.
	// Behavior and prefix identities are always derived by Agent.
	Key string

	Name          string
	Description   string
	Model         agentmodel.BaseChatModel
	ModelIdentity agentschema.CapabilityIdentity
	Instructions  string
	// AttachmentRoot is the current host's absolute owner root for durable
	// slash-relative user Attachment paths. Tool images use Artifacts' resolver.
	// It is runtime routing, not behavior
	// identity, and is therefore excluded from Definition fingerprints.
	AttachmentRoot string

	Tools agenttool.Toolset
	// ResultProcessor is the single fixed post-tool projection authority. It
	// runs outside host middleware so every tool path receives identical
	// materialization, result projection, cleanup, and transcript semantics.
	ResultProcessor agenttoolresult.ToolResultProcessor
	// Artifacts is used both by streaming tools and ResultProcessor. Its stable
	// identity participates in behavior validation, never the model prefix fingerprint.
	Artifacts   agenttool.ToolArtifactStorage
	Context     agentcontext.ContextSource
	Goal        agentgoal.GoalManager
	Compaction  agentcompaction.CompactionManager
	Elision     *agenthistory.ElisionPolicy
	Permission  agentpermission.PermissionPolicy
	Interaction agentinteraction.InteractionPolicy
	Canonical   agentcanonical.CanonicalAdapter
	// Effects owns tool mutation receipts independently of conversation commits.
	// Nil is valid only for tools that do not return Effects.
	Effects agentcanonical.EffectApplier

	Middlewares []agentmiddleware.Middleware
	Execution   agentexecution.ExecutionPolicy
}

func (definition Definition) Prepare(context.Context, PrepareRequest) (Definition, error) {
	return definition, nil
}

func (definition Definition) CanonicalInput(context.Context, PrepareRequest) (agentcanonical.CanonicalAdapter, error) {
	return definition.Canonical, nil
}

func validateDefinition(definition Definition) error {
	if definition.Model == nil {
		return errors.New("agent Definition Model is required")
	}
	if definition.Execution.MaxIterations < 0 {
		return errors.New("agent Definition MaxIterations cannot be negative")
	}
	if definition.Execution.ModelMaxAttempts < 0 {
		return errors.New("agent Definition ModelMaxAttempts cannot be negative")
	}
	if definition.Execution.IdleTimeout < 0 {
		return errors.New("agent Definition IdleTimeout cannot be negative")
	}
	if definition.Execution.MaxAutomaticCompactionFailures < 0 {
		return errors.New("agent Definition MaxAutomaticCompactionFailures cannot be negative")
	}
	if strings.TrimSpace(definition.Key) != definition.Key {
		return errors.New("agent Definition Key cannot contain surrounding whitespace")
	}
	if definition.Compaction != nil && definition.Compaction.SummaryLimitBytes() <= 0 {
		return errors.New("agent Definition Compaction summary limit must be positive")
	}
	if definition.ResultProcessor != nil {
		if err := definition.ResultProcessor.Identity().Validate("ToolResultProcessor"); err != nil {
			return err
		}
	}
	if definition.Artifacts != nil {
		if err := definition.Artifacts.Identity().Validate("ToolArtifactStorage"); err != nil {
			return err
		}
	}
	if definition.Effects != nil {
		if err := definition.Effects.Identity().Validate("Effects"); err != nil {
			return err
		}
	}
	return nil
}

func InitializeDefinition(ctx context.Context, definition Definition) (Definition, error) {
	var err error
	definition.Elision, err = agenthistory.NormalizeElisionPolicy(definition.Elision)
	if err != nil {
		return Definition{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	type component struct {
		name  string
		value any
	}
	components := []component{
		{name: "Model", value: definition.Model},
		{name: "Tools", value: definition.Tools},
		{name: "ToolResultProcessor", value: definition.ResultProcessor},
		{name: "ToolArtifactStorage", value: definition.Artifacts},
		{name: "Context", value: definition.Context},
		{name: "Goal", value: definition.Goal},
		{name: "Compaction", value: definition.Compaction},
		{name: "Permission", value: definition.Permission},
		{name: "Interaction", value: definition.Interaction},
		{name: "Canonical", value: definition.Canonical},
		{name: "Effects", value: definition.Effects},
	}
	for index, middleware := range definition.Middlewares {
		components = append(components, component{
			name: fmt.Sprintf("Middleware[%d]", index), value: middleware,
		})
	}
	var initializationErrors []error
	for _, candidate := range components {
		initializer, ok := candidate.value.(DefinitionInitializer)
		if !ok {
			continue
		}
		if err := initializer.InitializeDefinition(ctx); err != nil {
			initializationErrors = append(initializationErrors, fmt.Errorf("%s: %w", candidate.name, err))
		}
	}
	if err := errors.Join(initializationErrors...); err != nil {
		return Definition{}, fmt.Errorf("initialize agent Definition: %w", err)
	}
	if model, ok := definition.Model.(agentmodel.DefinitionModel); ok {
		identity := model.ModelIdentity()
		if err := identity.Validate("Model"); err != nil {
			return Definition{}, err
		}
		if definition.ModelIdentity == (agentschema.CapabilityIdentity{}) {
			definition.ModelIdentity = identity
		} else if definition.ModelIdentity != identity {
			return Definition{}, errors.New("agent Definition ModelIdentity does not match declarative Model")
		}
	}
	if err := validateDefinition(definition); err != nil {
		return Definition{}, err
	}
	return definition, nil
}

type preparedDefinition struct {
	historyHead             agentcanonical.CanonicalHistoryHead
	archive                 *agenthistory.HistoryArchive
	activeModelUser         *agentschema.Message
	activeUserIndex         int
	lastResponseOrdinal     int
	definition              Definition
	tools                   []agenttool.ToolDefinition
	toolSnapshots           []agenttool.ToolDefinitionSnapshot
	fragments               []agentschema.ContextFragment
	goalFragments           []agentschema.ContextFragment
	goalReservedTokens      int
	definitionKey           string
	behaviorKey             string
	prefixFingerprint       string
	materializedFingerprint string
	definitionOperationID   string
	definitionCommandID     string
	definitionCycle         int
	preparationStage        enginePreparationStage
	hostData                *agentschema.HostData
	clearRevision           uint64
	contextState            agenthistory.ContextStateSnapshot
	contextSequence         int
	elision                 agenthistory.ElisionRecord
}

func prepareDefinition(
	ctx context.Context,
	source Source,
	request PrepareRequest,
) (preparedDefinition, error) {
	prepared, err := prepareDefinitionBase(ctx, source, request)
	if err != nil {
		return preparedDefinition{}, err
	}
	if err := materializeDefinitionCapabilities(ctx, request, &prepared); err != nil {
		return preparedDefinition{}, err
	}
	return prepared, nil
}

// prepareDefinitionBase resolves the immutable composition and its identity
// without materializing tool or context capabilities. Agent uses this phase to
// fence the exact Definition and canonicalize accepted input before any
// product ContextSource reads the resulting state.
func prepareDefinitionBase(
	ctx context.Context,
	source Source,
	request PrepareRequest,
) (preparedDefinition, error) {
	definition, err := source.Prepare(ctx, request)
	if err != nil {
		return preparedDefinition{}, fmt.Errorf("prepare agent Definition: %w", err)
	}
	definition, err = InitializeDefinition(ctx, definition)
	if err != nil {
		return preparedDefinition{}, err
	}
	behaviorKey, err := definitionBehaviorIdentity(definition)
	if err != nil {
		return preparedDefinition{}, err
	}
	definitionKey := strings.TrimSpace(definition.Key)
	if definitionKey == "" {
		definitionKey = behaviorKey
	}
	return preparedDefinition{
		definition: definition, definitionKey: definitionKey, behaviorKey: behaviorKey,
	}, nil
}

// DefinitionBehaviorIdentity returns the stable behavior identity used by
// Agent before materializing tools or Context. Composition catalogs may embed
// it to fence a child Definition without duplicating the lifecycle's identity
// vocabulary. It hashes behavior only; credentials and process addresses must
// stay out of every CapabilityIdentity supplied by the caller.
func DefinitionBehaviorIdentity(definition Definition) (string, error) {
	initialized, err := InitializeDefinition(context.Background(), definition)
	if err != nil {
		return "", err
	}
	return definitionBehaviorIdentity(initialized)
}

func definitionBehaviorIdentity(definition Definition) (string, error) {
	return agentschema.HashCanonical(definitionIdentity{
		DefinitionKey: definition.Key,
		Name:          definition.Name, Model: definition.ModelIdentity,
		Instructions: definition.Instructions, Execution: identityOfExecution(definition.Execution),
		Toolset: identityOfToolset(definition.Tools), ResultProcessor: agenttoolresult.IdentityOfToolResultProcessor(definition.ResultProcessor),
		Artifacts: agenttool.IdentityOfToolArtifactStorage(definition.Artifacts), Context: identityOfContext(definition.Context),
		Goal: identityOfGoal(definition.Goal), Compaction: identityOfCompaction(definition.Compaction),
		Elision:    definition.Elision,
		Permission: identityOfPermission(definition.Permission), Interaction: identityOfInteraction(definition.Interaction),
		Canonical:   identityOfCanonical(definition.Canonical),
		Effects:     identityOfEffects(definition.Effects),
		Middlewares: middlewareIdentities(definition.Middlewares),
	})
}

func materializeDefinitionCapabilities(
	ctx context.Context,
	request PrepareRequest,
	prepared *preparedDefinition,
) error {
	if prepared == nil {
		return errors.New("materialize agent Definition capabilities: prepared Definition is nil")
	}
	if err := materializeDefinitionTools(ctx, request, prepared); err != nil {
		return err
	}

	definition := prepared.definition
	var err error
	var fragments []agentschema.ContextFragment
	if definition.Context != nil {
		fragments, err = definition.Context.Materialize(ctx, agentcontext.ContextRequest{
			Session: request.Session, Run: request.Run, Input: request.Input,
			Compaction: agenthistory.CloneCompactionState(request.Compaction),
		})
		if err != nil {
			return fmt.Errorf("materialize agent Context: %w", err)
		}
	}
	fragments = append(fragments, request.Input.Context...)
	if err := validateContextFragments(fragments); err != nil {
		return err
	}
	prepared.fragments = fragments
	prepared.goalFragments = nil
	prepared.goalReservedTokens = 0
	return updatePreparedPrefixFingerprint(prepared)
}

func rematerializeDefinitionContext(
	ctx context.Context,
	request PrepareRequest,
	prepared *preparedDefinition,
) error {
	if prepared == nil {
		return errors.New("rematerialize agent Context: prepared Definition is nil")
	}
	var fragments []agentschema.ContextFragment
	var err error
	if prepared.definition.Context != nil {
		fragments, err = prepared.definition.Context.Materialize(ctx, agentcontext.ContextRequest{
			Session: request.Session, Run: request.Run, Input: request.Input,
			Compaction: agenthistory.CloneCompactionState(request.Compaction),
		})
		if err != nil {
			return fmt.Errorf("rematerialize agent Context: %w", err)
		}
	}
	fragments = append(fragments, request.Input.Context...)
	fragments = append(fragments, prepared.goalFragments...)
	if err := validateContextFragments(fragments); err != nil {
		return err
	}
	prepared.fragments = fragments
	if err := updatePreparedPrefixFingerprint(prepared); err != nil {
		return err
	}
	if prepared.preparationStage == enginePreparationMaterialized {
		prepared.materializedFingerprint, err = materializedDefinitionFingerprint(*prepared)
	}
	return err
}

func updatePreparedPrefixFingerprint(prepared *preparedDefinition) error {
	if prepared == nil {
		return errors.New("fingerprint prepared Definition: prepared Definition is nil")
	}
	stableFragments := make([]agentschema.ContextFragment, 0, len(prepared.fragments))
	for _, fragment := range prepared.fragments {
		if fragment.Placement == agentschema.ContextLeadingMessage {
			stableFragments = append(stableFragments, fragment)
		}
	}
	var err error
	prepared.prefixFingerprint, err = agentschema.HashCanonical(struct {
		Model        agentschema.CapabilityIdentity
		Instructions string
		Tools        []agenttool.ToolDefinitionSnapshot
		Context      []agenthistory.ContextFragmentIdentity
		Middlewares  []agentschema.CapabilityIdentity
	}{
		prepared.definition.ModelIdentity, prepared.definition.Instructions,
		prepared.toolSnapshots, contextFragmentIdentities(stableFragments),
		middlewareIdentities(prepared.definition.Middlewares),
	})
	return err
}

type definitionIdentity struct {
	DefinitionKey   string
	Name            string
	Model           agentschema.CapabilityIdentity
	Instructions    string
	Execution       executionPolicyIdentity
	Toolset         agentschema.CapabilityIdentity
	ResultProcessor agentschema.CapabilityIdentity
	Artifacts       agentschema.CapabilityIdentity
	Context         agentschema.CapabilityIdentity
	Goal            agentschema.CapabilityIdentity
	Compaction      agentschema.CapabilityIdentity
	Elision         *agenthistory.ElisionPolicy `json:",omitempty"`
	Permission      agentschema.CapabilityIdentity
	Interaction     agentschema.CapabilityIdentity
	Canonical       agentschema.CapabilityIdentity
	Effects         agentschema.CapabilityIdentity
	Middlewares     []agentschema.CapabilityIdentity
}

type executionPolicyIdentity struct {
	Retry                          agentschema.CapabilityIdentity
	ModelMaxAttempts               int
	ToolParallelism                int
	MaxIterations                  int
	IdleTimeout                    time.Duration
	MaxAutomaticCompactionFailures int
}

func identityOfExecution(policy agentexecution.ExecutionPolicy) executionPolicyIdentity {
	retry := policy.RetryIdentity
	if policy.Retry == nil {
		retry = agentschema.CapabilityIdentity{Kind: "retry.none", Version: 1}
	}
	return executionPolicyIdentity{
		ModelMaxAttempts: max(1, policy.ModelMaxAttempts),
		Retry:            retry, ToolParallelism: policy.ToolParallelism, MaxIterations: policy.MaxIterations,
		IdleTimeout:                    policy.IdleTimeout,
		MaxAutomaticCompactionFailures: normalizedAutomaticCompactionFailureLimit(policy),
	}
}

func middlewareIdentities(middlewares []agentmiddleware.Middleware) []agentschema.CapabilityIdentity {
	identities := make([]agentschema.CapabilityIdentity, len(middlewares))
	for index, middleware := range middlewares {
		if identified, ok := middleware.(agentmiddleware.IdentifiedMiddleware); ok {
			identities[index] = identified.Identity()
		}
	}
	return identities
}

func contextFragmentIdentities(fragments []agentschema.ContextFragment) []agenthistory.ContextFragmentIdentity {
	identities := make([]agenthistory.ContextFragmentIdentity, 0, len(fragments))
	for _, fragment := range fragments {
		hash := sha256.Sum256([]byte(fragment.Content))
		identities = append(identities, agenthistory.ContextFragmentIdentity{
			Source: fragment.Source, Purpose: fragment.Purpose, Resource: fragment.Resource,
			StateID: fragment.StateID, Stability: fragment.Stability,
			Revision: fragment.Revision, Placement: fragment.Placement, Rendering: agenthistory.EffectiveContextRendering(fragment.Rendering),
			Role:        effectiveContextRole(fragment),
			ContentHash: hex.EncodeToString(hash[:]), Bytes: len(fragment.Content),
		})
	}
	return identities
}

func identityOfToolset(toolset agenttool.Toolset) agentschema.CapabilityIdentity {
	if toolset == nil {
		return agentschema.CapabilityIdentity{Kind: "tools.none", Version: 1}
	}
	return toolset.Identity()
}

func identityOfContext(source agentcontext.ContextSource) agentschema.CapabilityIdentity {
	if source == nil {
		return agentschema.CapabilityIdentity{Kind: "context.none", Version: 1}
	}
	return source.Identity()
}

func identityOfCanonical(adapter agentcanonical.CanonicalAdapter) agentschema.CapabilityIdentity {
	if adapter == nil {
		return agentschema.CapabilityIdentity{Kind: "canonical.none", Version: 1}
	}
	return adapter.Identity()
}

func identityOfEffects(applier agentcanonical.EffectApplier) agentschema.CapabilityIdentity {
	if applier == nil {
		return agentschema.CapabilityIdentity{Kind: "effects.none", Version: 1}
	}
	return applier.Identity()
}

func identityOfGoal(manager agentgoal.GoalManager) agentschema.CapabilityIdentity {
	if manager == nil {
		return agentschema.CapabilityIdentity{Kind: "goal.none", Version: 1}
	}
	return manager.Identity()
}

func identityOfCompaction(manager agentcompaction.CompactionManager) agentschema.CapabilityIdentity {
	if manager == nil {
		return agentschema.CapabilityIdentity{Kind: "compaction.none", Version: 1}
	}
	return manager.Identity()
}

func identityOfPermission(policy agentpermission.PermissionPolicy) agentschema.CapabilityIdentity {
	if policy == nil {
		return agentschema.CapabilityIdentity{Kind: "permission.safe_default", Version: 1}
	}
	return policy.Identity()
}

func identityOfInteraction(policy agentinteraction.InteractionPolicy) agentschema.CapabilityIdentity {
	return agentinteraction.EffectiveInteractionPolicy(policy).Identity()
}

func validateContextFragments(fragments []agentschema.ContextFragment) error {
	finalUserMessages := 0
	finalUserPrefixes := 0
	for index, fragment := range fragments {
		if strings.TrimSpace(fragment.Source) == "" || strings.TrimSpace(fragment.Purpose) == "" ||
			strings.TrimSpace(fragment.Resource) == "" || fragment.HardLimit <= 0 {
			return fmt.Errorf("agent Context fragment %d requires source, purpose, resource, and HardLimit", index)
		}
		if len(fragment.Content) > fragment.HardLimit {
			return fmt.Errorf("agent Context fragment %d exceeds its %d-byte hard limit", index, fragment.HardLimit)
		}
		switch fragment.Placement {
		case agentschema.ContextLeadingMessage, agentschema.ContextStateMessage, agentschema.ContextCompactionCheckpoint, agentschema.ContextAuditOnly:
		case agentschema.ContextFinalUserPrefix:
			finalUserPrefixes++
		case agentschema.ContextFinalUserMessage:
			finalUserMessages++
		default:
			return fmt.Errorf("agent Context fragment %d has invalid placement %q", index, fragment.Placement)
		}
		switch fragment.Stability {
		case agentschema.ContextStablePrefix:
			if fragment.Placement != agentschema.ContextLeadingMessage {
				return fmt.Errorf("agent Context fragment %d stable_prefix requires leading_message placement", index)
			}
		case agentschema.ContextSessionState:
			if fragment.Placement != agentschema.ContextStateMessage || strings.TrimSpace(fragment.StateID) == "" {
				return fmt.Errorf("agent Context fragment %d session_state requires state_message placement and StateID", index)
			}
		case agentschema.ContextTurn:
			if fragment.Placement != agentschema.ContextFinalUserPrefix && fragment.Placement != agentschema.ContextFinalUserMessage {
				return fmt.Errorf("agent Context fragment %d turn stability requires final user placement", index)
			}
		case agentschema.ContextCheckpoint:
			if fragment.Placement != agentschema.ContextCompactionCheckpoint {
				return fmt.Errorf("agent Context fragment %d checkpoint stability requires compaction_checkpoint placement", index)
			}
		case agentschema.ContextAudit:
			if fragment.Placement != agentschema.ContextAuditOnly {
				return fmt.Errorf("agent Context fragment %d audit stability requires audit_only placement", index)
			}
		default:
			return fmt.Errorf("agent Context fragment %d requires an explicit stability", index)
		}
		switch agenthistory.EffectiveContextRendering(fragment.Rendering) {
		case agentschema.ContextRenderAttributed, agentschema.ContextRenderVerbatim:
		default:
			return fmt.Errorf("agent Context fragment %d has invalid rendering %q", index, fragment.Rendering)
		}
		role := effectiveContextRole(fragment)
		if fragment.Placement == agentschema.ContextLeadingMessage {
			if role != agentschema.System && role != agentschema.User {
				return fmt.Errorf("agent Context fragment %d has invalid leading message role %q", index, fragment.Role)
			}
		} else if fragment.Role != "" {
			return fmt.Errorf("agent Context fragment %d sets a role outside leading_message placement", index)
		}
	}
	stateIDs := make(map[string]int)
	for index, fragment := range fragments {
		if fragment.Placement != agentschema.ContextStateMessage {
			continue
		}
		if previous, exists := stateIDs[fragment.StateID]; exists {
			return fmt.Errorf("agent Context fragments %d and %d reuse StateID %q", previous, index, fragment.StateID)
		}
		stateIDs[fragment.StateID] = index
	}
	if finalUserMessages > 1 {
		return errors.New("agent Context permits at most one final user message")
	}
	if finalUserMessages > 0 && finalUserPrefixes > 0 {
		return errors.New("agent Context final user message cannot be combined with final user prefixes")
	}
	return nil
}

func effectiveContextRole(fragment agentschema.ContextFragment) agentschema.RoleType {
	if fragment.Role == "" {
		return agentschema.System
	}
	return fragment.Role
}
