package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// Tool is the single provider-neutral execution interface. Long-running tools
// report ephemeral progress through EmitToolProgress and return one final,
// structured result.
type Tool interface {
	Info(context.Context) (*agentschema.ToolInfo, error)
	Run(context.Context, string, ...ToolOption) (agentschema.ToolResult, error)
}

// ToolSource classifies where a tool reads or changes state.
type ToolSource string

const (
	ToolSourceOther   ToolSource = "other"
	ToolSourceRead    ToolSource = "read"
	ToolSourceWrite   ToolSource = "write"
	ToolSourceShell   ToolSource = "shell"
	ToolSourceHistory ToolSource = "history"
	ToolSourceWeb     ToolSource = "web"
	ToolSourceImage   ToolSource = "image"
)

// ToolExecutionClass controls ordering inside one model-produced batch.
type ToolExecutionClass string

const (
	ToolExecutionParallelRead       ToolExecutionClass = "parallel_read"
	ToolExecutionWorkspaceExclusive ToolExecutionClass = "workspace_exclusive"
	ToolExecutionSessionExclusive   ToolExecutionClass = "session_exclusive"
	ToolExecutionConfigExclusive    ToolExecutionClass = "config_exclusive"
	ToolExecutionInteractiveWait    ToolExecutionClass = "interactive_wait"
	ToolExecutionChild              ToolExecutionClass = "child"
)

// ToolMutationScope identifies the state domain a successful call may
// change. It is deliberately independent from Source: a shell call reads and
// writes through one execution surface, while config and session mutations do
// not belong to the workspace mutation pipeline.
type ToolMutationScope string

const (
	ToolMutationNone      ToolMutationScope = "none"
	ToolMutationWorkspace ToolMutationScope = "workspace"
	ToolMutationSession   ToolMutationScope = "session"
	ToolMutationConfig    ToolMutationScope = "config"
	ToolMutationExternal  ToolMutationScope = "external"
)

// ToolPostCheckPolicy selects the domain-specific verification performed
// after a successful mutation. The policy must match MutationScope.
type ToolPostCheckPolicy string

const (
	ToolPostCheckNone            ToolPostCheckPolicy = "none"
	ToolPostCheckWorkspaceChange ToolPostCheckPolicy = "workspace_change"
	ToolPostCheckSessionState    ToolPostCheckPolicy = "session_state"
	ToolPostCheckConfigRevision  ToolPostCheckPolicy = "config_revision"
	ToolPostCheckExternalReceipt ToolPostCheckPolicy = "external_receipt"
)

// ToolRecoveryClass documents a Tool's retry/idempotency semantics for
// permission and audit policy. Agent does not automatically recover a Tool.
type ToolRecoveryClass string

const (
	ToolRecoveryReadOnly      ToolRecoveryClass = "read_only"
	ToolRecoveryIdempotent    ToolRecoveryClass = "idempotent"
	ToolRecoveryReconcilable  ToolRecoveryClass = "reconcilable"
	ToolRecoveryNonIdempotent ToolRecoveryClass = "non_idempotent"
)

// SteeringPolicy controls what a pending safe preemption may do to a call that
// has already started.
type SteeringPolicy string

const (
	SteeringFinishCurrent     SteeringPolicy = "finish_current"
	SteeringInterruptibleWait SteeringPolicy = "interruptible_wait"
)

// ToolPresentationKind is a product-neutral render intent for one tool call
// or result. It never enters model context and deliberately describes semantic
// shape rather than a concrete frontend component.
type ToolPresentationKind string

const (
	ToolPresentationGeneric          ToolPresentationKind = "generic"
	ToolPresentationFile             ToolPresentationKind = "file"
	ToolPresentationSearch           ToolPresentationKind = "search"
	ToolPresentationTerminal         ToolPresentationKind = "terminal"
	ToolPresentationWeb              ToolPresentationKind = "web"
	ToolPresentationBrowser          ToolPresentationKind = "browser"
	ToolPresentationImage            ToolPresentationKind = "image"
	ToolPresentationInteractiveMedia ToolPresentationKind = "interactive_media"
	ToolPresentationTodo             ToolPresentationKind = "todo"
	ToolPresentationInteraction      ToolPresentationKind = "interaction"
	ToolPresentationDelegation       ToolPresentationKind = "delegation"
	ToolPresentationScript           ToolPresentationKind = "script"
)

