package config

import (
	"net/url"
	"strings"
)

// DecisionSettings owns user-scoped System One connections and model choices.
// All opted-in scenes use the selected default; unavailable choices leave the
// existing LLM flow active instead of silently switching to another connection.
type DecisionSettings struct {
	Enabled            bool                       `toml:"enabled" json:"enabled"`
	Endpoints          []DecisionEndpointSettings `toml:"endpoints" json:"endpoints"`
	Profiles           []DecisionProfileSettings  `toml:"profiles" json:"profiles"`
	DefaultProfileID   string                     `toml:"default_profile_id" json:"default_profile_id"`
	GamePresentation   bool                       `toml:"game_presentation" json:"game_presentation"`
	LoreClassification bool                       `toml:"lore_classification" json:"lore_classification"`
	SemanticTriggers   bool                       `toml:"semantic_triggers" json:"semantic_triggers"`
}

// DecisionEndpointSettings shares one full request URL and credential across
// its profiles. IDs are stable independently of display names and model names.
type DecisionEndpointSettings struct {
	ID       string `toml:"id" json:"id"`
	Name     string `toml:"name" json:"name"`
	Enabled  *bool  `toml:"enabled,omitempty" json:"enabled,omitempty"`
	Endpoint string `toml:"endpoint" json:"endpoint"`
	APIKey   string `toml:"api_key" json:"api_key"`
}

type DecisionProfileSettings struct {
	ID         string `toml:"id" json:"id"`
	Name       string `toml:"name" json:"name"`
	Enabled    *bool  `toml:"enabled,omitempty" json:"enabled,omitempty"`
	EndpointID string `toml:"endpoint_id" json:"endpoint_id"`
	Model      string `toml:"model" json:"model"`
}

// ResolvedDecisionModel is the immutable route for one evaluation.
type ResolvedDecisionModel struct {
	Endpoint string
	APIKey   string
	Model    string
}

func (s *DecisionSettings) Resolve() (ResolvedDecisionModel, bool) {
	if s == nil || !s.Enabled || s.DefaultProfileID == "" {
		return ResolvedDecisionModel{}, false
	}
	for _, profile := range s.Profiles {
		if profile.ID != s.DefaultProfileID {
			continue
		}
		if profile.Enabled != nil && !*profile.Enabled || profile.EndpointID == "" || strings.TrimSpace(profile.Model) == "" {
			return ResolvedDecisionModel{}, false
		}
		for _, endpoint := range s.Endpoints {
			if endpoint.ID != profile.EndpointID {
				continue
			}
			if endpoint.Enabled != nil && !*endpoint.Enabled {
				return ResolvedDecisionModel{}, false
			}
			address, err := url.Parse(strings.TrimSpace(endpoint.Endpoint))
			if err != nil || address.Host == "" || (address.Scheme != "https" && address.Scheme != "http") || address.User != nil {
				return ResolvedDecisionModel{}, false
			}
			return ResolvedDecisionModel{
				Endpoint: address.String(),
				APIKey:   strings.TrimSpace(endpoint.APIKey),
				Model:    strings.TrimSpace(profile.Model),
			}, true
		}
		return ResolvedDecisionModel{}, false
	}
	return ResolvedDecisionModel{}, false
}

func (s *DecisionSettings) Configured() bool {
	_, ok := s.Resolve()
	return ok
}
