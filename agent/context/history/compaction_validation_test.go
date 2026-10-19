package history

import (
	"testing"
)

func TestCompactionCalibrationNeverReducesLocalEstimate(t *testing.T) {
	metrics := CompactionMetrics{
		ObservedPromptTokens:   500,
		ObservedEstimateTokens: 1000,
	}
	if got := metrics.CalibratedTokens(800); got != 800 {
		t.Fatalf("downward provider calibration = %d, want local estimate 800", got)
	}
}