// ToolPresentation keeps call and result rendering colocated with the tool
// contract. The host remains responsible for localized copy, visual styling,
// and the concrete renderer selected for each kind.
type ToolPresentation struct {
	Call   ToolPresentationKind `json:"call"`
	Result ToolPresentationKind `json:"result"`
}

// UniformToolPresentation declares the same semantic presentation for a call
// and its result.
func UniformToolPresentation(kind ToolPresentationKind) ToolPresentation {
	return ToolPresentation{Call: kind, Result: kind}
}

// Normalize fills the generic default and rejects unknown render intents.
// An omitted result inherits the call kind so partially authored descriptors
// still have one deterministic, repairable outcome.
func (presentation ToolPresentation) Normalize() (ToolPresentation, error) {
	if presentation.Call == "" {
		presentation.Call = ToolPresentationGeneric
	}
	if presentation.Result == "" {
		presentation.Result = presentation.Call
	}
	if !validToolPresentationKind(presentation.Call) {
		return ToolPresentation{}, fmt.Errorf("invalid tool call presentation %q", presentation.Call)
	}
	if !validToolPresentationKind(presentation.Result) {
		return ToolPresentation{}, fmt.Errorf("invalid tool result presentation %q", presentation.Result)
	}
	return presentation, nil
}

func validToolPresentationKind(kind ToolPresentationKind) bool {
	switch kind {
	case ToolPresentationGeneric, ToolPresentationFile, ToolPresentationSearch, ToolPresentationTerminal,
		ToolPresentationWeb, ToolPresentationBrowser, ToolPresentationImage, ToolPresentationInteractiveMedia, ToolPresentationTodo,
		ToolPresentationInteraction, ToolPresentationDelegation, ToolPresentationScript:
		return true
	default:
		return false
	}
}

// ToolDescriptor is the complete execution, recovery, context, and display
// contract for one model-visible tool.
type ToolDescriptor struct {
	Source        ToolSource          `json:"source"`
	Capability    string              `json:"capability,omitempty"`
	Execution     ToolExecutionClass  `json:"execution"`
	MutationScope ToolMutationScope   `json:"mutation_scope"`
	PostCheck     ToolPostCheckPolicy `json:"post_check"`
	Recovery      ToolRecoveryClass   `json:"recovery"`
	// ResultRecoveryKind names the ordinary model-visible capability that can
	// reconstruct a successful result after pressure cleanup. It is distinct
	// from Recovery, which describes crash/durability semantics.
	ResultRecoveryKind agentschema.ToolResultRecoveryKind  `json:"result_recovery_kind,omitempty"`
	ResultProjection   agentschema.ToolResultProjection    `json:"result_projection"`
	ResultRetention    agentschema.ToolResultRetentionMode `json:"result_retention"`
	Steering           SteeringPolicy                      `json:"steering"`
	MaxResultBytes     int                                 `json:"max_result_bytes"`
	// Presentation is display-only. Excluding it from descriptor JSON keeps
	// frontend-only changes out of model-prefix and recovery fingerprints;
	// lifecycle events persist it through their dedicated metadata envelope.
	Presentation ToolPresentation `json:"-"`
}

