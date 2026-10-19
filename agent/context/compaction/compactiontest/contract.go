// Package compactiontest provides reusable behavioral checks for Compaction
// Manager implementations.
package compactiontest

import (
	"context"
	"reflect"
	"strings"
	"testing"

	agentcompaction "github.com/alfredxw/denova/agent/context/compaction"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type Factory func(testing.TB) agentcompaction.CompactionManager

func RunManagerContract(t *testing.T, factory Factory) {
	t.Helper()
	manager := factory(t)
	identity := manager.Identity()
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		t.Fatalf("identity = %#v", identity)
	}
	if manager.SummaryLimitBytes() <= 0 {
		t.Fatalf("summary limit = %d", manager.SummaryLimitBytes())
	}
	messages := []*agentschema.Message{
		agentschema.UserMessage(strings.Repeat("old request ", 32)),
		agentschema.AssistantMessage(strings.Repeat("old answer ", 32), nil),
		agentschema.UserMessage("recent request"),
	}
	modelRequest := append([]*agentschema.Message{agentschema.SystemMessage("stable instructions")}, messages...)
	modelSnapshot := (&agentmodel.ModelCall{
		Messages: modelRequest, Options: []agentmodel.ModelOption{agentmodel.WithSessionKey("contract-session")}, Streaming: true,
	}).Snapshot()
	plan, err := manager.Plan(context.Background(), agentcompaction.CompactionPlanRequest{
		Groups: []agentcompaction.CompactionGroup{{Messages: clone(messages[:2])}}, ModelSnapshot: modelSnapshot, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	switch plan.Action {
	case agentcompaction.CompactionNone:
		return
	case agentcompaction.CompactionCreate:
		if plan.GroupCount != 1 {
			t.Fatalf("plan = %#v", plan)
		}
	default:
		t.Fatalf("unsupported action %q", plan.Action)
	}
	sourceMessages := clone(messages[:2])
	probe := &contractManager{CompactionManager: manager}
	checkpoint, err := probe.Compact(context.Background(), agentcompaction.CompactionCompactRequest{
		Messages:      sourceMessages,
		ModelSnapshot: modelSnapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(checkpoint.Summary) == "" || len(checkpoint.Summary) > manager.SummaryLimitBytes() {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
	if probe.request.ModelSnapshot != modelSnapshot ||
		!reflect.DeepEqual(probe.request.Messages, sourceMessages) {
		t.Fatalf("Compaction contract lost exact ModelSnapshot or SourceMessages: %#v", probe.request)
	}
}

type contractManager struct {
	agentcompaction.CompactionManager
	request agentcompaction.CompactionCompactRequest
}

func (manager *contractManager) Compact(ctx context.Context, request agentcompaction.CompactionCompactRequest) (agentcompaction.CompactionCheckpoint, error) {
	manager.request = request
	return manager.CompactionManager.Compact(ctx, request)
}

func clone(messages []*agentschema.Message) []*agentschema.Message {
	result := make([]*agentschema.Message, len(messages))
	for index, message := range messages {
		result[index] = message.Clone()
	}
	return result
}
