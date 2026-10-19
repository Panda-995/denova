package platform

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"sync"

	"denova/config"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// HostAgentTools reads the installed plugins and shared settings for a new
// execution. It persists nothing; calls remain bound to the caller's Project
// and conversation. Existing executions finish with their already loaded tools.
func (m *Manager) HostAgentTools(cfg *config.Config, agentKind string) (agenttool.Toolset, error) {
	if cfg == nil || cfg.AgentPluginScope == (config.AgentPluginScope{}) {
		return nil, nil
	}
	scope := Scope{ProjectID: cfg.ProjectID, SessionID: cfg.AgentPluginScope.SessionID, StoryID: cfg.AgentPluginScope.StoryID, BranchID: cfg.AgentPluginScope.BranchID}
	switch {
	case scope.ProjectID == "":
		return nil, failure("INVALID_ARGUMENT", "Plugin tools require a Project")
	case scope.SessionID != "" && scope.StoryID == "" && scope.BranchID == "":
		scope.Kind = "session"
	case scope.SessionID == "" && scope.StoryID != "" && scope.BranchID != "":
		scope.Kind = "story"
	default:
		return nil, failure("INVALID_ARGUMENT", "Plugin tools require a Product Session or Story branch")
	}
	if _, _, err := m.registry.Resolve(scope.ProjectID, true); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	items, err := m.List(Plugin)
	if err != nil {
		return nil, err
	}
	projectConfig, err := m.ProjectConfiguration(scope.ProjectID)
	if err != nil {
		return nil, err
	}
	consumerContext := ContextGeneral
	switch agentKind {
	case config.AgentKindIDE:
		consumerContext = ContextWriting
	case config.AgentKindInteractiveStory:
		consumerContext = ContextGame
	}
	models := maps.Clone(projectConfig.Extensions.Models)
	profileID := config.ResolveAgentModel(cfg, agentKind).ProfileID
	if profileID == "" {
		profileID = "default"
	}
	models["builtin/assistant"] = profileID
	set := &hostPluginToolset{manager: m, context: consumerContext, releases: map[string]Release{}, settings: map[string]map[string]any{}, scope: scope, models: models}
	for _, item := range items {
		if !item.Enabled || item.Removed || slices.Contains(projectConfig.Extensions.DisabledPlugins, item.ID) {
			continue
		}
		release, _, err := m.release(ReleaseRef{Package: PackageRef{Kind: Plugin, ID: item.ID}, ReleaseID: item.CurrentRelease})
		if err == nil && !slices.ContainsFunc(release.Manifest.contributions().Tools, func(tool Tool) bool { return slices.Contains(tool.AgentContexts, consumerContext) }) {
			continue
		}
		var pins []DependencyPin
		if err == nil {
			pins, err = m.resolveDependencies(release.Manifest, nil)
		}
		var releases map[string]Release
		var settings map[string]map[string]any
		if err == nil {
			releases, settings, err = m.pluginBindings(release, pins)
		}
		if err == nil {
			for id := range releases {
				if slices.Contains(projectConfig.Extensions.DisabledPlugins, id) {
					err = failure("DEPENDENCY_UNAVAILABLE", "Plugin %s is disabled in this Project", id)
					break
				}
			}
		}
		if err == nil && !slices.Contains(release.Grants, "tools.invoke") {
			err = failure("PERMISSION_DENIED", "tools.invoke is not granted")
		}
		if err == nil {
			err = m.validateModels(release, pins, scope.ProjectID, models)
		}
		if err == nil {
			for _, tool := range release.Manifest.contributions().Tools {
				if !slices.Contains(tool.AgentContexts, consumerContext) {
					continue
				}
				if _, err = m.agentTool(release, tool, settings[item.ID], nil); err != nil {
					break
				}
			}
		}
		if err != nil {
			slog.Warn("platform_agent_plugin_unavailable", "plugin", item.ID, "release", item.CurrentRelease, "scope", scope, "error", err)
			continue
		}
		set.providers = append(set.providers, item.ID)
		maps.Copy(set.releases, releases)
		maps.Copy(set.settings, settings)
	}
	if len(set.providers) == 0 {
		return nil, nil
	}
	return set, nil
}

type hostPluginToolset struct {
	context   ContributionContext
	manager   *Manager
	providers []string
	releases  map[string]Release
	settings  map[string]map[string]any
	scope     Scope
	models    map[string]string
}

