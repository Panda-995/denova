package agentchat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"denova/config"
	agents "denova/internal/agents"
	agentcompaction "denova/internal/agents/context/compaction"
	"denova/internal/agents/conversationconfig"
	agentexecution "denova/internal/agents/execution"
	agentruntime "denova/internal/agents/runtime"
	"denova/internal/agents/session"
	apptask "denova/internal/app/task"
	projectdomain "denova/internal/project"
)

type compactionLifecycleHost struct {
	activeViewTestHost
	prepare func(context.Context) error
	engines *agentruntime.Engines
}

func (host compactionLifecycleHost) AgentEngines() *agentruntime.Engines { return host.engines }

func (host compactionLifecycleHost) ProjectAgentHostCapabilities(ctx context.Context, _ projectdomain.Type, _ *config.Config, _ string) (agents.AgentHostCapabilities, error) {
	return agents.AgentHostCapabilities{}, host.prepare(ctx)
}

// Failures after admission must finish the Task and release the same binding.
// The Host seam also verifies panic recovery without a production test hook.
func TestCompactionLifecycleReleasesConversation(t *testing.T) {
	for _, exit := range []string{"error", "panic", "cancel", "shutdown", "external_locked"} {
		t.Run(exit, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			failure := errors.New("compaction preparation failed")
			host := compactionLifecycleHost{prepare: func(ctx context.Context) error {
				close(started)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-release:
					if exit == "panic" {
						panic("compaction preparation panicked")
					}
					return failure
				}
			}, engines: agentruntime.NewEngines()}
			t.Cleanup(func() { _ = host.engines.Close() })
			root, workspace := t.TempDir(), t.TempDir()
			registry := projectdomain.NewRegistry(root)
			record, err := registry.Add(workspace, projectdomain.TypeGeneral, "")
			if err != nil {
				t.Fatal(err)
			}
			layout, err := registry.EnsureStore(record)
			if err != nil {
				t.Fatal(err)
			}
			store, err := session.NewStore(layout.SessionsDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			conversation, err := store.GetOrCreate("compacting")
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(host, registry)
			service.projects[record.ID] = &projectRuntime{
				projectID: record.ID, projectType: record.Type, agentKind: "general",
				stateRoot: layout.StoreRoot, workspace: workspace, store: store,
				cfg: config.Config{NovaDir: root, OpenAIModel: "fixture-model"},
			}
			binding := Binding{ProjectID: record.ID, SessionID: conversation.ID}
			if exit == "external_locked" {
				seed := conversationconfig.Config{AgentKind: config.AgentKindGeneral, ProfileID: "default",
					Runtime: &config.RuntimeSelection{Kind: config.RuntimeCodex, Codex: &config.CodexRuntimeSettings{Model: "fixture-external"}}}
				if _, err := conversation.EnsureRuntimeConfig(seed); err != nil {
					t.Fatal(err)
				}
				resolved, err := service.ResolveBinding(binding)
				if err != nil {
					t.Fatal(err)
				}
				state, err := agentruntime.SessionState(runtimeOptions(resolved, ""), conversation)
				if err != nil {
					t.Fatal(err)
				}
				control, err := host.engines.ExternalControl(runtimeOptions(resolved, ""), state)
				if err != nil {
					t.Fatal(err)
				}
				prepare := host.prepare
				host.prepare = func(ctx context.Context) error {
					_, err := control.Maintain(ctx, "external-maintenance", func() (agentcompaction.Result, error) {
						return agentcompaction.Result{}, prepare(ctx)
					})
					return err
				}
				service.host = host
			}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			compacted := make(chan error, 1)
			go func() {
				defer func() {
					if value := recover(); value != nil {
						t.Errorf("compaction panic escaped: %v", value)
						compacted <- failure
					}
				}()
				_, err := service.CompactContext(ctx, binding, "compact-lifecycle")
				compacted <- err
			}()
			select {
			case <-started:
			case err := <-compacted:
				t.Fatalf("compaction did not reach preparation: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("compaction preparation did not start")
			}
			active := service.activeRun(binding)
			if active == nil || active.kind != compactionRun || active.task.Finished() {
				t.Fatal("compaction did not reserve the conversation")
			}
			if service.DisplayTask(binding, active.task.ID()) != nil {
				t.Fatal("internal maintenance task became a reconnectable chat stream")
			}
			observed := make(chan error, 1)
			go func() {
				defer func() {
					if value := recover(); value != nil {
						observed <- errors.New("maintenance observation panicked")
					}
				}()
				view := service.ActiveView(ctx, binding)
				if view.Task != nil || view.RuntimeProjectionOK || !service.SessionBusy(binding) {
					observed <- errors.New("maintenance observation exposed a user operation or lost occupancy")
					return
				}
				_, err := service.SubmitCommand(ctx, binding, agentruntime.Command{Kind: agentexecution.CommandSuspend, CommandID: "conflicting-suspend", OperationID: "old-operation"})
				observed <- err
			}()
			select {
			case err := <-observed:
				if !errors.Is(err, agentruntime.ErrOperationActive) {
					t.Fatalf("maintenance control = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("maintenance observation or control waited for the model lock")
			}
			switch exit {
			case "cancel":
				cancel()
			case "shutdown":
				closed := make(chan struct{})
				go func() {
					service.Close(ctx)
					close(closed)
				}()
				select {
				case <-closed:
				case <-time.After(3 * time.Second):
					t.Fatal("shutdown waited for the compaction barrier")
				}
			default:
				close(release)
			}
			select {
			case err := <-compacted:
				if err == nil || (exit == "error" && !errors.Is(err, failure)) ||
					(exit == "panic" && !strings.Contains(err.Error(), "panicked")) ||
					((exit == "cancel" || exit == "shutdown") && !errors.Is(err, context.Canceled)) {
					t.Fatalf("compaction error = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("compaction did not settle")
			}
			if service.SessionBusy(binding) || !active.task.Finished() {
				t.Fatal("compaction retained occupancy or an unfinished Task")
			}
			wantStatus := apptask.Failed
			if exit == "cancel" || exit == "shutdown" {
				wantStatus = apptask.Aborted
			}
			if active.task.Status() != wantStatus {
				t.Fatalf("compaction task status = %q, want %q", active.task.Status(), wantStatus)
			}
			if exit != "shutdown" {
				if _, err := service.PatchConversationConfig(t.Context(), binding, conversationconfig.Patch{}, 1); err != nil {
					t.Fatalf("conversation remained fenced after compaction: %v", err)
				}
			}
		})
	}
}