// Validate rejects incomplete descriptors and inconsistent safety claims.
func (descriptor ToolDescriptor) Validate() error {
	if _, err := descriptor.Presentation.Normalize(); err != nil {
		return err
	}
	if !validToolSource(descriptor.Source) {
		return fmt.Errorf("invalid tool source %q", descriptor.Source)
	}
	switch descriptor.Execution {
	case ToolExecutionParallelRead, ToolExecutionWorkspaceExclusive, ToolExecutionSessionExclusive,
		ToolExecutionConfigExclusive, ToolExecutionInteractiveWait, ToolExecutionChild:
	default:
		return fmt.Errorf("invalid tool execution class %q", descriptor.Execution)
	}
	switch descriptor.MutationScope {
	case ToolMutationNone, ToolMutationWorkspace, ToolMutationSession, ToolMutationConfig, ToolMutationExternal:
	default:
		return fmt.Errorf("invalid tool mutation scope %q", descriptor.MutationScope)
	}
	switch descriptor.PostCheck {
	case ToolPostCheckNone, ToolPostCheckWorkspaceChange, ToolPostCheckSessionState,
		ToolPostCheckConfigRevision, ToolPostCheckExternalReceipt:
	default:
		return fmt.Errorf("invalid tool post-check policy %q", descriptor.PostCheck)
	}
	switch descriptor.Recovery {
	case ToolRecoveryReadOnly, ToolRecoveryIdempotent, ToolRecoveryReconcilable, ToolRecoveryNonIdempotent:
	default:
		return fmt.Errorf("invalid tool recovery class %q", descriptor.Recovery)
	}
	switch descriptor.ResultRecoveryKind {
	case "", agentschema.ToolResultRecoveryRead, agentschema.ToolResultRecoveryRefetch, agentschema.ToolResultRecoveryRerun:
	case agentschema.ToolResultRecoveryArtifact:
		return errors.New("artifact result recovery is runtime output metadata, not a descriptor capability")
	default:
		return fmt.Errorf("invalid tool result recovery kind %q", descriptor.ResultRecoveryKind)
	}
	if descriptor.ResultProjection != agentschema.ToolResultBoundedModelContext {
		return fmt.Errorf("invalid tool result projection %q", descriptor.ResultProjection)
	}
	switch descriptor.ResultRetention {
	case agentschema.ToolResultDeferred, agentschema.ToolResultEagerCandidate, agentschema.ToolResultProtected:
	default:
		return fmt.Errorf("invalid tool result retention %q", descriptor.ResultRetention)
	}
	if descriptor.ResultRetention == agentschema.ToolResultEagerCandidate && descriptor.ResultRecoveryKind == "" {
		return errors.New("eager tool result requires an explicit result recovery kind")
	}
	switch descriptor.Steering {
	case SteeringFinishCurrent, SteeringInterruptibleWait:
	default:
		return fmt.Errorf("invalid tool steering policy %q", descriptor.Steering)
	}
	if descriptor.MaxResultBytes <= 0 {
		return errors.New("tool result limit must be positive")
	}
	if descriptor.Execution == ToolExecutionParallelRead && descriptor.MutationScope != ToolMutationNone {
		return errors.New("parallel read tool cannot mutate state")
	}
	if descriptor.Source == ToolSourceWrite && descriptor.MutationScope == ToolMutationNone {
		return errors.New("write-source tool must declare a mutation scope")
	}
	if err := validateToolExecutionMutation(descriptor.Execution, descriptor.MutationScope); err != nil {
		return err
	}
	if err := validateToolPostCheck(descriptor.MutationScope, descriptor.PostCheck); err != nil {
		return err
	}
	if descriptor.Steering == SteeringInterruptibleWait &&
		(descriptor.MutationScope != ToolMutationNone || descriptor.Recovery != ToolRecoveryReadOnly) {
		return errors.New("interruptible wait must be read-only and non-mutating")
	}
	return nil
}

// validToolSource keeps built-in descriptors predictable while allowing an
// application to name its own state domain. ToolSource is deliberately an
// open extension point: product concepts such as a lore database belong to
// the product adapter, not to the provider-neutral Agent vocabulary.
func validToolSource(source ToolSource) bool {
	value := string(source)
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for index, current := range []byte(value) {
		if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' {
			continue
		}
		if index > 0 && (current == '.' || current == '_' || current == '-') {
			continue
		}
		return false
	}
	return true
}

