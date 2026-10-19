package engine

import (
	"context"
	"errors"
	"sort"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// prepareElision is a side-effect-free maintenance stage. It uses canonical
// coordinates for durability, then rebuilds through the active middleware and
// provider estimator. Only the caller may commit the returned projection.
func prepareElision(
	ctx context.Context,
	prepared preparedDefinition,
	raw []*agentschema.Message,
	compaction agenthistory.CompactionRecord,
	compactionPresent bool,
	before *agentmodel.ModelRequestSnapshot,
	buildAfter func(agenthistory.ElisionRecord) (*preparedModelCall, error),
) (agenthistory.ElisionRecord, *preparedModelCall, error) {
	policy := prepared.definition.Elision
	if policy == nil {
		return prepared.elision, nil, nil
	}
	size, err := before.EstimateInput()
	if err != nil {
		return agenthistory.ElisionRecord{}, nil, err
	}
	calibration := elisionCalibration(before)
	reserve := policy.ReservedTokens + prepared.goalReservedTokens
	if output := before.ResolvedOptions().MaxTokens; output != nil {
		reserve = agentmodel.CapacityAwareTokenReserve(reserve, *output, policy.ContextWindowTokens, policy.TriggerRatio)
	}
	pressure := calibration.CalibratedTokens(size.Tokens) + reserve
	if pressure < int(float64(policy.ContextWindowTokens)*policy.TriggerRatio) {
		return prepared.elision, nil, nil
	}
	projected, err := agenthistory.ElisionForHistory(prepared.elision, compaction, compactionPresent).ProjectArchive(raw, prepared.archive)
	if err != nil {
		return agenthistory.ElisionRecord{}, nil, err
	}
	boundaries := agenthistory.InteractionBoundaries(raw)
	// Incomplete batches cannot displace the two complete steps protected here.
	end := len(boundaries) - 1
	recentTokens := agentmodel.EstimateMessagesTextTokens(projected[boundaries[end]:])
	kept := 0
	for end > 0 && (kept < 2 || recentTokens < policy.ContextWindowTokens*3/10) {
		recentTokens += agentmodel.EstimateMessagesTextTokens(projected[boundaries[end-1]:boundaries[end]])
		end--
		kept++
	}
	start := 0
	if compactionPresent && !compaction.Removed {
		start = prepared.archive.Local(compaction.ReplacementTo)
	}
	next := agenthistory.ElisionRecord{Version: 1, Revision: prepared.elision.Revision + 1, ClearRevision: prepared.clearRevision}
	selected := make(map[int]bool)
	for _, replacement := range prepared.elision.Replacements {
		if prepared.archive.Local(replacement.MessageIndex) >= start && prepared.archive.Contains(replacement.MessageIndex) {
			next.Replacements = append(next.Replacements, replacement)
			selected[prepared.archive.Local(replacement.MessageIndex)] = true
		}
	}
	previousCount := len(next.Replacements)
	targetSavings := max(policy.MinimumSavingsTokens, pressure-int(float64(policy.ContextWindowTokens)*policy.TriggerRatio*.85))
	saved := 0
	// Prefer the newest eligible groups: keep the longest possible unchanged
	// prefix, and batch enough savings to avoid repeated cache invalidations.
	for group := end - 1; group >= 0 && saved < targetSavings; group-- {
		if err := ctx.Err(); err != nil {
			return agenthistory.ElisionRecord{}, nil, err
		}
		from, to := boundaries[group], boundaries[group+1]
		if from < start {
			break
		}
		calls := make(map[string]agentschema.ToolCall)
		eligible := true
		for _, message := range raw[from:to] {
			if message == nil {
				continue
			}
			for _, call := range message.ToolCalls {
				calls[call.ID] = call
			}
			if message.Role == agentschema.ToolRole && (agenthistory.ElisionPlaceholder(message) == "" || !elisionRecoveryAvailable(message, calls[message.ToolCallID], prepared)) {
				eligible = false
				break
			}
		}
		if !eligible {
			continue
		}
		for index := from; index < to && len(next.Replacements) < agenthistory.MaxElisionReplacements; index++ {
			message := raw[index]
			if message == nil || message.Role != agentschema.ToolRole || selected[index] {
				continue
			}
			stub := agenthistory.ElisionPlaceholder(message)
			gain := agentmodel.EstimateTextTokens(projected[index].Content) - agentmodel.EstimateTextTokens(stub)
			if gain < 256 {
				continue
			}
			fingerprint, err := agentschema.HashCanonical(message)
			if err != nil {
				return agenthistory.ElisionRecord{}, nil, err
			}
			next.Replacements = append(next.Replacements, agenthistory.ElisionReplacement{MessageIndex: prepared.archive.Raw(index), SourceHash: fingerprint})
			selected[index] = true
			saved += gain
		}
	}
	if len(next.Replacements) == previousCount || saved < policy.MinimumSavingsTokens {
		return prepared.elision, nil, nil
	}
	sort.Slice(next.Replacements, func(i, j int) bool { return next.Replacements[i].MessageIndex < next.Replacements[j].MessageIndex })
	after, err := buildAfter(next)
	if err != nil {
		return agenthistory.ElisionRecord{}, nil, err
	}
	snapshot := after.call.Snapshot()
	afterSize, err := snapshot.EstimateInput()
	if err != nil {
		return agenthistory.ElisionRecord{}, nil, err
	}
	if size.Bytes <= afterSize.Bytes || calibration.CalibratedTokens(size.Tokens)-calibration.CalibratedTokens(afterSize.Tokens) < policy.MinimumSavingsTokens {
		return prepared.elision, nil, nil
	}
	beforeMessages, afterMessages := before.Messages(), snapshot.Messages()
	common := 0
	for common < min(len(beforeMessages), len(afterMessages)) {
		equal, err := canonicalMessagesEqual(beforeMessages[common], afterMessages[common])
		if err != nil {
			return agenthistory.ElisionRecord{}, nil, err
		}
		if !equal {
			break
		}
		common++
	}
	if common < before.StablePrefixMessages() {
		return agenthistory.ElisionRecord{}, nil, errors.New("Elision changed the stable context prefix")
	}
	prefix, err := before.WithMessages(beforeMessages[:common]).EstimateInput()
	if err != nil {
		return agenthistory.ElisionRecord{}, nil, err
	}
	next.Metrics = agenthistory.ElisionMetrics{
		TokensBefore: calibration.CalibratedTokens(size.Tokens), TokensAfter: calibration.CalibratedTokens(afterSize.Tokens),
		ResultsElided: len(next.Replacements) - previousCount, CacheExpectedPrefixTokens: prefix.Tokens,
	}
	return next, after, ctx.Err()
}

func elisionRecoveryAvailable(message *agentschema.Message, call agentschema.ToolCall, prepared preparedDefinition) bool {
	result := message.ToolResult
	kind := result.ContextHints.Recovery.Kind
	originalReadOnly := false
	for _, definition := range prepared.toolSnapshots {
		if definition.Info != nil && definition.Info.Name == call.Function.Name {
			originalReadOnly = definition.Descriptor.Recovery == agenttool.ToolRecoveryReadOnly && definition.Descriptor.MutationScope == agenttool.ToolMutationNone
			break
		}
	}
	for _, definition := range prepared.toolSnapshots {
		if definition.Info == nil {
			continue
		}
		descriptor := definition.Descriptor
		if kind == agentschema.ToolResultRecoveryArtifact {
			// Complete artifacts are authenticated by result processing. Recovery
			// requires an ordinary read capability; side effects retain a receipt.
			if descriptor.ResultRecoveryKind == agentschema.ToolResultRecoveryRead && descriptor.Recovery == agenttool.ToolRecoveryReadOnly &&
				(originalReadOnly || result.ProtectedReceipt != nil && result.ProtectedReceipt.Outcome != "") {
				return true
			}
		} else if definition.Info.Name == call.Function.Name && descriptor.ResultRecoveryKind == kind &&
			originalReadOnly {
			return true
		}
	}
	return false
}

func elisionCalibration(snapshot *agentmodel.ModelRequestSnapshot) agenthistory.CompactionMetrics {
	messages := snapshot.Messages()
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message == nil || message.Role != agentschema.Assistant || message.ResponseMeta == nil || message.ResponseMeta.Usage == nil {
			continue
		}
		estimate := message.ResponseMeta.InputEstimate
		if estimate != nil && estimate.Version == agentmodel.InputEstimateVersion && estimate.Model == snapshot.ModelIdentity() {
			return agenthistory.CompactionMetrics{ObservedPromptTokens: message.ResponseMeta.Usage.PromptTokens, ObservedEstimateTokens: estimate.Tokens}
		}
		break
	}
	return agenthistory.CompactionMetrics{}
}
