package engine

import (
	"errors"
	"strings"
	"testing"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func compactionValidationSnapshot(messages []*agentschema.Message, stable int) *agentmodel.ModelRequestSnapshot {
	return (&modelCall{
		Model: &lifecycleModel{}, Messages: messages,
		stablePrefixMessages: stable,
	}).Snapshot()
}

func TestCompactionPostValidationDistinguishesRecoveryBandAndHardPublishBand(t *testing.T) {
	before := compactionValidationSnapshot([]*agentschema.Message{
		agentschema.SystemMessage(strings.Repeat("stable ", 100)),
		agentschema.UserMessage(strings.Repeat("old context ", 500)),
		agentschema.AssistantMessage(strings.Repeat("old answer ", 500), nil),
		agentschema.UserMessage("continue"),
	}, 1)
	degradedAfter := compactionValidationSnapshot([]*agentschema.Message{
		agentschema.SystemMessage(strings.Repeat("stable ", 100)),
		agentschema.SystemMessage(strings.Repeat("checkpoint ", 210)),
		agentschema.UserMessage("continue"),
	}, 2)
	afterTokens := agentmodel.EstimateRequestTextTokens(degradedAfter.Messages(), nil)
	window := max(afterTokens+1, int(float64(afterTokens)/.80))
	plan := agenthistory.CompactionPlan{
		GroupCount: 1,
		Validation: agenthistory.CompactionValidationPolicy{
			ContextWindowTokens: window, Threshold: .90, RecoveryBand: .80,
			HardLimitBytes: 8 << 20,
		},
	}
	metrics, err := validateCompactionProjection(before, degradedAfter, agenthistory.CompactionExecutionPlan{CompactionPlan: plan, SourceTo: 2})
	if err != nil || !metrics.Degraded || metrics.RecoveryBandMet ||
		metrics.ProjectedTokensAfter >= int(float64(window)*.90) {
		t.Fatalf("degraded metrics=%#v err=%v", metrics, err)
	}

	healthyAfter := compactionValidationSnapshot([]*agentschema.Message{
		agentschema.SystemMessage(strings.Repeat("stable ", 100)),
		agentschema.SystemMessage("short checkpoint"),
		agentschema.UserMessage("continue"),
	}, 2)
	healthy, err := validateCompactionProjection(before, healthyAfter, agenthistory.CompactionExecutionPlan{CompactionPlan: plan, SourceTo: 2})
	if err != nil || healthy.Degraded || !healthy.RecoveryBandMet {
		t.Fatalf("healthy metrics=%#v err=%v", healthy, err)
	}

	hardPlan := plan
	hardPlan.Validation.ContextWindowTokens = max(1, afterTokens)
	if metrics, err := validateCompactionProjection(before, degradedAfter, agenthistory.CompactionExecutionPlan{CompactionPlan: hardPlan, SourceTo: 2}); !errors.Is(err, agentschema.ErrContextLimit) || metrics.ProjectedTokensAfter < int(float64(afterTokens)*.90) {
		t.Fatalf("hard-band metrics=%#v err=%v", metrics, err)
	}
}

func TestCompactionPostValidationRejectsNoProgressAndInsignificantProgress(t *testing.T) {
	before := compactionValidationSnapshot([]*agentschema.Message{agentschema.UserMessage("small history")}, 0)
	larger := compactionValidationSnapshot([]*agentschema.Message{agentschema.SystemMessage(strings.Repeat("larger checkpoint ", 20))}, 1)
	plan := agenthistory.CompactionPlan{GroupCount: 1, Validation: agenthistory.CompactionValidationPolicy{HardLimitBytes: 8 << 20}}
	if _, err := validateCompactionProjection(before, larger, agenthistory.CompactionExecutionPlan{CompactionPlan: plan, SourceTo: 1}); err == nil || !strings.Contains(err.Error(), "no progress") {
		t.Fatalf("no-progress error = %v", err)
	}

	largeBefore := compactionValidationSnapshot([]*agentschema.Message{agentschema.UserMessage(strings.Repeat("history ", 100))}, 0)
	slightlySmaller := compactionValidationSnapshot([]*agentschema.Message{agentschema.SystemMessage(strings.Repeat("checkpoint ", 50))}, 1)
	progress := agentmodel.EstimateRequestTextTokens(largeBefore.Messages(), nil) - agentmodel.EstimateRequestTextTokens(slightlySmaller.Messages(), nil)
	plan.Validation.MinimumChangeTokens = progress + 1
	if _, err := validateCompactionProjection(largeBefore, slightlySmaller, agenthistory.CompactionExecutionPlan{CompactionPlan: plan, SourceTo: 1}); err == nil || !strings.Contains(err.Error(), "required minimum") {
		t.Fatalf("minimum-progress error = %v progress=%d", err, progress)
	}
}

func TestInteractiveCompactionCalibratesTruePostContextAfterStableReinjection(t *testing.T) {
	before := compactionValidationSnapshot([]*agentschema.Message{
		agentschema.UserMessage(strings.Repeat("original history ", 300)),
		agentschema.AssistantMessage("previous answer", nil),
		agentschema.UserMessage("continue"),
	}, 0)
	after := compactionValidationSnapshot([]*agentschema.Message{
		agentschema.UserMessage(strings.Repeat("resident lore remains exact ", 80)),
		agentschema.SystemMessage("bounded checkpoint"),
		agentschema.UserMessage("continue"),
	}, 2)
	localBefore := agentmodel.EstimateRequestTextTokens(before.Messages(), nil)
	localAfter := agentmodel.EstimateRequestTextTokens(after.Messages(), nil)
	const reserve = 78
	calibratedAfter := localAfter*2 + reserve
	window := (calibratedAfter*100 + 83) / 84
	plan := agenthistory.CompactionPlan{
		GroupCount: 1,
		Metrics: agenthistory.CompactionMetrics{
			ObservedPromptTokens: localBefore * 2, ObservedEstimateTokens: localBefore,
		},
		Validation: agenthistory.CompactionValidationPolicy{
			ContextWindowTokens: window, ReservedTokens: reserve,
			Threshold: .90, RecoveryBand: .80, HardLimitBytes: 8 << 20,
		},
	}
	metrics, err := validateCompactionProjection(before, after, agenthistory.CompactionExecutionPlan{CompactionPlan: plan, SourceTo: 2})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.EstimatedTokensAfter != localAfter || metrics.ProjectedTokensAfter != calibratedAfter {
		t.Fatalf("post-Compaction calibration local=%d calibrated=%d metrics=%#v", localAfter, calibratedAfter, metrics)
	}
	if metrics.RecoveryBandMet || !metrics.Degraded {
		t.Fatalf("provider-calibrated ~84%% projection was misclassified: %#v", metrics)
	}
}