func (t *hostPluginToolset) Identity() agentschema.CapabilityIdentity {
	// Reuse the Agent's behavior check so a paused tool batch cannot silently
	// resume against changed implementations or shared settings.
	configuration := make(map[string]any, len(t.releases))
	for id, release := range t.releases {
		configuration[id] = []any{release.Ref.ReleaseID, t.settings[id]}
	}
	raw, _ := json.Marshal([]any{configuration, t.providers, t.context, t.models})
	return agentschema.CapabilityIdentity{Kind: "denova.plugin.tools", Version: 1, ConfigHash: stableID(string(raw))}
}

func (t *hostPluginToolset) PrepareTools(ctx context.Context, request agenttool.ToolRequest) ([]agenttool.ToolDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	invocation := &hostPluginInvocation{toolset: t, owner: ctx, releases: t.releases, settings: t.settings, runtimes: map[string]*Runtime{}}
	definitions := []agenttool.ToolDefinition{}
	for _, id := range t.providers {
		release := t.releases[id]
		prepared := []agenttool.ToolDefinition{}
		for _, tool := range release.Manifest.Contributes.Tools {
			if !slices.Contains(tool.AgentContexts, t.context) {
				continue
			}
			definition, err := t.manager.agentTool(release, tool, t.settings[id], func(ctx context.Context, input json.RawMessage) (ToolResult, error) {
				return invocation.invoke(ctx, id, tool.ID, input)
			})
			if err != nil {
				slog.Warn("platform_agent_plugin_unavailable", "plugin", id, "operation", "prepare_tools", "error", err)
				prepared = nil
				break
			}
			prepared = append(prepared, definition)
		}
		definitions = append(definitions, prepared...)
	}
	return definitions, nil
}

// One prepared run owns its lazy processes. Tools cannot restart a runtime
// after cancellation or permission revocation using an old prepared binding.
type hostPluginInvocation struct {
	toolset  *hostPluginToolset
	owner    context.Context
	releases map[string]Release
	settings map[string]map[string]any
	mu       sync.Mutex
	runtimes map[string]*Runtime
}

func (call *hostPluginInvocation) invoke(ctx context.Context, provider, tool string, input json.RawMessage) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	call.mu.Lock()
	runtime, err := call.runtime(provider)
	call.mu.Unlock()
	if err != nil {
		return ToolResult{}, err
	}
	if runtime.ctx.Err() != nil {
		return ToolResult{}, failure("RUNTIME_UNAVAILABLE", "Plugin runtime was stopped; start a new task")
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(runtime.ctx, cancel)
	defer stop()
	defer cancel()
	return runtime.invokeTool(linked, runtime.owner, provider, tool, input, authorizedAgentToolInvocation)
}

func (call *hostPluginInvocation) runtime(provider string) (*Runtime, error) {
	if runtime := call.runtimes[provider]; runtime != nil {
		return runtime, nil
	}
	if err := call.owner.Err(); err != nil {
		return nil, err
	}
	m := call.toolset.manager
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	release := call.releases[provider]
	// Resolve just this provider's graph using the already loaded releases.
	pins := []DependencyPin{}
	var add func(Manifest)
	seen := map[string]bool{}
	add = func(manifest Manifest) {
		for _, dep := range manifest.Requires {
			if !seen[dep.PluginID] {
				seen[dep.PluginID] = true
				bound := call.releases[dep.PluginID]
				pins = append(pins, DependencyPin{PluginID: dep.PluginID, ReleaseID: bound.Ref.ReleaseID})
				add(bound.Manifest)
			}
		}
	}
	add(release.Manifest)
	// Revocation is checked only for this tool's graph; an unrelated plugin
	// cannot invalidate another provider's already prepared invocation.
	for _, pin := range append([]DependencyPin{{PluginID: provider, ReleaseID: release.Ref.ReleaseID}}, pins...) {
		admitted := call.releases[pin.PluginID]
		current, installed, err := m.release(admitted.Ref)
		if err != nil {
			return nil, err
		}
		if !installed.Enabled || installed.Removed || !slices.Equal(current.Grants, admitted.Grants) {
			return nil, failure("PERMISSION_DENIED", "Plugin %s authorization changed", pin.PluginID)
		}
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].PluginID < pins[j].PluginID })
	id := "agent-" + randomToken()
	runtime, err := m.startRuntime(id, release, call.toolset.scope, pins, RuntimeConfiguration{frozenSettings: call.settings}, call.toolset.models, OpenOptions{hostOnly: true})
	if err != nil {
		return nil, err
	}
	m.runtimes[id], call.runtimes[provider] = runtime, runtime
	context.AfterFunc(call.owner, func() {
		launch("platform_agent_cleanup", func() {}, func() {
			if err := m.Stop(context.Background(), id); err != nil {
				slog.Error("platform_agent_cleanup_failed", "runtime", id, "error", err)
			}
		})
	})
	return runtime, nil
}
