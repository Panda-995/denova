package integration_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alfredxw/denova/agent"
	agentcontext "github.com/alfredxw/denova/agent/context"
	"github.com/alfredxw/denova/agent/context/compaction"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	"github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
	"github.com/alfredxw/denova/agent/tool/permission"
)

const continuationEvidence = "LIVE_TOOL_EVIDENCE_72519"

type continuationContextKey struct{}

type continuationModel struct {
	inputs         [][]*agentschema.Message
	requireContext bool
	postCompaction chan struct{}
}

func (model *continuationModel) Generate(ctx context.Context, messages []*agentschema.Message, _ ...agentmodel.ModelOption) (*agentschema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if model.requireContext && ctx.Value(continuationContextKey{}) != "active invocation" {
		return nil, errors.New("model lost the active middleware context")
	}
	model.inputs = append(model.inputs, messages)
	if len(model.inputs) == 1 {
		return agentschema.AssistantMessage("Reading current evidence", []agentschema.ToolCall{{
			ID: "live-read", Type: "function", Function: agentschema.FunctionCall{Name: "read_live", Arguments: `{}`},
		}}), nil
	}
	if len(model.inputs) == 2 && model.postCompaction != nil {
		close(model.postCompaction)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return agentschema.AssistantMessage("Completed", nil), nil
}

func (model *continuationModel) Stream(ctx context.Context, messages []*agentschema.Message, options ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	result, err := model.Generate(ctx, messages, options...)
	return agentstream.StreamReaderFromArray([]*agentschema.Message{result}), err
}

type continuationContext struct{ unavailable bool }

func (*continuationContext) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.continuation-context", Version: 1}
}

func (source *continuationContext) Materialize(_ context.Context, request agentcontext.ContextRequest) ([]agentschema.ContextFragment, error) {
	if source.unavailable {
		return nil, errors.New("project instructions are unavailable after reopen")
	}
	content := "Original context before compaction"
	if request.Compaction != nil {
		content = "Accepted context after compaction"
	}
	return []agentschema.ContextFragment{{Source: "project", Purpose: "compaction context projection", Resource: "AGENTS.md",
		Placement: agentschema.ContextLeadingMessage, Stability: agentschema.ContextStablePrefix, Content: content, HardLimit: 1024}}, nil
}

type continuationPreparation struct {
	agentmiddleware.BaseMiddleware
	checkpoints      [][]*agentschema.Message
	iterations       []int
	beforeAgentCalls int
	failure          string
}

func (middleware *continuationPreparation) BeforeAgent(ctx context.Context, run *agentmiddleware.RunContext) (context.Context, *agentmiddleware.RunContext, error) {
	middleware.beforeAgentCalls++
	run.Instruction = "Instruction resolved by BeforeAgent"
	return context.WithValue(ctx, continuationContextKey{}, "active invocation"), run, nil
}

func (middleware *continuationPreparation) BeforeModelCall(ctx context.Context, call *agentmodel.ModelCall, metadata *agentmiddleware.ModelContext) (context.Context, *agentmodel.ModelCall, error) {
	for _, message := range call.Messages {
		if strings.Contains(message.Content, "Old history checkpoint") {
			call.Messages = append(call.Messages, agentschema.UserMessage(fmt.Sprintf("CHECKPOINT_PREPARATION_%d", len(middleware.checkpoints)+1)))
			middleware.checkpoints = append(middleware.checkpoints, call.Snapshot().Messages())
			middleware.iterations = append(middleware.iterations, metadata.Iteration)
			if middleware.failure == "preparation" {
				return ctx, nil, errors.New("injected candidate preparation failure")
			}
			if middleware.failure == "validation" {
				call.Messages = append(call.Messages, agentschema.UserMessage(strings.Repeat("oversized candidate ", 8000)))
			}
			break
		}
	}
	return ctx, call, nil
}

// Exercise the public Session API, real tool execution, the standard planner,
// projection validation and journal recovery. Only model text is deterministic.
func TestCompactionPreservesLiveToolTailAndExecutesValidatedRequest(t *testing.T) {
	for _, failure := range []string{"", "preparation", "validation", "abort", "resume", "post_compaction_resume"} {
		name := failure
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) { testCompactionContinuation(t, failure) })
	}
}