func validateToolExecutionMutation(execution ToolExecutionClass, scope ToolMutationScope) error {
	switch execution {
	case ToolExecutionParallelRead, ToolExecutionChild:
		if scope != ToolMutationNone {
			return fmt.Errorf("execution class %q requires mutation scope %q", execution, ToolMutationNone)
		}
	case ToolExecutionWorkspaceExclusive:
		if scope != ToolMutationWorkspace && scope != ToolMutationExternal {
			return fmt.Errorf("execution class %q requires workspace or external mutation", execution)
		}
	case ToolExecutionSessionExclusive:
		if scope != ToolMutationSession && scope != ToolMutationExternal {
			return fmt.Errorf("execution class %q requires session or external mutation", execution)
		}
	case ToolExecutionConfigExclusive:
		if scope != ToolMutationConfig {
			return fmt.Errorf("execution class %q requires mutation scope %q", execution, ToolMutationConfig)
		}
	case ToolExecutionInteractiveWait:
		if scope != ToolMutationNone && scope != ToolMutationSession {
			return fmt.Errorf("execution class %q requires no mutation or a session mutation", execution)
		}
	}
	return nil
}

func validateToolPostCheck(scope ToolMutationScope, policy ToolPostCheckPolicy) error {
	want := ToolPostCheckNone
	switch scope {
	case ToolMutationWorkspace:
		want = ToolPostCheckWorkspaceChange
	case ToolMutationSession:
		want = ToolPostCheckSessionState
	case ToolMutationConfig:
		want = ToolPostCheckConfigRevision
	case ToolMutationExternal:
		want = ToolPostCheckExternalReceipt
	}
	if policy != ToolPostCheckNone && policy != want {
		return fmt.Errorf("post-check policy %q does not match mutation scope %q", policy, scope)
	}
	return nil
}

// ToolDefinition is the only registration unit accepted by Agent.
type ToolDefinition struct {
	Tool       Tool
	Descriptor ToolDescriptor
	// ImplementationIdentity describes behavior not represented by the
	// provider-visible schema/descriptor. Built-in Adapter-backed tools always
	// set it. StaticToolsIdentified folds it into behavior identity without
	// polluting the model-prefix fingerprint.
	ImplementationIdentity agentschema.CapabilityIdentity
}

// Validate checks the concrete schema and its execution contract.
func (definition ToolDefinition) Validate(ctx context.Context) error {
	_, err := definition.snapshot(ctx)
	return err
}

func (definition ToolDefinition) snapshot(ctx context.Context) (ToolDefinitionSnapshot, error) {
	if definition.Tool == nil {
		return ToolDefinitionSnapshot{}, errors.New("tool definition has nil Tool")
	}
	if err := definition.Descriptor.Validate(); err != nil {
		return ToolDefinitionSnapshot{}, fmt.Errorf("tool descriptor: %w", err)
	}
	if definition.ImplementationIdentity != (agentschema.CapabilityIdentity{}) {
		if err := definition.ImplementationIdentity.Validate("tool implementation"); err != nil {
			return ToolDefinitionSnapshot{}, err
		}
	}
	info, err := definition.Tool.Info(ctx)
	if err != nil {
		return ToolDefinitionSnapshot{}, fmt.Errorf("read tool info: %w", err)
	}
	if info == nil || strings.TrimSpace(info.Name) == "" {
		return ToolDefinitionSnapshot{}, errors.New("tool definition has no stable name")
	}
	if info.Name != strings.TrimSpace(info.Name) {
		return ToolDefinitionSnapshot{}, fmt.Errorf("tool %q has leading or trailing whitespace", info.Name)
	}
	schema, err := info.ToJSONSchema()
	if err != nil {
		return ToolDefinitionSnapshot{}, fmt.Errorf("tool %q schema: %w", info.Name, err)
	}
	if err := validateToolSchema(schema); err != nil {
		return ToolDefinitionSnapshot{}, fmt.Errorf("tool %q schema: %w", info.Name, err)
	}
	descriptor := definition.Descriptor
	descriptor.Presentation, err = descriptor.Presentation.Normalize()
	if err != nil {
		return ToolDefinitionSnapshot{}, fmt.Errorf("tool %q presentation: %w", info.Name, err)
	}
	return ToolDefinitionSnapshot{Info: agentschema.CloneToolInfo(info), Descriptor: descriptor}, nil
}

