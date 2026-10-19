package model

import (
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestClassifyModelFinishReason(t *testing.T) {
	tests := []struct {
		reason       string
		wantClass    ModelFinishReasonClass
		wantTerminal string
		incomplete   bool
	}{
		{reason: "stop", wantClass: ModelFinishReasonOther},
		{reason: " length ", wantClass: ModelFinishReasonOutputLimit, wantTerminal: agentschema.ModelOutputTruncatedReason, incomplete: true},
		{reason: "max-output-tokens", wantClass: ModelFinishReasonOutputLimit, wantTerminal: agentschema.ModelOutputTruncatedReason, incomplete: true},
		{reason: "model_context_window_exceeded", wantClass: ModelFinishReasonContextLimit, wantTerminal: agentschema.ModelContextWindowExceededReason, incomplete: true},
		{reason: "content_filter", wantClass: ModelFinishReasonContentFilter, wantTerminal: agentschema.ModelOutputFilteredReason, incomplete: true},
		{reason: "MAX_TOKENS", wantClass: ModelFinishReasonOutputLimit, wantTerminal: agentschema.ModelOutputTruncatedReason, incomplete: true},
		{reason: "SAFETY", wantClass: ModelFinishReasonContentFilter, wantTerminal: agentschema.ModelOutputFilteredReason, incomplete: true},
		{reason: "MALFORMED_FUNCTION_CALL", wantClass: ModelFinishReasonIncomplete, wantTerminal: agentschema.ModelOutputIncompleteReason, incomplete: true},
		{reason: "incomplete", wantClass: ModelFinishReasonIncomplete, wantTerminal: agentschema.ModelOutputIncompleteReason, incomplete: true},
	}
	for _, test := range tests {
		t.Run(test.reason, func(t *testing.T) {
			class := ClassifyModelFinishReason(test.reason)
			if class != test.wantClass || class.Incomplete() != test.incomplete || class.TerminalReason() != test.wantTerminal {
				t.Fatalf("classification = (%q, %t, %q), want (%q, %t, %q)",
					class, class.Incomplete(), class.TerminalReason(), test.wantClass, test.incomplete, test.wantTerminal)
			}
		})
	}
}

func TestIsModelIncompleteTerminalReason(t *testing.T) {
	for _, reason := range []string{
		agentschema.ModelOutputTruncatedReason,
		agentschema.ModelContextWindowExceededReason,
		agentschema.ModelOutputFilteredReason,
		agentschema.ModelOutputIncompleteReason,
	} {
		if !IsModelIncompleteTerminalReason(reason) {
			t.Fatalf("terminal reason %q was not recognized", reason)
		}
	}
	if IsModelIncompleteTerminalReason("provider failed") {
		t.Fatal("ordinary provider error was classified as an incomplete terminal reason")
	}
}
