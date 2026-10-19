package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMarketTokenSettingsRoundTrip(t *testing.T) {
	root := t.TempDir()
	existing := Settings{Market: MarketSettings{RegistryURL: "https://example.com/index.json"}}
	next, err := ApplySettingsMergePatch(existing, json.RawMessage(`{"market":{"github_token":"test-token"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSettingsFile(UserConfigPath(root), next); err != nil {
		t.Fatal(err)
	}
	layered, err := LoadLayered(root, "")
	if err != nil || layered.Effective.Market.GitHubToken != "test-token" || layered.Effective.Market.RegistryURL != existing.Market.RegistryURL {
		t.Fatalf("market settings did not round-trip: %+v %v", layered.Effective.Market, err)
	}
	next, err = ApplySettingsMergePatch(next, json.RawMessage(`{"market":{"github_token":null}}`))
	if err != nil || next.Market.GitHubToken != "" || next.Market.RegistryURL != existing.Market.RegistryURL {
		t.Fatalf("clearing the token changed registry: %+v %v", next.Market, err)
	}
}

func TestApplySettingsMergePatchPreservesOmittedFieldsAndMergesNestedObjects(t *testing.T) {
	existing := Settings{
		OpenAIModel: "keep-model",
		Theme:       "dark",
		AgentModels: AgentModelSettings{
			IDE: AgentModelOverride{ProfileID: "writer", ThinkingLevel: "medium"},
		},
	}
	next, err := ApplySettingsMergePatch(existing, json.RawMessage(`{
		"theme":"light",
		"agent_models":{"ide":{"thinking_level":"high"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if next.OpenAIModel != "keep-model" || next.Theme != "light" {
		t.Fatalf("top-level merge lost an omitted field: %#v", next)
	}
	if next.AgentModels.IDE.ProfileID != "writer" || next.AgentModels.IDE.ThinkingLevel != "high" {
		t.Fatalf("nested merge = %#v", next.AgentModels.IDE)
	}
}

func TestApplySettingsMergePatchSupportsClearAndArrayReplacement(t *testing.T) {
	existing := Settings{
		Theme: "dark",
		ModelProfiles: []ModelProfileSettings{
			{ID: "first", Model: "model-a"},
			{ID: "second", Model: "model-b"},
		},
	}
	next, err := ApplySettingsMergePatch(existing, json.RawMessage(`{
		"theme":null,
		"model_profiles":[{"id":"only","model":"model-c"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if next.Theme != "" {
		t.Fatalf("null should clear theme, got %q", next.Theme)
	}
	if len(next.ModelProfiles) != 1 || next.ModelProfiles[0].ID != "only" {
		t.Fatalf("array should be replaced atomically: %#v", next.ModelProfiles)
	}
}

func TestApplySettingsMergePatchRejectsUnknownFields(t *testing.T) {
	for _, patch := range []string{
		`{"agent_models":{"ide":{"thinking_lvel":"high"}}}`,
		`{"retired_setting":null}`,
	} {
		_, err := ApplySettingsMergePatch(Settings{}, json.RawMessage(patch))
		if !errors.Is(err, ErrInvalidSettingsPatch) {
			t.Fatalf("expected strict patch error for %s, got %v", patch, err)
		}
	}
}

func TestApplySettingsMergePatchRejectsOversizedCheckpointGuidance(t *testing.T) {
	tooLong := strings.Repeat("界", MaxCheckpointGuidanceRunes+1)
	patches := []string{
		`{"agent_context":{"ide":{"checkpoint_guidance":"` + tooLong + `"}}}`,
		`{"custom_agents":[{"id":"editor","contract":"writing.primary.v1","runtime_context":{"checkpoint_guidance":"` + tooLong + `"}}]}`,
	}
	for _, patch := range patches {
		_, err := ApplySettingsMergePatch(Settings{}, json.RawMessage(patch))
		if !errors.Is(err, ErrInvalidSettingsPatch) || !strings.Contains(err.Error(), "maximum is 1000") {
			t.Fatalf("oversized checkpoint guidance error = %v", err)
		}
	}
}

func TestValidateWorkspaceSettingsPatchUsesExplicitScopeBoundary(t *testing.T) {
	if err := ValidateWorkspaceSettingsPatch(json.RawMessage(`{"agent_tools":{"ide":{"shell":false}}}`)); err != nil {
		t.Fatalf("agent override should be workspace-scoped: %v", err)
	}
	if err := ValidateWorkspaceSettingsPatch(json.RawMessage(`{"theme":"light"}`)); !errors.Is(err, ErrInvalidSettingsPatch) {
		t.Fatalf("user-only setting should be rejected, got %v", err)
	}
	if err := ValidateWorkspaceSettingsPatch(json.RawMessage(`{"interactive_stage_character_layout":"sides"}`)); !errors.Is(err, ErrInvalidSettingsPatch) {
		t.Fatalf("personal character layout should be rejected in workspace settings, got %v", err)
	}
	if err := ValidateWorkspaceSettingsPatch(json.RawMessage(`{"agent_quick_prompts":{"writing":[]}}`)); !errors.Is(err, ErrInvalidSettingsPatch) {
		t.Fatalf("personal quick prompts should be rejected in workspace settings, got %v", err)
	}
}
