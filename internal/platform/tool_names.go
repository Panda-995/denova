package platform

import (
	"regexp"
	"slices"
)

var toolIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Tool IDs are model-visible API names. Prefixes reserve separate namespaces
// for public plugins and game-private tools; IDs are never rewritten or hashed.
func agentToolName(kind Kind, id string) string { return string(kind) + "_" + id }

func validateToolID(kind Kind, id string) error {
	if !toolIdentifier.MatchString(id) || len(agentToolName(kind, id)) > 64 {
		return failure("INVALID_ARGUMENT", "Tool ID %q must match [a-z][a-z0-9_-]* and fit within 64 characters including the %s_ prefix", id, kind)
	}
	return nil
}

// Derive ownership from installed manifests, including disabled plugins and
// retained releases that existing consumers can still pin. No separate registry
// is persisted. Call while holding the installation mutation lock.
func validatePluginToolNames(plugins []Installed, additions ...Manifest) error {
	owners := map[string]string{}
	claim := func(manifest Manifest) error {
		for _, tool := range manifest.contributions().Tools {
			name := agentToolName(Plugin, tool.ID)
			if owner, exists := owners[name]; exists && owner != manifest.ID {
				return failure("TOOL_NAME_CONFLICT", "Tool %s is declared by both plugins %s and %s", name, owner, manifest.ID)
			}
			owners[name] = manifest.ID
		}
		return nil
	}
	for _, plugin := range plugins {
		if plugin.Removed && !slices.ContainsFunc(additions, func(manifest Manifest) bool { return manifest.ID == plugin.ID }) {
			continue
		}
		if plugin.Problem != nil {
			return plugin.Problem
		}
		for _, release := range plugin.Releases {
			if err := claim(release.Manifest); err != nil {
				return err
			}
		}
	}
	for _, manifest := range additions {
		if err := claim(manifest); err != nil {
			return err
		}
	}
	return nil
}
