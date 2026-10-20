package config

import (
	"encoding/json"
	"reflect"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func TestDecisionConfigurationRoundTripAndScope(t *testing.T) {
	settings := Settings{Decision: &DecisionSettings{
		Enabled: true, DefaultProfileID: "test", GamePresentation: true,
		Endpoints: []DecisionEndpointSettings{{ID: "local", Name: "Local", Endpoint: "http://localhost/v1/systemone", APIKey: "secret"}},
		Profiles:  []DecisionProfileSettings{{ID: "test", Name: "Fast", EndpointID: "local", Model: "test"}},
	}}
	data, err := toml.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var restored Settings
	if err := toml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	cfg := configFromLayered(t.TempDir(), t.TempDir(), LayeredSettings{Effective: restored})
	if !reflect.DeepEqual(cfg.Decision, settings.Decision) {
		t.Fatal("runtime lost decision settings")
	}
	disabled, err := ApplySettingsMergePatch(restored, json.RawMessage(`{"decision":{"enabled":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	if Merge(restored, disabled).Decision.Configured() {
		t.Fatal("disable was not preserved through merge")
	}
	if ValidateWorkspaceSettingsPatch(json.RawMessage(`{"decision":{"endpoints":[{"id":"local","api_key":"project-secret"}]}}`)) == nil {
		t.Fatal("workspace accepted a user-owned connection")
	}
}

func TestDecisionRequiresAvailableExplicitDefault(t *testing.T) {
	disabled := false
	tests := []struct {
		name   string
		change func(*DecisionSettings)
	}{
		{"disabled", func(s *DecisionSettings) { s.Enabled = false }},
		{"no default", func(s *DecisionSettings) { s.DefaultProfileID = "" }},
		{"missing default", func(s *DecisionSettings) { s.DefaultProfileID = "missing" }},
		{"disabled profile", func(s *DecisionSettings) { s.Profiles[0].Enabled = &disabled }},
		{"missing model", func(s *DecisionSettings) { s.Profiles[0].Model = " " }},
		{"missing connection", func(s *DecisionSettings) { s.Profiles[0].EndpointID = "missing" }},
		{"disabled connection", func(s *DecisionSettings) { s.Endpoints[0].Enabled = &disabled }},
		{"invalid URL", func(s *DecisionSettings) { s.Endpoints[0].Endpoint = "file:///tmp/socket" }},
		{"credentials in URL", func(s *DecisionSettings) { s.Endpoints[0].Endpoint = "https://user:secret@example.com/decision" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := &DecisionSettings{
				Enabled: true, DefaultProfileID: "selected",
				Endpoints: []DecisionEndpointSettings{
					{ID: "first", Endpoint: "https://first.example/decision", APIKey: "first-key"},
					{ID: "second", Endpoint: "https://second.example/decision", APIKey: "second-key"},
				},
				Profiles: []DecisionProfileSettings{
					{ID: "selected", EndpointID: "first", Model: "model"},
					{ID: "other", EndpointID: "second", Model: "model"},
				},
			}
			tt.change(settings)
			if route, ok := settings.Resolve(); ok || route != (ResolvedDecisionModel{}) {
				t.Fatal("unavailable default silently routed to a connection")
			}
		})
	}
	var absent *DecisionSettings
	if absent.Configured() {
		t.Fatal("absent settings enabled decisions")
	}
}
