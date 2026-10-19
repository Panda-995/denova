package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"denova/internal/agents/canonicalstore"
	agentrun "denova/internal/agents/run"
	productsession "denova/internal/agents/session"
	"denova/internal/project"

	"github.com/alfredxw/denova/agent"
	agentsession "github.com/alfredxw/denova/agent/session"
)

type statusCountingStore struct {
	agentsession.Store
	lists, readers, writers int
}

func (store *statusCountingStore) List(ctx context.Context, selector agentsession.Selector) ([]agentsession.Key, error) {
	store.lists++
	return store.Store.List(ctx, selector)
}
func (store *statusCountingStore) Open(ctx context.Context, key agentsession.Key) (agentsession.Log, error) {
	store.writers++
	return store.Store.Open(ctx, key)
}
func (store *statusCountingStore) OpenReader(ctx context.Context, key agentsession.Key) (agentsession.Log, error) {
	store.readers++
	return store.Store.(agentsession.ReaderStore).OpenReader(ctx, key)
}

// Exercise the real product journal and file manifest catalog. Historical
// tasks must not change the number of I/O operations or acquire execution.
func TestStatusReadsOnlyPendingChildren(t *testing.T) {
	for _, historical := range []int{20, 300} {
		t.Run(fmt.Sprint(historical), func(t *testing.T) {
			ctx := t.Context()
			data, workspace := t.TempDir(), t.TempDir()
			registry := project.NewRegistry(data)
			projectRecord, err := registry.Add(workspace, project.TypeGeneral, "Status recovery")
			if err != nil {
				t.Fatal(err)
			}
			layout, err := registry.EnsureStore(projectRecord)
			if err != nil {
				t.Fatal(err)
			}
			products, err := productsession.NewStore(layout.SessionsDir())
			if err != nil {
				t.Fatal(err)
			}
			root, err := products.GetOrCreate("status-root")
			if err != nil {
				t.Fatal(err)
			}
			if err := products.Close(); err != nil {
				t.Fatal(err)
			}
			binding := agentrun.RuntimeBinding{AgentKind: agentrun.AgentKindIDE, ProjectID: projectRecord.ID, SessionID: root.ID}
			key, err := binding.AgentSessionKey()
			if err != nil {
				t.Fatal(err)
			}
			store, err := canonicalstore.New(data, registry)
			if err != nil {
				t.Fatal(err)
			}
			attributes, err := agent.ChildSessionAttributes(key)
			if err != nil {
				t.Fatal(err)
			}
			pending := make(map[string]agent.ChildTask)
			for index := range historical + 2 {
				child := agentsession.Key{Namespace: "task.status", ID: fmt.Sprintf("child-%d", index), Attributes: attributes}
				log, err := store.Open(ctx, child)
				if err != nil {
					t.Fatal(err)
				}
				if err := log.Close(); err != nil {
					t.Fatal(err)
				}
				if index < 2 {
					canonical, _ := agentsession.CanonicalKey(child)
					pending[canonical] = agent.ChildTask{Key: child}
				}
			}
			log, err := store.Open(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint, err := json.Marshal(map[string]any{"initialized": true, "pending": pending})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := log.Append(ctx, 0, agentsession.Record{Kind: "session.child_tasks", Version: 1, Data: checkpoint}); err != nil {
				t.Fatal(err)
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			counted := &statusCountingStore{Store: store}
			owner, err := agent.New(ctx, agent.SourceFunc(func(context.Context, agent.PrepareRequest) (agent.Definition, error) {
				t.Error("status inspection prepared execution")
				return agent.Definition{}, fmt.Errorf("unexpected execution")
			}), agent.WithSessionStore(counted))
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close(context.Background())
			backend := &publicBackend{agent: owner}
			options := agentrun.Options{AgentKind: binding.AgentKind, ProjectID: binding.ProjectID, SessionID: root.ID, Workspace: workspace}
			for query := range 4 {
				counted.lists, counted.readers, counted.writers = 0, 0, 0
				started := time.Now()
				status, err := backend.status(ctx, options)
				if err != nil {
					t.Fatal(err)
				}
				if status.Phase != agentrun.RunPhaseIdle || counted.lists != 0 || counted.writers != 0 || counted.readers != 3 {
					t.Fatalf("phase=%s lists=%d readers=%d writers=%d", status.Phase, counted.lists, counted.readers, counted.writers)
				}
				t.Logf("historical=%d query=%d elapsed=%s exact_readers=%d catalog_scans=%d", historical, query, time.Since(started), counted.readers, counted.lists)
			}
		})
	}
}
