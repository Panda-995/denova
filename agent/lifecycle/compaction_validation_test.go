package lifecycle

import (
	"context"
	"strings"
	"testing"

	agentcompaction "github.com/alfredxw/denova/agent/context/compaction"
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentengine "github.com/alfredxw/denova/agent/engine"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

type expandingCompactionManager struct{}

func (expandingCompactionManager) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "compaction.expanding-post-validation-test", Version: 1}
}

func (expandingCompactionManager) SummaryLimitBytes() int { return 64 << 10 }

func (expandingCompactionManager) Plan(_ context.Context, request agentcompaction.CompactionPlanRequest) (agenthistory.CompactionPlan, error) {
	if len(request.Groups) == 0 {
		return agenthistory.CompactionPlan{Action: agenthistory.CompactionNone}, nil
	}
	return agenthistory.CompactionPlan{
		Action: agenthistory.CompactionCreate, GroupCount: 1,
		Validation: agenthistory.CompactionValidationPolicy{HardLimitBytes: 8 << 20},
	}, nil
}

func (expandingCompactionManager) Compact(context.Context, agentcompaction.CompactionCompactRequest) (agentcompaction.CompactionCheckpoint, error) {
	return agentcompaction.CompactionCheckpoint{Summary: strings.Repeat("expanded checkpoint ", 500)}, nil
}

func TestAutomaticAndManualCompactionRejectUnpublishablePostProjection(t *testing.T) {
	model := &lifecycleModel{responses: []*agentschema.Message{
		agentschema.AssistantMessage("first answer", nil), agentschema.AssistantMessage("second answer", nil), agentschema.AssistantMessage("third answer", nil),
	}}
	owner, err := New(context.Background(), agentengine.Definition{Model: model, Compaction: expandingCompactionManager{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err := owner.Session(context.Background(), agentsession.Named("post-compaction-validation"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := session.Run(context.Background(), agentschema.Input{Text: "first", IdempotencyKey: "post-validation-first"})
	if err != nil {
		t.Fatal(err)
	}
	if result, waitErr := first.Wait(context.Background()); waitErr != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("first=%#v err=%v", result, waitErr)
	}
	second, err := session.Run(context.Background(), agentschema.Input{Text: "second", IdempotencyKey: "post-validation-second"})
	if err != nil {
		t.Fatal(err)
	}
	if result, waitErr := second.Wait(context.Background()); waitErr != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("second=%#v err=%v", result, waitErr)
	}
	third, err := session.Run(context.Background(), agentschema.Input{Text: "third", IdempotencyKey: "post-validation-third"})
	if err != nil {
		t.Fatal(err)
	}
	if result, waitErr := third.Wait(context.Background()); waitErr != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("third=%#v err=%v", result, waitErr)
	}
	foundFailure := false
	for event := range third.Events() {
		if failure, ok := event.Payload.(agentevent.CompactionFailed); ok {
			foundFailure = true
			if failure.Metrics.ProjectedTokensAfter <= failure.Metrics.ProjectedTokensBefore {
				t.Fatalf("failure metrics did not describe rejected post-projection: %#v", failure)
			}
		}
	}
	if !foundFailure || len(model.calls()) != 3 {
		t.Fatalf("automatic failure=%v provider calls=%d", foundFailure, len(model.calls()))
	}
	if state, present, stateErr := session.compactionState(context.Background()); stateErr != nil || present {
		t.Fatalf("rejected automatic Compaction became durable: %#v present=%v err=%v", state, present, stateErr)
	}

	if result, compactErr := session.Compact(context.Background(), agentcompaction.CompactionRequest{
		Force: true, IdempotencyKey: "post-validation-manual",
	}); compactErr == nil || result.Changed || !strings.Contains(compactErr.Error(), "no progress") {
		t.Fatalf("manual rejected Compaction=%#v err=%v", result, compactErr)
	}
	if state, present, stateErr := session.compactionState(context.Background()); stateErr != nil || present {
		t.Fatalf("rejected manual Compaction became durable: %#v present=%v err=%v", state, present, stateErr)
	}
}
