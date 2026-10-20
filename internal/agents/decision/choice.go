// Package decision implements bounded System One choice requests without a
// chat-provider adapter. Callers own scene policy, fallbacks, and persistence.
package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"denova/config"
	agentrun "denova/internal/agents/run"
)

const MaxRequestBytes = 1024 * 1024
const maxResponseBytes = 1024 * 1024

type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

func Choice(instructions string, criteria map[string]string) Question {
	return Question{Type: "choice", Instructions: instructions, Criteria: criteria}
}

type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    *float64           `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Accepted treats missing confidence and malformed/out-of-catalog answers as
// uncertainty, preserving valid siblings for scene-specific partial fallback.
func (a Answer) Accepted(q Question, minimum float64) bool {
	_, known := q.Criteria[a.Choice]
	p, present := a.Probabilities[a.Choice]
	if a.Type != "choice" || !known || a.Confidence == nil ||
		!(*a.Confidence >= minimum && *a.Confidence <= 1) || !present || !(p >= 0 && p <= 1) {
		return false
	}
	for key, probability := range a.Probabilities {
		if _, exists := q.Criteria[key]; !exists || !(probability >= 0 && probability <= p) {
			return false
		}
	}
	return true
}

// Evaluate performs one cancellable HTTP exchange. Its infrastructure deadline
// bounds the optional optimization, not the fallback LLM's total execution.
func Evaluate(ctx context.Context, settings *config.DecisionSettings, scene string, state any, questions map[string]Question) (answers map[string]Answer, err error) {
	start := time.Now()
	span, ctx := agentrun.StartTraceSpan(ctx, "decision."+scene, map[string]any{"questions": len(questions)})
	defer func() {
		slog.InfoContext(ctx, "Decision evaluation completed", "scene", scene, "questions", len(questions), "answers", len(answers), "duration_ms", time.Since(start).Milliseconds(), "error", err)
		status := "ok"
		if err != nil {
			status = "error"
		}
		span.Finish(status, map[string]any{"answers": len(answers)})
	}()
	model, configured := settings.Resolve()
	if !configured {
		return nil, fmt.Errorf("decision model is not configured")
	}
	for name, q := range questions {
		if q.Type != "choice" || len(q.Criteria) == 0 || len(q.Criteria) > 255 {
			return nil, fmt.Errorf("decision question %q requires 1 to 255 choices", name)
		}
	}
	body, err := json.Marshal(struct {
		Model     string              `json:"model"`
		State     any                 `json:"state"`
		Questions map[string]Question `json:"questions"`
	}{model.Model, state, questions})
	if err != nil {
		return nil, err
	}
	if len(body) > MaxRequestBytes {
		return nil, fmt.Errorf("decision request exceeds %d bytes", MaxRequestBytes)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, model.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := model.APIKey; key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		// Do not include endpoint URLs or provider bodies in logs: either may
		// contain credentials or user content.
		if requestCtx.Err() != nil {
			return nil, requestCtx.Err()
		}
		return nil, fmt.Errorf("decision transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("decision HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read decision response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("decision response exceeds byte limit")
	}
	var result struct {
		Model   string            `json:"model"`
		Answers map[string]Answer `json:"answers"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid decision response")
	}
	slog.InfoContext(ctx, "Decision model usage", "scene", scene, "model", result.Model, "input_tokens", result.Usage.InputTokens, "output_tokens", result.Usage.OutputTokens)
	return result.Answers, nil
}
