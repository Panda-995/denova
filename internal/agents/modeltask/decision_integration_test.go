package modeltask

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"denova/config"
	"denova/internal/agents/decision"
	"denova/internal/automation"
	"denova/internal/book/lore"
)

func decisionTestConfig(endpoint string) *config.Config {
	return &config.Config{
		Decision: &config.DecisionSettings{
			Enabled: true, DefaultProfileID: "test", LoreClassification: true, SemanticTriggers: true,
			Endpoints: []config.DecisionEndpointSettings{{ID: "local", Endpoint: endpoint + "/v1/systemone", APIKey: "test-key"}},
			Profiles:  []config.DecisionProfileSettings{{ID: "test", EndpointID: "local", Model: "decision-test"}},
		},
		ModelEndpoints: []config.ModelEndpointSettings{{ID: "test", Provider: "openai-compatible", Protocol: "openai-chat-completions", BaseURL: endpoint + "/v1", APIKey: "test-key"}},
		ModelProfiles:  []config.ModelProfileSettings{{ID: "test", EndpointID: "test", Model: "language-test"}},
		AgentModels:    config.AgentModelSettings{Default: config.AgentModelOverride{ProfileID: "test"}},
		Language:       "en-US",
	}
}

func writeTestChat(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "test", "model": "language-test", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}}})
}

func TestChoiceFallbackPreservesAcceptedSiblingsAndCancellation(t *testing.T) {
	var llmCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/systemone" {
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("missing bearer authentication")
			}
			_, _ = w.Write([]byte(`{"answers":{"background":{"type":"choice","choice":"keep","confidence":0.98,"probabilities":{"keep":1}},"ambient":{"type":"choice","choice":"keep","confidence":0.1,"probabilities":{"keep":1}}}}`))
			return
		}
		llmCalls.Add(1)
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		last := request.Messages[len(request.Messages)-1].Content
		var input struct {
			Questions map[string]decision.Question `json:"questions"`
		}
		if err := json.Unmarshal([]byte(last), &input); err != nil {
			t.Error(err)
		}
		if len(input.Questions) != 1 || input.Questions["ambient"].Type != "choice" {
			t.Errorf("fallback reran accepted questions: %s", last)
		}
		// Unknown siblings and invented IDs must not overwrite accepted answers.
		writeTestChat(w, `{"answers":{"ambient":"clear","background":"invented"}}`)
	}))
	defer server.Close()
	cfg := decisionTestConfig(server.URL)
	question := decision.Choice("Choose stage media.", map[string]string{"keep": "Keep", "clear": "Clear"})
	questions := map[string]decision.Question{"background": question, "ambient": question}
	selected, err := SelectChoices(t.Context(), cfg, config.AgentKindToolAgent, "test", "Completed prose", questions, .8)
	if err != nil || selected["background"] != "keep" || selected["ambient"] != "clear" || llmCalls.Load() != 1 {
		t.Fatalf("partial fallback: %v %v", selected, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := SelectChoices(ctx, cfg, config.AgentKindToolAgent, "test", "", questions, .8); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if llmCalls.Load() != 1 {
		t.Fatal("cancel launched fallback")
	}
}

func TestLoreDecisionKeepsPartialResultsWhenLanguageModelUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			http.Error(w, "offline", 400)
			return
		}
		_, _ = w.Write([]byte(`{"answers":{"item_0":{"type":"choice","choice":"character","confidence":0.99,"probabilities":{"character":1}},"item_1":{"type":"choice","choice":"invented","confidence":0.99,"probabilities":{"invented":1}}}}`))
	}))
	defer server.Close()
	cfg := decisionTestConfig(server.URL)
	cfg.Workspace = t.TempDir()
	results, err := ClassifyLoreItems(t.Context(), cfg, []lore.ClassificationInput{{ID: "alice", Name: "Alice"}, {ID: "ambiguous", Name: "X"}})
	if err != nil || len(results) != 1 || results[0].ID != "alice" || results[0].Type != "character" {
		t.Fatalf("lost accepted classification: %+v %v", results, err)
	}
}

func TestSemanticDecisionRequiresEvidenceAndUsesOwningModelFallback(t *testing.T) {
	for _, mode := range []string{"negative", "supported", "unsupported", "offline", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			var llmCalls, decisionCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/systemone" {
					llmCalls.Add(1)
					writeTestChat(w, `{"matched":false,"confidence":0.7,"reason":"Insufficient evidence"}`)
					return
				}
				decisionCalls.Add(1)
				if mode == "offline" {
					http.Error(w, "unavailable", 503)
					return
				}
				choice, evidence := "satisfied", "evidence_0"
				if mode == "negative" {
					choice = "not_satisfied"
				}
				if mode == "unsupported" {
					evidence = "none"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{
					"condition": map[string]any{"type": "choice", "choice": choice, "confidence": .99, "probabilities": map[string]float64{choice: 1}},
					"evidence":  map[string]any{"type": "choice", "choice": evidence, "confidence": .99, "probabilities": map[string]float64{evidence: 1}},
				}})
			}))
			defer server.Close()
			cfg := decisionTestConfig(server.URL)
			cfg.Workspace = t.TempDir()
			cfg.Decision.Enabled = mode != "disabled"
			record := automation.TriggerEvaluationRecord{Condition: "The gate opened", Instruction: "Evaluate the gate.",
				Context: automation.TriggerContext{Evidence: []automation.TriggerEvidence{{Ref: "turn-1", Title: "The gate", Snippet: "The gate opened."}}}}
			if mode == "supported" {
				// Conditions may be longer than the durable evaluation's reason.
				record.Condition = strings.Repeat("The gate opened. ", 600)
			}
			raw, err := EvaluateSemanticTrigger(t.Context(), cfg, config.AgentKindGeneral, record)
			if err != nil {
				t.Fatal(err)
			}
			evaluation, err := automation.ParseSemanticEvaluation(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := automation.ValidateSemanticEvaluationEvidence(evaluation, record.Context); err != nil {
				t.Fatal(err)
			}
			wantFallback := mode == "unsupported" || mode == "offline" || mode == "disabled"
			if (llmCalls.Load() == 1) != wantFallback {
				t.Fatalf("fallback calls: %d", llmCalls.Load())
			}
			if mode == "disabled" && decisionCalls.Load() != 0 {
				t.Fatal("disabled configuration called decision service")
			}
			if evaluation.Matched != (mode == "supported") {
				t.Fatalf("unexpected match: %+v", evaluation)
			}
		})
	}
}