// ToolDefinitionSnapshot contains immutable call metadata without exposing the
// concrete implementation.
type ToolDefinitionSnapshot struct {
	Info       *agentschema.ToolInfo `json:"info"`
	Descriptor ToolDescriptor        `json:"descriptor"`
}

// ToolErrorResult constructs a model-visible execution error.
func ToolErrorResult(modelContent, displayContent string) agentschema.ToolResult {
	if displayContent == "" {
		displayContent = modelContent
	}
	return agentschema.ToolResult{ModelContent: modelContent, DisplayContent: displayContent, Status: agentschema.ToolResultError}
}

// SyntheticToolResult constructs a paired result for a call that did not
// complete normally.
func SyntheticToolResult(status agentschema.ToolResultStatus, reason agentschema.ToolSyntheticReason, content string) agentschema.ToolResult {
	return agentschema.ToolResult{
		ModelContent: content, DisplayContent: content,
		Status: status, SyntheticReason: reason,
	}
}

// NormalizeToolResult validates and bounds a result using its descriptor. It is
// safe to call more than once; metadata is recalculated from visible content.
func NormalizeToolResult(result agentschema.ToolResult, descriptor ToolDescriptor) (agentschema.ToolResult, error) {
	if err := descriptor.Validate(); err != nil {
		return agentschema.ToolResult{}, err
	}
	switch result.Status {
	case agentschema.ToolResultSuccess, agentschema.ToolResultError, agentschema.ToolResultBlocked, agentschema.ToolResultSkipped:
	default:
		return agentschema.ToolResult{}, fmt.Errorf("invalid tool result status %q", result.Status)
	}
	switch result.SyntheticReason {
	case "", agentschema.ToolSyntheticUnknownTool, agentschema.ToolSyntheticInvalidCall, agentschema.ToolSyntheticInvalidArguments,
		agentschema.ToolSyntheticModelIncomplete, agentschema.ToolSyntheticPolicyBlocked, agentschema.ToolSyntheticSteeringBeforeStart,
		agentschema.ToolSyntheticSteeringInterrupted, agentschema.ToolSyntheticEffectUnknown:
	default:
		return agentschema.ToolResult{}, fmt.Errorf("invalid tool synthetic reason %q", result.SyntheticReason)
	}
	if result.Status == agentschema.ToolResultSuccess && result.SyntheticReason != "" {
		return agentschema.ToolResult{}, errors.New("successful tool result cannot be synthetic")
	}
	result.ResultRetention = descriptor.ResultRetention
	if err := agentschema.ValidateToolAttachments(result.Attachments); err != nil {
		return agentschema.ToolResult{}, err
	}
	result.Attachments = agentschema.CloneAttachments(result.Attachments)
	normalizedHints, err := agentschema.NormalizeToolResultContextHints(result.ContextHints)
	if err != nil {
		return agentschema.ToolResult{}, err
	}
	result.ContextHints = normalizedHints
	if result.ProtectedReceipt != nil {
		receipt := *result.ProtectedReceipt
		receipt.SanitizedArguments = strings.TrimSpace(strings.ToValidUTF8(receipt.SanitizedArguments, "\uFFFD"))
		receipt.Outcome = strings.TrimSpace(strings.ToValidUTF8(receipt.Outcome, "\uFFFD"))
		if len(receipt.SanitizedArguments) > descriptor.MaxResultBytes || len(receipt.Outcome) > descriptor.MaxResultBytes {
			return agentschema.ToolResult{}, fmt.Errorf("protected tool receipt exceeds %d bytes", descriptor.MaxResultBytes)
		}
		if receipt.SanitizedArguments != "" && !json.Valid([]byte(receipt.SanitizedArguments)) {
			return agentschema.ToolResult{}, errors.New("protected tool receipt arguments must be valid JSON")
		}
		if receipt.Outcome != "" && !json.Valid([]byte(receipt.Outcome)) {
			return agentschema.ToolResult{}, errors.New("protected tool receipt outcome must be valid JSON")
		}
		if receipt.SanitizedArguments == "" && receipt.Outcome == "" {
			result.ProtectedReceipt = nil
		} else {
			result.ProtectedReceipt = &receipt
		}
	}
	if len(result.Artifacts) > agentschema.MaxToolResultArtifacts {
		return agentschema.ToolResult{}, fmt.Errorf("tool result has %d artifacts; maximum is %d", len(result.Artifacts), agentschema.MaxToolResultArtifacts)
	}
	for index := range result.Artifacts {
		artifact := &result.Artifacts[index]
		artifact.ID = strings.TrimSpace(artifact.ID)
		artifact.Purpose = agentschema.ToolArtifactPurpose(strings.TrimSpace(string(artifact.Purpose)))
		switch artifact.Purpose {
		case "", agentschema.ToolArtifactPurposeCompleteModelOutput, agentschema.ToolArtifactPurposeCompleteToolOutput, agentschema.ToolArtifactPurposeAttachment:
		default:
			return agentschema.ToolResult{}, fmt.Errorf("tool artifact %d has invalid purpose %q", index, artifact.Purpose)
		}
		artifact.ReadablePath = strings.TrimSpace(strings.ToValidUTF8(artifact.ReadablePath, "\uFFFD"))
		artifact.ContentType = strings.TrimSpace(artifact.ContentType)
		if artifact.EstimatedTokens == 0 && artifact.EstimatedBytes > 0 && !agentschema.IsNativeImageMediaType(artifact.ContentType) {
			artifact.EstimatedTokens = agentschema.EstimateToolResultTokens(artifact.EstimatedBytes)
		}
		artifact.SHA256 = strings.ToLower(strings.TrimSpace(artifact.SHA256))
		if artifact.ID == "" || artifact.ReadablePath == "" || artifact.ContentType == "" ||
			artifact.EstimatedBytes < 0 || artifact.EstimatedTokens < 0 ||
			strings.ContainsRune(artifact.ReadablePath, '\x00') {
			return agentschema.ToolResult{}, fmt.Errorf("tool artifact %d is invalid", index)
		}
		if artifact.SHA256 != "" {
			if decoded, err := hex.DecodeString(artifact.SHA256); err != nil || len(decoded) != sha256.Size {
				return agentschema.ToolResult{}, fmt.Errorf("tool artifact %d has an invalid SHA-256", index)
			}
		}
	}
	artifactMetadataLimit := max(descriptor.MaxResultBytes, agentschema.MaxToolResultArtifactMetadataBytes)
	if encodedArtifacts, err := json.Marshal(result.Artifacts); err != nil {
		return agentschema.ToolResult{}, fmt.Errorf("encode tool artifacts: %w", err)
	} else if len(encodedArtifacts) > artifactMetadataLimit {
		return agentschema.ToolResult{}, fmt.Errorf("tool artifact metadata exceeds %d bytes", artifactMetadataLimit)
	}
	result.Artifacts = append([]agentschema.ToolArtifactRef(nil), result.Artifacts...)
	if len(result.Effects) > 64 {
		return agentschema.ToolResult{}, fmt.Errorf("tool result has %d effects; maximum is 64", len(result.Effects))
	}
	for index := range result.Effects {
		result.Effects[index].Kind = strings.TrimSpace(result.Effects[index].Kind)
		result.Effects[index].Data = append(json.RawMessage(nil), result.Effects[index].Data...)
		if result.Effects[index].Kind == "" || len(result.Effects[index].Kind) > 4096 || !json.Valid(result.Effects[index].Data) {
			return agentschema.ToolResult{}, fmt.Errorf("tool result effect %d is invalid", index)
		}
	}
	result.Effects = append([]agentschema.Effect(nil), result.Effects...)
	if result.Metadata.ArtifactPersistence != nil {
		persistence := *result.Metadata.ArtifactPersistence
		if err := agentschema.ValidateToolArtifactPersistence(persistence); err != nil {
			return agentschema.ToolResult{}, err
		}
		result.Metadata.ArtifactPersistence = &persistence
	}
	if len(result.Details) != 0 {
		if !json.Valid(result.Details) {
			return agentschema.ToolResult{}, errors.New("tool result details must be valid JSON")
		}
		if len(result.Details) > descriptor.MaxResultBytes {
			return agentschema.ToolResult{}, fmt.Errorf("tool result details exceed %d bytes", descriptor.MaxResultBytes)
		}
		result.Details = append(json.RawMessage(nil), result.Details...)
	}
	result.ModelContent = strings.ToValidUTF8(result.ModelContent, "\uFFFD")
	result.DisplayContent = strings.ToValidUTF8(result.DisplayContent, "\uFFFD")
	if result.DisplayContent == "" {
		result.DisplayContent = result.ModelContent
	}
	originalModelBytes := max(len(result.ModelContent), result.Metadata.OriginalModelBytes)
	originalDisplayBytes := max(len(result.DisplayContent), result.Metadata.OriginalDisplayBytes)
	result.Metadata.OriginalModelBytes = originalModelBytes
	result.Metadata.OriginalDisplayBytes = originalDisplayBytes
	modelContent, modelTruncated := truncateToolResult(result.ModelContent, descriptor.MaxResultBytes)
	displayContent, displayTruncated := truncateToolResult(result.DisplayContent, descriptor.MaxResultBytes)
	result.ModelContent = modelContent
	result.DisplayContent = displayContent
	result.Metadata.ModelTruncated = result.Metadata.ModelTruncated || modelTruncated
	result.Metadata.DisplayTruncated = result.Metadata.DisplayTruncated || displayTruncated
	result.Metadata.ReturnedModelBytes = len(result.ModelContent)
	result.Metadata.ReturnedDisplayBytes = len(result.DisplayContent)
	return result, nil
}

