package platform

import (
	"encoding/json"
	"slices"
)

// PluginPage is the installed navigation projection. Opening still enforces
// the selected Project's plugin enablement, grants, settings and model binding.
type PluginPage struct {
	ID        string        `json:"id"`
	PluginID  string        `json:"pluginId"`
	ReleaseID string        `json:"releaseId"`
	Title     LocalizedText `json:"title"`
	Agent     bool          `json:"agent"`
}

func (m *Manager) pluginPages(release Release) ([]PluginPage, error) {
	labels := map[string]map[string]json.RawMessage{}
	for locale, path := range release.Manifest.Locales {
		data, err := m.readReleaseFile(release, path)
		if err != nil {
			return nil, err
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		labels[locale] = values
	}
	pages := []PluginPage{}
	for _, page := range release.Manifest.contributions().Pages {
		var title LocalizedText
		_ = json.Unmarshal(labels["zh-CN"][page.TitleKey], &title.Chinese)
		_ = json.Unmarshal(labels["en-US"][page.TitleKey], &title.English)
		pages = append(pages, PluginPage{ID: page.ID, PluginID: release.Manifest.ID, ReleaseID: release.Ref.ReleaseID, Title: title, Agent: page.Agent != nil})
	}
	return pages, nil
}

func (m *Manager) PluginPages() ([]PluginPage, error) {
	installed, err := m.List(Plugin)
	if err != nil {
		return nil, err
	}
	pages := []PluginPage{}
	for _, item := range installed {
		if item.Removed || !item.Enabled || item.Problem != nil {
			continue
		}
		release, _, err := m.release(ReleaseRef{Package: PackageRef{Kind: Plugin, ID: item.ID}, ReleaseID: item.CurrentRelease})
		if err != nil {
			return nil, err
		}
		entries, err := m.pluginPages(release)
		if err != nil {
			return nil, err
		}
		pages = append(pages, entries...)
	}
	return pages, nil
}

// PageInstructions resolves installed guidance in the same Project scope used
// for plugin tools. Page snapshots never grant capabilities or select models.
func (m *Manager) PageInstructions(projectID, pluginID, pageID string) (string, error) {
	if _, _, err := m.registry.Resolve(projectID, true); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	config, err := m.ProjectConfiguration(projectID)
	if err != nil {
		return "", err
	}
	if slices.Contains(config.Extensions.DisabledPlugins, pluginID) {
		return "", failure("PERMISSION_DENIED", "Plugin is disabled in this Project")
	}
	items, err := m.List(Plugin)
	if err != nil {
		return "", err
	}
	var selected Installed
	for _, item := range items {
		if item.ID == pluginID {
			selected = item
			break
		}
	}
	release, installed, err := m.release(ReleaseRef{Package: PackageRef{Kind: Plugin, ID: pluginID}, ReleaseID: selected.CurrentRelease})
	if err != nil {
		return "", err
	}
	if !installed.Enabled || installed.Removed {
		return "", failure("PERMISSION_DENIED", "Plugin is unavailable")
	}
	for _, page := range release.Manifest.Contributes.Pages {
		if page.ID == pageID && page.Agent != nil {
			return page.Agent.Instructions, nil
		}
	}
	return "", failure("NOT_FOUND", "Plugin page assistant is unavailable")
}