func testCompactionContinuation(t *testing.T, failure string) {
	ctx := context.Background()
	model := &continuationModel{requireContext: true}
	middleware := &continuationPreparation{failure: failure}
	toolCalls := 0
	tool, err := agenttool.InferTool("read_live", "Read current evidence", func(context.Context, struct{}) (string, error) {
		toolCalls++
		return strings.Repeat(continuationEvidence+" ", 1000), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := agenttool.StaticToolsIdentified(agentschema.CapabilityIdentity{Kind: "test.continuation-tools", Version: 1}, agenttool.ToolDefinition{
		Tool: tool, Descriptor: agenttool.ToolDescriptor{
			Source: agenttool.ToolSourceRead, Execution: agenttool.ToolExecutionParallelRead,
			MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone,
			Recovery: agenttool.ToolRecoveryReadOnly, ResultProjection: agentschema.ToolResultBoundedModelContext,
			ResultRetention: agentschema.ToolResultProtected, Steering: agenttool.SteeringFinishCurrent, MaxResultBytes: 64 << 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	summaryCalls := 0
	summaryStarted := make(chan struct{})
	manager := compaction.Standard(compaction.StandardConfig{
		Summarizer: compaction.SummarizerFunc{
			Capability: agentschema.CapabilityIdentity{Kind: "test.continuation-summary", Version: 1},
			Func: func(ctx context.Context, request compaction.SummaryRequest) (compaction.CompactionCheckpoint, error) {
				summaryCalls++
				for _, message := range request.Messages {
					if strings.Contains(message.Content, continuationEvidence) {
						t.Error("active tool result entered the old-history summary source")
					}
				}
				if failure == "abort" || failure == "resume" && summaryCalls == 1 {
					close(summaryStarted)
					<-ctx.Done()
					return compaction.CompactionCheckpoint{}, ctx.Err()
				}
				return compaction.CompactionCheckpoint{Summary: "Old history checkpoint"}, nil
			},
		},
		TriggerBytes: 12_000, KeepRecentBytes: 100, HardLimitBytes: 1 << 20, SummaryLimitBytes: 8192,
	})
	store := session.Memory()
	definition := agent.Definition{
		Name: "continuation", Model: model, Tools: tools, Permission: permission.FullAccess(),
		Compaction: manager, Middlewares: []agentmiddleware.Middleware{middleware},
	}
	contextSource := &continuationContext{}
	if failure == "post_compaction_resume" {
		definition.Context = contextSource
		model.postCompaction = make(chan struct{})
	}
	owner, err := agent.New(ctx, definition, agent.WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(ctx) })
	conversation, err := owner.Session(ctx, session.Named("active-turn"))
	if err != nil {
		t.Fatal(err)
	}
	raw := []*agentschema.Message{
		agentschema.UserMessage(strings.Repeat("Historical instructions. ", 180)), agentschema.AssistantMessage("Old answer", nil),
		agentschema.UserMessage("Recent request"), agentschema.AssistantMessage("Recent answer", nil),
	}
	if err := conversation.LoadCanonicalMessages(ctx, raw); err != nil {
		t.Fatal(err)
	}
	run, err := conversation.Run(ctx, agent.Text("Use the current tool evidence"))
	if err != nil {
		t.Fatal(err)
	}
	status := agentschema.ResultCompleted
	if failure == "abort" || failure == "resume" || failure == "post_compaction_resume" {
		pausePoint := summaryStarted
		if failure == "post_compaction_resume" {
			pausePoint = model.postCompaction
		}
		select {
		case <-pausePoint:
		case <-time.After(2 * time.Second):
			t.Fatal("summary did not start")
		}
		if failure == "abort" {
			if _, err := run.Abort(ctx, agentevent.AbortRequest{Reason: "Cancel during compaction"}); err != nil {
				t.Fatal(err)
			}
			status = agentschema.ResultAborted
		} else {
			runID := run.ID()
			if _, err := conversation.SuspendAndClose(ctx, agent.SuspendRequest{RunID: runID, IdempotencyKey: "pause-summary"}); err != nil {
				t.Fatal(err)
			}
			if err := owner.Close(ctx); err != nil {
				t.Fatal(err)
			}
			contextSource.unavailable = true
			owner, err = agent.New(ctx, definition, agent.WithSessionStore(store))
			if err != nil {
				t.Fatal(err)
			}
			conversation, err = owner.Session(ctx, session.Named("active-turn"))
			if err != nil {
				t.Fatal(err)
			}
			run, err = conversation.ResumeRun(ctx, agent.ResumeRequest{RunID: runID, IdempotencyKey: "resume-summary"})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	result, err := run.Wait(ctx)
	if err != nil || result.Status != status {
		t.Fatalf("run: result=%+v error=%v", result, err)
	}
	snapshot, err := conversation.Snapshot(ctx)
	wantSummaryCalls, wantBeforeAgent, wantIteration := 1, 1, 1
	if failure == "resume" {
		wantSummaryCalls, wantBeforeAgent, wantIteration = 2, 2, 0
	}
	wantIterations := []int{wantIteration}
	wantCheckpoints := 1
	if failure == "post_compaction_resume" {
		wantBeforeAgent, wantCheckpoints = 2, 2
		wantIterations = []int{1, 0}
	}
	compacted := failure == "" || failure == "resume" || failure == "post_compaction_resume"
	if err != nil || summaryCalls != wantSummaryCalls || (snapshot.Compaction != nil) != compacted {
		t.Fatalf("compaction: snapshot=%+v calls=%d error=%v", snapshot, summaryCalls, err)
	}
	if toolCalls != 1 {
		t.Fatalf("live tool executed %d times", toolCalls)
	}
	if middleware.beforeAgentCalls != wantBeforeAgent || failure != "abort" && !reflect.DeepEqual(middleware.iterations, wantIterations) {
		t.Fatalf("candidate left the current invocation: BeforeAgent=%d iterations=%v", middleware.beforeAgentCalls, middleware.iterations)
	}
	last := model.inputs[len(model.inputs)-1]
	if failure == "post_compaction_resume" {
		var found bool
		for _, message := range last {
			found = found || strings.Contains(message.Content, "Accepted context after compaction")
			if strings.Contains(message.Content, "Original context before compaction") {
				t.Fatal("resume restored the obsolete pre-compaction context")
			}
		}
		if !found {
			t.Fatal("resume lost the accepted post-compaction context")
		}
	}
	var liveResult *agentschema.Message
	if failure == "abort" {
		if len(model.inputs) != 1 || len(middleware.checkpoints) != 0 {
			t.Fatal("aborted compaction continued preparing or calling the model")
		}
		liveResult = &agentschema.Message{Role: agentschema.ToolRole, ToolCallID: "live-read", Content: strings.Repeat(continuationEvidence+" ", 1000)}
	}
	for _, message := range last {
		if message.Role == agentschema.ToolRole && strings.Contains(message.Content, continuationEvidence) {
			liveResult = message
		}
	}
	if liveResult == nil {
		t.Fatal("post-compaction provider request lost the unselected live tool result")
	}
	if failure != "abort" && len(middleware.checkpoints) != wantCheckpoints {
		t.Fatalf("candidate prepared %d times", len(middleware.checkpoints))
	}
	if compacted {
		if !reflect.DeepEqual(last, middleware.checkpoints[len(middleware.checkpoints)-1]) {
			t.Fatal("provider did not use the validated request")
		}
	} else if failure != "abort" {
		var historical bool
		for _, message := range last {
			historical = historical || strings.Contains(message.Content, "Historical instructions.")
			if strings.Contains(message.Content, "Old history checkpoint") {
				t.Fatal("unpublished candidate reached the provider")
			}
		}
		if !historical {
			t.Fatal("failed candidate replaced the original history")
		}
	}
	if err := owner.Close(ctx); err != nil {
		t.Fatal(err)
	}
	model.requireContext = false
	owner, err = agent.New(ctx, agent.Definition{
		Name: "continuation", Model: model, Compaction: compaction.Disabled(1<<20, 8192),
	}, agent.WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err = owner.Session(ctx, session.Named("active-turn"))
	if err != nil {
		t.Fatal(err)
	}
	run, err = conversation.Run(ctx, agent.Text("Continue after reopen"))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("reopen: result=%+v error=%v", result, err)
	}
	for _, message := range model.inputs[len(model.inputs)-1] {
		if message.Role == agentschema.ToolRole && message.ToolCallID == liveResult.ToolCallID && message.Content == liveResult.Content {
			return
		}
	}
	t.Fatal("reopen did not retain the canonical live tool result")
}
