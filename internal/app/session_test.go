package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"denova/config"
	"denova/internal/agents"
	agentattachment "denova/internal/agents/attachment"
	"denova/internal/agents/canonicalstore"
	agentchat "denova/internal/agents/chat"
	agentexecution "denova/internal/agents/execution"
	agentrun "denova/internal/agents/run"
	"denova/internal/agents/session"
	"denova/internal/agents/sessionjournal"
	agentchatapp "denova/internal/app/agentchat"

	"github.com/alfredxw/denova/agent"
	agentsession "github.com/alfredxw/denova/agent/session"
)

func TestAgentChatConcurrentDeletesPreserveTheRemainingConversation(t *testing.T) {
	application := newExecutionProfileTestApp(t)
	service := application.AgentChat()
	project, err := service.AddProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	layout, err := application.projectRegistry.Layout(project)
	if err != nil {
		t.Fatal(err)
	}
	before := make(map[string][]byte)
	for _, title := range []string{"First", "Second"} {
		conversation, err := service.CreateSession(project.ID, title, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.MutateConversationGoal(context.Background(), agentchatapp.Binding{
			ProjectID: project.ID, SessionID: conversation.ID,
		}, "set", "Preserve "+title, 0); err != nil {
			t.Fatal(err)
		}
		before[conversation.ID], err = os.ReadFile(filepath.Join(layout.SessionsDir(), conversation.ID+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
	}
	type deletion struct {
		id  string
		err error
	}
	start := make(chan struct{})
	results := make(chan deletion, len(before))
	for id := range before {
		go func(id string) {
			result := deletion{id: id}
			defer func() {
				if value := recover(); value != nil {
					result.err = fmt.Errorf("concurrent conversation deletion panicked: %v", value)
				}
				results <- result
			}()
			<-start
			result.err = service.DeleteSession(project.ID, id)
		}(id)
	}
	close(start)
	deleted := 0
	for range before {
		result := <-results
		if result.err == nil {
			deleted++
			continue
		}
		if !errors.Is(result.err, session.ErrOnlySession) {
			t.Fatal(result.err)
		}
		after, err := os.ReadFile(filepath.Join(layout.SessionsDir(), result.id+".jsonl"))
		if err != nil || !bytes.Equal(before[result.id], after) {
			t.Fatalf("concurrent deletion changed the remaining conversation: %v", err)
		}
	}
	if deleted != 1 {
		t.Fatalf("deleted %d conversations, want exactly one", deleted)
	}
}

func TestAgentChatDeleteSessionPreservesRejectedJournalAndCleansAcceptedTree(t *testing.T) {
	for _, test := range []struct {
		name        string
		warm        bool
		accept      bool
		denyJournal bool
	}{
		{name: "reject cold"},
		{name: "reject warm", warm: true},
		{name: "delete cold", accept: true},
		{name: "delete warm", warm: true, accept: true},
		{name: "reject journal removal cold", denyJournal: true},
		{name: "reject journal removal warm", warm: true, denyJournal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.denyJournal && runtime.GOOS == "windows" {
				t.Skip("directory permissions do not prevent deletion on Windows")
			}
			ctx := context.Background()
			application := newExecutionProfileTestApp(t)
			service := application.AgentChat()
			project, err := service.AddProject(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			target, err := service.CreateSession(project.ID, "Delete target", nil)
			if err != nil {
				t.Fatal(err)
			}
			binding := agentchatapp.Binding{ProjectID: project.ID, SessionID: target.ID}
			key, err := (agentrun.RuntimeBinding{
				AgentKind: agentrun.AgentKindGeneral, Mode: "agent_chat", ProjectID: project.ID, SessionID: target.ID,
			}).AgentSessionKey()
			if err != nil {
				t.Fatal(err)
			}
			layout, err := application.projectRegistry.Layout(project)
			if err != nil {
				t.Fatal(err)
			}
			store, err := session.NewStore(layout.SessionsDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			conversation, err := store.Get(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := conversation.Append(agents.UserMessage("Keep this conversation intact if deletion is refused.")); err != nil {
				t.Fatal(err)
			}
			goal := json.RawMessage(`{"id":"goal-one","objective":"Finish the draft","status":"active","revision":1}`)
			if err := conversation.UpdateCapabilities(ctx, key, "", func(sessionjournal.CapabilityReader) (map[string]json.RawMessage, error) {
				return map[string]json.RawMessage{"agent.goal": goal}, nil
			}); err != nil {
				t.Fatal(err)
			}
			if test.warm {
				state, present, err := service.ConversationGoal(ctx, binding)
				if err != nil || !present || state.Objective != "Finish the draft" {
					t.Fatalf("warm Goal = %#v, present=%v, err=%v", state, present, err)
				}
			}
			canonical, err := canonicalstore.New(application.cfg.NovaDir, application.projectRegistry)
			if err != nil {
				t.Fatal(err)
			}
			parent := key
			for _, id := range []string{"child", "grandchild"} {
				attributes, err := agent.ChildSessionAttributes(parent)
				if err != nil {
					t.Fatal(err)
				}
				attributes["agent"] = "researcher"
				child := agentsession.Key{Namespace: "task.researcher", ID: id, Attributes: attributes}
				log, err := canonical.Open(ctx, child)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := log.Append(ctx, 0, agentsession.Record{
					Kind: "session.capability_set", Version: 1,
					Data: json.RawMessage(`{"capability":"agent.todo","state":{"revision":1}}`),
				}); err != nil {
					t.Fatal(err)
				}
				if err := log.Close(); err != nil {
					t.Fatal(err)
				}
				parent = child
			}
			copies, err := agentattachment.Materialize(filepath.Dir(layout.SessionsDir()), agentattachment.SessionScope(target.ID), "attachment-command", []agentattachment.Upload{
				{Name: "draft.txt", DataURL: "data:text/plain;base64,ZHJhZnQ="},
			})
			if err != nil {
				t.Fatal(err)
			}
			journalPath := filepath.Join(layout.SessionsDir(), target.ID+".jsonl")
			artifactPath := journalPath + ".artifacts"
			if err := os.MkdirAll(artifactPath, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(artifactPath, "tool-output.txt"), []byte("tool output"), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			childPaths, err := filepath.Glob(filepath.Join(layout.SessionsDir(), "children", "*.jsonl"))
			if err != nil || len(childPaths) != 2 {
				t.Fatalf("child journals = %v, err=%v", childPaths, err)
			}
			childBytes := make(map[string][]byte)
			for _, path := range childPaths {
				childBytes[path], err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			var keep agentchatapp.Session
			if test.accept || test.denyJournal {
				keep, err = service.CreateSession(project.ID, "Keep", nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			if test.denyJournal {
				if err := os.Chmod(layout.SessionsDir(), 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(layout.SessionsDir(), 0o755) })
			}
			err = service.DeleteSession(project.ID, target.ID)
			if test.accept {
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range append(childPaths, journalPath, artifactPath, copies[0].RuntimePath, filepath.Join(layout.SessionsDir(), target.ID+".idx.json")) {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("deleted conversation left %s: %v", path, err)
					}
				}
				if !store.Exists(keep.ID) {
					t.Fatal("deleting one conversation removed its sibling")
				}
				return
			}
			if err == nil {
				t.Fatal("conversation deletion was not refused")
			}
			if errors.Is(err, session.ErrOnlySession) == test.denyJournal {
				t.Fatalf("deletion rejection = %v, deny journal removal = %v", err, test.denyJournal)
			}
			if test.denyJournal {
				if err := os.Chmod(layout.SessionsDir(), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			after, err := os.ReadFile(journalPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("rejected deletion changed the canonical journal: err=%v", err)
			}
			for path, before := range childBytes {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("rejected deletion changed child journal %s: %v", path, err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(layout.SessionsDir(), target.ID+".idx.json")); err != nil {
				t.Fatal(err)
			}
			reopened, err := store.Get(target.ID)
			if err != nil {
				t.Fatal(err)
			}
			recovered, present, err := reopened.LoadCapability(ctx, key, "agent.goal")
			if err != nil || !present || !bytes.Equal(goal, recovered) || len(reopened.History()) != 1 {
				t.Fatalf("cold recovery after rejected deletion = %s, present=%v, err=%v", recovered, present, err)
			}
			for _, path := range []string{artifactPath, copies[0].RuntimePath} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("rejected deletion removed %s: %v", path, err)
				}
			}
		})
	}
}

func TestWritingStartRejectsAStaleExplicitSessionBinding(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.GetOrCreate("session-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create("session-b")
	if err != nil {
		t.Fatal(err)
	}
	application := &App{sessionStore: store, session: second}

	task, startErr := application.StartTaskForSessionWithError(context.Background(), first.ID, agentchat.ChatRequest{
		CommandID: "stale-session-start",
		Message:   "continue session A",
	})
	if task != nil || !errors.Is(startErr, ErrAgentContextChanged) {
		t.Fatalf("stale explicit Session start = task=%v err=%v, want ErrAgentContextChanged", task, startErr)
	}
}

func TestAppSwitchSessionUsesCurrentSessionHistoryOnly(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{sessionStore: store}

	first, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Append(agents.UserMessage("会话 A 消息")); err != nil {
		t.Fatal(err)
	}
	app.session = first

	second, err := app.CreateSession("会话 B")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("新会话 ID 不应复用 default")
	}
	if err := second.Append(agents.UserMessage("会话 B 消息")); err != nil {
		t.Fatal(err)
	}

	history, err := app.SessionMessages("")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Content != "会话 B 消息" {
		t.Fatalf("当前历史应来自新会话: %#v", history)
	}

	if _, err := app.SwitchSession(first.ID); err != nil {
		t.Fatal(err)
	}
	history, err = app.SessionMessages("")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Content != "会话 A 消息" {
		t.Fatalf("切换后历史应来自目标会话: %#v", history)
	}
}

func TestAppDeleteActiveSessionSwitchesToRemainingSession(t *testing.T) {
	workspace := t.TempDir()
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	executionRuntime := agentexecution.NewEphemeralRuntime()
	t.Cleanup(func() { _ = executionRuntime.Close(context.Background()) })
	app := &App{
		sessionStore: store, session: first, workspace: workspace,
		executionRuntime: executionRuntime,
		cfg:              &config.Config{ProjectID: "project-test", Workspace: workspace, ProjectStoreDir: t.TempDir()},
	}
	second, err := app.CreateSession("会话 B")
	if err != nil {
		t.Fatal(err)
	}

	active, err := app.DeleteSession(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != first.ID {
		t.Fatalf("删除当前会话后应切换到剩余会话: want=%s got=%s", first.ID, active.ID)
	}
	metas, err := app.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || !metas[0].Active || metas[0].ID != first.ID {
		t.Fatalf("剩余会话列表不符合预期: %#v", metas)
	}
}

func TestAppUserSessionsPreserveAndHideLegacyConfigManagerSessions(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Append(agents.UserMessage("创作会话")); err != nil {
		t.Fatal(err)
	}
	app := &App{sessionStore: store, session: first}
	legacyFixed, err := store.GetOrCreate("config-manager-agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := legacyFixed.Append(agents.UserMessage("配置输入")); err != nil {
		t.Fatal(err)
	}
	if err := legacyFixed.Append(agents.AssistantMessage("配置输出", nil)); err != nil {
		t.Fatal(err)
	}
	scopedID := "config-manager-agent-automation-resource-daily-review-0123456789ab"
	scoped, err := store.GetOrCreate(scopedID)
	if err != nil {
		t.Fatal(err)
	}
	if err := scoped.Append(agents.UserMessage("自动化配置会话")); err != nil {
		t.Fatal(err)
	}

	metas, err := app.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].ID != first.ID {
		t.Fatalf("创作会话列表不应包含固定 Agent 会话: %#v", metas)
	}
	if _, err := app.SwitchSession("config-manager-agent"); err == nil {
		t.Fatal("创作 Agent 不应允许切换到配置管理 Agent 固定会话")
	}
	if _, err := app.SessionMessages("config-manager-agent"); err == nil {
		t.Fatal("创作会话 API 不应读取配置管理 Agent 固定会话")
	}
	if err := app.RenameSession("config-manager-agent", "误改名"); err == nil {
		t.Fatal("创作会话 API 不应重命名配置管理 Agent 固定会话")
	}
	if _, err := app.DeleteSession("config-manager-agent"); err == nil {
		t.Fatal("创作会话 API 不应删除配置管理 Agent 固定会话")
	}
	if _, err := app.SwitchSession(scopedID); err == nil {
		t.Fatal("创作 Agent 不应允许切换到配置管理 Agent scoped 会话")
	}
	if _, err := app.SessionMessages(scopedID); err == nil {
		t.Fatal("创作会话 API 不应读取配置管理 Agent scoped 会话")
	}

	history := legacyFixed.History()
	if len(history) != 2 || history[0].Content != "配置输入" || history[1].Content != "配置输出" {
		t.Fatalf("旧配置管理会话数据应原样保留在存储中: %#v", history)
	}
}

func TestActiveUserSessionOrCreateIgnoresFixedAgentActiveSession(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := store.GetOrCreate("config-manager-agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Append(agents.UserMessage("配置输入")); err != nil {
		t.Fatal(err)
	}
	if err := store.SetActiveID("config-manager-agent"); err != nil {
		t.Fatal(err)
	}

	active, err := activeUserSessionOrCreate(store, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != first.ID {
		t.Fatalf("固定 Agent 会话不应恢复为创作 Agent 当前会话: got=%s want=%s", active.ID, first.ID)
	}
	activeID, err := store.ActiveID()
	if err != nil {
		t.Fatal(err)
	}
	if activeID != first.ID {
		t.Fatalf("active_id 应被修正回创作会话: got=%s want=%s", activeID, first.ID)
	}
}