func truncateToolResult(content string, limit int) (string, bool) {
	if limit <= 0 || len(content) <= limit {
		return content, false
	}
	const suffix = "\n[tool result truncated]"
	end := limit - len(suffix)
	if end <= 0 {
		end = limit
		for end > 0 && !utf8.RuneStart(content[end]) {
			end--
		}
		return content[:end], true
	}
	for end > 0 && !utf8.RuneStart(content[end]) {
		end--
	}
	return strings.TrimRight(content[:end], "\n") + suffix, true
}

type toolProgressSink func(string)

type toolProgressContextKey struct{}

func ContextWithToolProgress(ctx context.Context, sink toolProgressSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, toolProgressContextKey{}, sink)
}

// EmitToolProgress emits one ephemeral display update. Progress never enters
// model context directly.
func EmitToolProgress(ctx context.Context, delta string) bool {
	if ctx == nil || delta == "" {
		return false
	}
	sink, _ := ctx.Value(toolProgressContextKey{}).(toolProgressSink)
	if sink == nil {
		return false
	}
	sink(delta)
	return true
}

type toolSteeringSignal struct {
	done    <-chan struct{}
	pending func() bool
}

type toolSteeringContextKey struct{}

func ContextWithToolSteering(ctx context.Context, done <-chan struct{}, pending func() bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, toolSteeringContextKey{}, toolSteeringSignal{done: done, pending: pending})
}

// ToolSteeringPending reports a safe preemption without consuming it.
func ToolSteeringPending(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	signal, _ := ctx.Value(toolSteeringContextKey{}).(toolSteeringSignal)
	return signal.pending != nil && signal.pending()
}

// ToolSteeringDone closes when a cancellation or safe preemption is requested.
// A nil channel means the call has no steering controller.
func ToolSteeringDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	signal, _ := ctx.Value(toolSteeringContextKey{}).(toolSteeringSignal)
	return signal.done
}
