package platform

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
)

// PluginAction is a read-only projection of a declared command or panel. Tool
// parameters use the existing configuration form renderer, never a new UI DSL.
type PluginAction struct {
	ID     string             `json:"id"`
	Kind   string             `json:"kind"`
	Title  LocalizedText      `json:"title"`
	Target CommandTarget      `json:"target"`
	Form   *ConfigurationForm `json:"form,omitempty"`
}

type ProjectPlugin struct {
	ID        string             `json:"id"`
	ReleaseID string             `json:"releaseId"`
	Name      LocalizedText      `json:"name"`
	Enabled   bool               `json:"enabled"`
	Problem   *Error             `json:"problem,omitempty"`
	Models    []ModelRequirement `json:"models"`
	Pages     []PluginPage       `json:"pages"`
	Actions   []PluginAction     `json:"actions"`
}

func (m *Manager) ProjectPlugins(projectID string, scene ContributionContext, locale string) ([]ProjectPlugin, error) {
	if err := validContexts([]ContributionContext{scene}, true); err != nil {
		return nil, err
	}
	configuration, err := m.ProjectConfiguration(projectID)
	if err != nil {
		return nil, err
	}
	installed, err := m.List(Plugin)
	if err != nil {
		return nil, err
	}
	result := []ProjectPlugin{}
	for _, item := range installed {
		if item.Removed {
			continue
		}
		entry := ProjectPlugin{ID: item.ID, ReleaseID: item.CurrentRelease, Name: LocalizedText{Chinese: item.ID, English: item.ID}, Enabled: item.Enabled && !slices.Contains(configuration.Extensions.DisabledPlugins, item.ID), Actions: []PluginAction{}, Models: []ModelRequirement{}}
		release, _, problem := m.release(ReleaseRef{Package: PackageRef{Kind: Plugin, ID: item.ID}, ReleaseID: item.CurrentRelease})
		if item.Problem != nil {
			problem = item.Problem
		}
		if problem == nil {
			entry.Name = release.Manifest.Name
			pins, err := m.resolveDependencies(release.Manifest, nil)
			problem = err
			if problem == nil {
				_, _, problem = m.pluginBindings(release, pins)
			}
			if problem == nil {
				entry.Models, problem = m.modelRequirements(release, pins)
			}
			// Entry declarations remain visible when configuration needs repair.
			entry.Pages, err = m.pluginPages(release)
			if problem == nil {
				problem = err
			}
			entry.Actions, err = m.pluginActions(release, pins, scene, locale)
			if problem == nil {
				problem = err
			}
			if problem == nil {
				for _, pin := range pins {
					if slices.Contains(configuration.Extensions.DisabledPlugins, pin.PluginID) {
						problem = failure("DEPENDENCY_UNAVAILABLE", "Plugin %s is disabled in this Project", pin.PluginID)
						break
					}
				}
			}
		}
		if problem != nil {
			_, entry.Problem = ErrorResponse(problem)
		}
		result = append(result, entry)
	}
	return result, nil
}

