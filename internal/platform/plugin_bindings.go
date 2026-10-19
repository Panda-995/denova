package platform

// pluginBindings validates each selected provider before exposing any of the
// consumer's contributions. Unrelated graphs can remain available on failure.
func (m *Manager) pluginBindings(owner Release, pins []DependencyPin) (map[string]Release, map[string]map[string]any, error) {
	releases := map[string]Release{owner.Manifest.ID: owner}
	settings := map[string]map[string]any{}
	for _, pin := range pins {
		release, _, err := m.release(ReleaseRef{Package: PackageRef{Kind: Plugin, ID: pin.PluginID}, ReleaseID: pin.ReleaseID})
		if err != nil {
			return nil, nil, err
		}
		releases[pin.PluginID] = release
	}
	for id, release := range releases {
		if err := compatibleManifest(release.Manifest); err != nil {
			return nil, nil, err
		}
		values, err := m.settingsValues(release, owner.Ref.Environment(), nil)
		if err != nil {
			return nil, nil, err
		}
		settings[id] = values
	}
	return releases, settings, nil
}
