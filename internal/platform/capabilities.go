package platform

import "slices"

var supportedPermissions = []string{"agents.run", "tools.invoke", "tools.write", "gameData", "pluginData", "library.read", "library.write", "assets.read", "assets.write", "images.generate", "stories.read", "stories.write", "settings.write", "project.content.read", "project.content.write"}

// CapabilityState describes independent admission conditions. Discovery does not
// grant authority; every operation checks its own permission and bound scope.
type CapabilityState struct {
	Implemented bool `json:"implemented"`
	Granted     bool `json:"granted"`
	Applicable  bool `json:"applicable"`
	Configured  bool `json:"configured"`
}

type Capabilities struct {
	APIMajor      int                        `json:"apiMajor"`
	HostVersion   string                     `json:"hostVersion"`
	Permissions   []string                   `json:"permissions"`
	Capabilities  map[string]CapabilityState `json:"capabilities"`
	Models        map[string]bool            `json:"models"`
	Limits        map[string]int             `json:"limits"`
	SchemaDialect string                     `json:"schemaDialect"`
}

func (r *Runtime) capabilities(caller *activation) Capabilities {
	result := Capabilities{APIMajor: APIMajor, HostVersion: hostVersion(), Permissions: slices.Clone(caller.grants), Capabilities: map[string]CapabilityState{}, Models: map[string]bool{}, SchemaDialect: "https://json-schema.org/draft/2020-12/schema", Limits: map[string]int{"requestBytes": MaxDefinitionBytes, "fileBytes": MaxFileBytes, "instructionsBytes": 256 << 10, "assetBytes": MaxAssetBytes, "libraryItemBytes": MaxLibraryItemBytes, "libraryPageItems": 100, "imagePromptBytes": 64 << 10}}
	project := caller.context.Scope.ProjectID != ""
	result.Models["builtin/assistant"] = r.models["builtin/assistant"] != ""
	for _, slot := range caller.release.Manifest.ModelSlots {
		key := caller.release.Manifest.ID + "/" + slot.ID
		if caller.release.Ref.Package.Kind == Game {
			key = "local:" + slot.ID
		}
		result.Models[key] = r.models[key] != ""
	}
	for _, permission := range supportedPermissions {
		state := CapabilityState{Implemented: true, Granted: slices.Contains(caller.grants, permission), Applicable: true, Configured: true}
		switch permission {
		case "agents.run":
			state.Implemented = r.manager.agents != nil
			state.Applicable = project
			state.Configured = result.Models["builtin/assistant"]

			for _, definition := range caller.release.Manifest.privateAgents() {
				if r.manager.agents == nil {
					break
				}
				config, err := r.manager.agents.definition(r, caller, "local:"+definition.ID)
				state.Configured = state.Configured || err == nil && config.ModelProfile != ""
			}
		case "library.read", "library.write", "assets.read", "assets.write", "images.generate":
			state.Implemented = r.manager.resources != nil && r.manager.resources.host != nil
			state.Applicable = project
			if permission == "library.write" && state.Implemented {
				_, state.Implemented = r.manager.resources.host.(LibraryWriter)
			}
			if permission == "images.generate" {
				state.Configured = false
				if state.Implemented && project {
					models, err := r.manager.resources.host.ImageModels(r.ctx, caller.context.Scope.ProjectID)
					state.Configured = err == nil && len(models) > 0
				}
			}

		case "project.content.read", "project.content.write":
			state.Applicable = project && len(caller.release.Manifest.ProjectContent) > 0
			if r.manager.resources != nil {
				_, state.Implemented = r.manager.resources.host.(ProjectContentHost)
			} else {
				state.Implemented = false
			}
		case "stories.read", "stories.write":
			state.Implemented = r.manager.stories != nil
			state.Applicable = caller.context.Scope.Kind == "game-instance" && caller.context.Scope.StoryID != "" && caller.context.Scope.InstanceID != ""
		case "gameData":
			state.Applicable = caller.release.Ref.Package.Kind == Game && caller.process == nil
		case "pluginData":
			state.Applicable = caller.release.Ref.Package.Kind == Plugin && caller.process == nil
		case "settings.write":
			state.Applicable = caller == r.owner && !r.hostOnly && caller.context.Environment == "installed"
		case "tools.invoke", "tools.write":
			// Individual tools still enforce their declared effect and dependency.
		}
		result.Capabilities[permission] = state
	}
	return result
}