func (m *Manager) pluginActions(release Release, pins []DependencyPin, scene ContributionContext, locale string) ([]PluginAction, error) {
	labels := map[string]map[string]json.RawMessage{}
	for language, path := range release.Manifest.Locales {
		data, err := m.readReleaseFile(release, path)
		if err != nil {
			return nil, err
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		labels[language] = values
	}
	title := func(key string) LocalizedText {
		var value LocalizedText
		_ = json.Unmarshal(labels["zh-CN"][key], &value.Chinese)
		_ = json.Unmarshal(labels["en-US"][key], &value.English)
		return value
	}
	actions := []PluginAction{}
	for _, panel := range release.Manifest.contributions().Panels {
		if slices.Contains(panel.Contexts, scene) {
			actions = append(actions, PluginAction{ID: panel.ID, Kind: "panel", Title: title(panel.TitleKey), Target: CommandTarget{Kind: "panel", ID: panel.ID}})
		}
	}
	for _, command := range release.Manifest.contributions().Commands {
		if !slices.Contains(command.Contexts, scene) {
			continue
		}
		action := PluginAction{ID: command.ID, Kind: "command", Title: title(command.TitleKey), Target: command.Target}
		if command.Target.Kind == "tool" {
			owner, tool, err := m.commandTool(release, pins, command.Target.ID)
			if err != nil {
				return actions, err
			}
			form, err := m.commandForm(owner, tool, locale)
			if err != nil {
				return actions, err
			}
			action.Form = form
			action.Target.ID = owner.Manifest.ID + "/" + tool.ID
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func (m *Manager) commandForm(owner Release, tool Tool, locale string) (*ConfigurationForm, error) {
	data, err := m.readReleaseFile(owner, tool.Definition)
	if err != nil {
		return nil, err
	}
	var definition ToolDefinition
	if err := decodeJSON(data, &definition); err != nil {
		return nil, err
	}
	form := &ConfigurationForm{Defaults: map[string]any{}, UISchema: map[string]any{}}
	if err := json.Unmarshal(definition.InputSchema, &form.Schema); err != nil {
		return nil, err
	}
	labels := map[string]map[string]json.RawMessage{}
	for language, path := range owner.Manifest.Locales {
		raw, err := m.readReleaseFile(owner, path)
		if err != nil {
			return nil, err
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, err
		}
		labels[language] = values
	}
	if err := localizeFormSchema(form.Schema, labels, locale, toolInputFormSchema); err != nil {
		return nil, err
	}
	return form, nil
}

func (m *Manager) commandTool(owner Release, pins []DependencyPin, reference string) (Release, Tool, error) {
	provider, id, qualified := strings.Cut(reference, "/")
	if !qualified {
		id, provider = reference, owner.Manifest.ID
	}
	if provider != owner.Manifest.ID {
		index := slices.IndexFunc(pins, func(pin DependencyPin) bool { return pin.PluginID == provider })
		if index < 0 {
			return Release{}, Tool{}, failure("DEPENDENCY_UNAVAILABLE", "Command provider %s is unavailable", provider)
		}
		var err error
		owner, _, err = m.release(ReleaseRef{Package: PackageRef{Kind: Plugin, ID: provider}, ReleaseID: pins[index].ReleaseID})
		if err != nil {
			return Release{}, Tool{}, err
		}
	}
	for _, tool := range owner.Manifest.contributions().Tools {
		if tool.ID == id {
			return owner, tool, nil
		}
	}
	return Release{}, Tool{}, failure("DEPENDENCY_UNAVAILABLE", "Command target %s is not a tool", reference)
}

type OpenPluginAction struct {
	ReleaseID  string              `json:"releaseId"`
	PluginID   string              `json:"pluginId"`
	ProjectID  string              `json:"projectId"`
	Context    ContributionContext `json:"context"`
	Kind       string              `json:"kind"`
	ActionID   string              `json:"actionId"`
	ConsumerID string              `json:"consumerId"`
	OpenOptions
}

func (m *Manager) OpenPluginAction(ctx context.Context, input OpenPluginAction) (RuntimeSnapshot, error) {
	if input.ConsumerID == "" {
		return RuntimeSnapshot{}, failure("INVALID_ARGUMENT", "A plugin action requires a consumer")
	}
	if err := validContexts([]ContributionContext{input.Context}, true); err != nil {
		return RuntimeSnapshot{}, err
	}
	release, _, err := m.currentPackage(Plugin, input.PluginID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	if input.ReleaseID != release.Ref.ReleaseID {
		return RuntimeSnapshot{}, failure("DOCUMENT_CONFLICT", "Plugin updated; refresh its actions before opening")
	}
	panelID, found := "", false
	switch input.Kind {
	case "page":
		for _, page := range release.Manifest.contributions().Pages {
			if page.ID == input.ActionID {
				found = true
			}
		}
	case "panel":
		panelID = input.ActionID
	case "command":
		for _, command := range release.Manifest.contributions().Commands {
			if command.ID == input.ActionID && slices.Contains(command.Contexts, input.Context) {
				found = true
				if command.Target.Kind == "panel" {
					panelID = command.Target.ID
				}
			}
		}
	default:
		return RuntimeSnapshot{}, failure("INVALID_ARGUMENT", "Unknown plugin action kind")
	}
	viewID := ""
	if input.Kind == "page" {
		for _, page := range release.Manifest.contributions().Pages {
			if page.ID == input.ActionID {
				viewID = page.ViewID
			}
		}
	}
	if panelID != "" {
		found = false
		for _, panel := range release.Manifest.contributions().Panels {
			if panel.ID == panelID && slices.Contains(panel.Contexts, input.Context) {
				viewID, found = panel.ViewID, true
			}
		}
	}
	if !found {
		return RuntimeSnapshot{}, failure("NOT_FOUND", "Plugin action is unavailable in this context")
	}
	configuration, err := m.ProjectConfiguration(input.ProjectID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	return m.ActivatePlugin(ctx, ActivatePlugin{PluginID: input.PluginID, ReleaseID: release.Ref.ReleaseID, Scope: Scope{Kind: "project", ProjectID: input.ProjectID}, Models: configuration.Extensions.Models, ConsumerID: input.ConsumerID, ViewID: viewID, OpenOptions: input.OpenOptions})
}
