package platform

import (
	"encoding/json"
	"slices"
	"strings"

	"denova/internal/buildinfo"
	"github.com/Masterminds/semver/v3"
)

// ContributionContext selects a product destination, never an authorization scope.
type ContributionContext string

const (
	ContextWriting ContributionContext = "writing"
	ContextGame    ContributionContext = "game"
	ContextGeneral ContributionContext = "general"
)

// CommandContribution reuses tool execution or navigates to an owning panel.
// It has no separate handler, parameter mapping or execution state machine.
type CommandContribution struct {
	ID       string                `json:"id"`
	TitleKey string                `json:"titleKey"`
	Contexts []ContributionContext `json:"contexts"`
	Target   CommandTarget         `json:"target"`
}

type CommandTarget struct {
	Kind string `json:"kind" jsonschema:"enum=tool,enum=panel"`
	ID   string `json:"id"`
}

// PanelContribution mounts an existing packaged view in the host's shared shell.
type PanelContribution struct {
	ID       string                `json:"id"`
	TitleKey string                `json:"titleKey"`
	Contexts []ContributionContext `json:"contexts"`
	ViewID   string                `json:"viewId"`
}

// PageContribution adds a Project-bound first-level destination. Its optional
// Agent is private to this page, not installed as a user Agent profile.
type PageContribution struct {
	ID       string     `json:"id"`
	TitleKey string     `json:"titleKey"`
	ViewID   string     `json:"viewId"`
	Agent    *PageAgent `json:"agent,omitempty"`
}

func validContexts(values []ContributionContext, required bool) error {
	if required && len(values) == 0 {
		return failure("INVALID_ARGUMENT", "Contribution contexts must not be empty")
	}
	seen := map[ContributionContext]bool{}
	for _, value := range values {
		if seen[value] || !slices.Contains([]ContributionContext{ContextWriting, ContextGame, ContextGeneral}, value) {
			return failure("INVALID_ARGUMENT", "Unknown or duplicate contribution context %q", value)
		}
		seen[value] = true
	}
	return nil
}

// hostVersion uses the source target only for development, never to disguise an
// older installed binary as supporting a newer public contract.
func hostVersion() string {
	if buildinfo.Version == "dev" {
		return buildinfo.DevelopmentVersion
	}
	return strings.TrimPrefix(buildinfo.Version, "v")
}

func compatibleManifest(manifest Manifest) error {
	if manifest.APIMajor != APIMajor {
		return failure("API_INCOMPATIBLE", "Package %s requires API %d; host provides %d", manifest.ID, manifest.APIMajor, APIMajor)
	}
	minimum, err := semver.StrictNewVersion(manifest.MinHostVersion)
	if err != nil {
		return failure("INVALID_ARGUMENT", "minHostVersion must be a semantic version: %v", err)
	}
	host, err := semver.StrictNewVersion(hostVersion())
	if err != nil || host.LessThan(minimum) {
		return failure("API_INCOMPATIBLE", "Package %s requires Denova %s; host provides %s", manifest.ID, minimum, hostVersion())
	}
	return nil
}

func validateUIContributions(kind Kind, manifest Manifest, views map[string]bool, locales map[string]map[string]json.RawMessage, addID func(string) error, checkTool func(string) error) error {
	c := manifest.contributions()
	if kind == Game && (len(c.Panels) != 0 || len(c.Commands) != 0 || len(c.Pages) != 0) {
		return failure("INVALID_ARGUMENT", "Game views belong to instances, not plugin contributions")
	}
	validate := func(id, title string, contexts []ContributionContext) error {
		if err := addID(id); err != nil {
			return err
		}
		for _, locale := range []string{"zh-CN", "en-US"} {
			var text string
			if json.Unmarshal(locales[locale][title], &text) != nil || strings.TrimSpace(text) == "" {
				return failure("INVALID_ARGUMENT", "Contribution %s requires localized title %s", id, title)
			}
		}
		return validContexts(contexts, true)
	}
	for _, page := range c.Pages {
		if err := validate(page.ID, page.TitleKey, []ContributionContext{ContextGeneral}); err != nil {
			return err
		}
		if !views[page.ViewID] {
			return failure("INVALID_ARGUMENT", "Page %s references an unknown view", page.ID)
		}
	}
	panels := map[string]PanelContribution{}
	for _, panel := range c.Panels {
		if err := validate(panel.ID, panel.TitleKey, panel.Contexts); err != nil {
			return err
		}
		if !views[panel.ViewID] {
			return failure("INVALID_ARGUMENT", "Panel %s references an unknown view", panel.ID)
		}
		panels[panel.ID] = panel
	}
	for _, command := range c.Commands {
		if err := validate(command.ID, command.TitleKey, command.Contexts); err != nil {
			return err
		}
		switch command.Target.Kind {
		case "tool":
			if command.Target.ID == "builtin/assistant" {
				return failure("INVALID_ARGUMENT", "Commands must target a declared tool")
			}
			if err := checkTool(command.Target.ID); err != nil {
				return err
			}
		case "panel":
			panel, ok := panels[command.Target.ID]
			if !ok {
				return failure("INVALID_ARGUMENT", "Unknown command panel %s", command.Target.ID)
			}
			for _, scene := range command.Contexts {
				if !slices.Contains(panel.Contexts, scene) {
					return failure("INVALID_ARGUMENT", "Panel %s is unavailable in command context %s", panel.ID, scene)
				}
			}
		default:
			return failure("INVALID_ARGUMENT", "Unsupported command target %s", command.Target.Kind)
		}
	}
	return nil
}

// PageAgent contributes task guidance to the host Project Agent. It owns no
// model, tools, execution loop or conversation; plugin tools use AgentContexts.
type PageAgent struct {
	Instructions string `json:"instructions"`
}
