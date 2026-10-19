package platform

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"denova/config"
	"denova/internal/revisionfile"
	toml "github.com/pelletier/go-toml/v2"
)

// ProjectConfiguration projects the existing settings file's revision, rather
// than creating a second configuration store or concurrency token.
type ProjectConfiguration struct {
	Revision   string                   `json:"revision"`
	Extensions config.ExtensionSettings `json:"extensions"`
}

type ProjectConfigurationInput struct {
	ExpectedRevision string                   `json:"expectedRevision"`
	Extensions       config.ExtensionSettings `json:"extensions"`
}

func (m *Manager) ProjectConfiguration(projectID string) (ProjectConfiguration, error) {
	_, layout, err := m.registry.Resolve(projectID, true)
	if err != nil {
		return ProjectConfiguration{}, err
	}
	snapshot, err := revisionfile.Read(context.Background(), layout.ConfigPath())
	if err != nil {
		return ProjectConfiguration{}, err
	}
	if len(snapshot.Content) > MaxDefinitionBytes {
		return ProjectConfiguration{}, failure("LIMIT_EXCEEDED", "Project configuration exceeds limit")
	}
	var settings config.Settings
	if err := toml.Unmarshal(snapshot.Content, &settings); err != nil {
		return ProjectConfiguration{}, failure("INVALID_CONFIGURATION", "Read project extension configuration: %v", err)
	}
	value := config.ExtensionSettings{DisabledPlugins: []string{}, Models: map[string]string{}}
	if settings.Extensions != nil {
		value.DisabledPlugins = append(value.DisabledPlugins, settings.Extensions.DisabledPlugins...)
		maps.Copy(value.Models, settings.Extensions.Models)
	}
	return ProjectConfiguration{Revision: snapshot.Revision, Extensions: value}, nil
}

func (m *Manager) SaveProjectConfiguration(ctx context.Context, projectID string, input ProjectConfigurationInput) (ProjectConfiguration, error) {
	if input.ExpectedRevision == "" {
		return ProjectConfiguration{}, failure("INVALID_ARGUMENT", "Project configuration revision is required")
	}
	for _, id := range input.Extensions.DisabledPlugins {
		if err := validateID(id); err != nil {
			return ProjectConfiguration{}, err
		}
	}
	slices.Sort(input.Extensions.DisabledPlugins)
	input.Extensions.DisabledPlugins = slices.Compact(input.Extensions.DisabledPlugins)
	for key, profile := range input.Extensions.Models {
		provider, slot, ok := strings.Cut(key, "/")
		if key != "builtin/assistant" && (!ok || validateID(provider) != nil || validateID(slot) != nil) {
			return ProjectConfiguration{}, failure("INVALID_ARGUMENT", "Invalid model binding %s", key)
		}
		if strings.TrimSpace(profile) == "" || len(profile) > 256 {
			return ProjectConfiguration{}, failure("INVALID_ARGUMENT", "Invalid model profile for %s", key)
		}
	}
	_, layout, err := m.registry.Resolve(projectID, true)
	if err != nil {
		return ProjectConfiguration{}, err
	}
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	_, err = config.MutateSettingsFile(layout.ConfigPath(), input.ExpectedRevision, func(settings config.Settings) (config.Settings, error) {
		settings.Extensions = &input.Extensions
		return settings, nil
	})
	if err != nil {
		if err == config.ErrSettingsRevisionConflict {
			return ProjectConfiguration{}, failure("DOCUMENT_CONFLICT", "Project configuration changed")
		}
		return ProjectConfiguration{}, err
	}
	// Cancellation follows the saved policy even if process cleanup later fails.
	for _, runtime := range m.runtimes {
		if runtime.owner.context.Scope.ProjectID != projectID {
			continue
		}
		for _, id := range input.Extensions.DisabledPlugins {
			if runtime.uses(Plugin, id) {
				runtime.cancel()
				break
			}
		}
	}
	for id, runtime := range m.runtimes {
		if runtime.ctx.Err() != nil && runtime.owner.context.Scope.ProjectID == projectID {
			if err := m.stopLocked(ctx, id); err != nil {
				return ProjectConfiguration{}, err
			}
		}
	}
	slog.Info("platform_project_configuration_saved", "project", projectID)
	return m.ProjectConfiguration(projectID)
}

func (m *Manager) checkProjectPlugins(projectID string, releases []Release) error {
	if projectID == "" {
		return nil
	}
	configuration, err := m.ProjectConfiguration(projectID)
	if err != nil {
		return err
	}
	for _, release := range releases {
		if release.Ref.Package.Kind == Plugin && slices.Contains(configuration.Extensions.DisabledPlugins, release.Manifest.ID) {
			return failure("DEPENDENCY_UNAVAILABLE", "Plugin %s is disabled in Project %s", release.Manifest.ID, projectID)
		}
	}
	return nil
}
