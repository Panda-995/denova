package interactiveapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"denova/config"
	"denova/internal/agents/decision"
	"denova/internal/interactive"
)

func TestHostStageSelectionRunsOnceAfterReadyAndSurvivesColdReplay(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "decision"
		if fail {
			name = "both-services-unavailable"
		}
		t.Run(name, func(t *testing.T) {
			var decisions atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/systemone" {
					http.Error(w, "offline", 400)
					return
				}
				decisions.Add(1)
				if fail {
					http.Error(w, "offline", 503)
					return
				}
				var request struct {
					State struct {
						Narrative string `json:"completed_prose"`
					} `json:"state"`
					Questions map[string]decision.Question `json:"questions"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.State.Narrative != "We arrive at the target." {
					t.Errorf("selection missed completed prose: %s", request.State.Narrative)
				}
				choice := ""
				for key, description := range request.Questions["background"].Criteria {
					if strings.Contains(description, "target") {
						choice = key
						break
					}
				}
				if choice == "" {
					t.Error("target material missing")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"background": map[string]any{
					"type": "choice", "choice": choice, "confidence": .99, "probabilities": map[string]float64{choice: 1},
				}}})
			}))
			defer server.Close()
			workspace := t.TempDir()
			parent := presentationLoreFixture(t, workspace, "parent")
			target := presentationLoreFixture(t, workspace, "target")
			store := interactive.NewStore(workspace)
			defer store.Close()
			story, err := store.CreateStory(interactive.CreateStoryRequest{Title: "Decision stage", PlanningMode: interactive.StoryPlanningModeDisabled,
				PresentationSettings: &interactive.StoryPresentationSettings{Background: true, DefaultBackground: &interactive.PresentationMaterial{ItemID: "parent", AssetID: parent.ID}}})
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{Workspace: workspace,
				Decision: &config.DecisionSettings{
					Enabled: true, DefaultProfileID: "test", GamePresentation: true,
					Endpoints: []config.DecisionEndpointSettings{{ID: "local", Endpoint: server.URL + "/v1/systemone"}},
					Profiles:  []config.DecisionProfileSettings{{ID: "test", EndpointID: "local", Model: "test"}},
				},
				ModelEndpoints: []config.ModelEndpointSettings{{ID: "test", Provider: "openai-compatible", Protocol: "openai-chat-completions", BaseURL: server.URL + "/v1", APIKey: "test"}},
				ModelProfiles:  []config.ModelProfileSettings{{ID: "test", EndpointID: "test", Model: "test"}},
				AgentModels:    config.AgentModelSettings{Default: config.AgentModelOverride{ProfileID: "test"}},
			}
			c := NewConversation(store, t.TempDir(), workspace, story.ID, "main", "Travel", 800, cfg)
			bindInteractiveCycleForTest(t, c)
			messages, err := assembleAndCommitInteractiveContextForTest(c, "Travel", "Travel")
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range messages {
				if strings.Contains(message.Content, "Enabled Lore media catalog") {
					t.Fatal("host selection still injects the media catalog into the narrative model")
				}
			}
			if err := c.AcceptNarrativeCandidate(t.Context(), "We arrive at the target."); err != nil {
				t.Fatal(err)
			}
			partial := interactive.DecodeInteractiveTurnSubmissionInput(gameStateArgs)
			receipt, err := c.SubmitTurnResult(t.Context(), partial)
			if err != nil || receipt.Ready || decisions.Load() != 0 {
				t.Fatalf("selection ran before ready: %+v %v", receipt, err)
			}
			complete := interactive.DecodeInteractiveTurnSubmissionInput(`{"choices":["Enter","Observe","Listen","Inspect","Wait"],"presentation":{"background":null}}`)
			receipt, err = c.SubmitTurnResult(t.Context(), complete)
			if err != nil || !receipt.Ready || decisions.Load() != 1 {
				t.Fatalf("final selection: %+v %v calls=%d", receipt, err, decisions.Load())
			}
			if _, err := c.SubmitTurnResult(t.Context(), complete); err != nil || decisions.Load() != 1 {
				t.Fatal("duplicate submission reran selection")
			}
			if err := commitInteractiveAssistantForTest(t, c, "We arrive at the target.", ""); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = interactive.NewStore(workspace)
			defer store.Close()
			snapshot, err := store.Snapshot(story.ID, "main")
			if err != nil {
				t.Fatal(err)
			}
			want := target.ID
			if fail {
				want = parent.ID
			}
			stage := snapshot.CurrentTurn.TurnResult.Presentation
			if stage == nil || stage.Background == nil || stage.Background.AssetID != want {
				t.Fatalf("journal lost selected/retained stage: %+v", stage)
			}
		})
	}
}
