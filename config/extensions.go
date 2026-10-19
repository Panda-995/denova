package config

// ExtensionSettings belongs only to a Project Store's config.toml. Platform
// consumers read it directly; it is not merged into global Agent settings.
type ExtensionSettings struct {
	DisabledPlugins []string          `toml:"disabled_plugins,omitempty" json:"disabledPlugins"`
	Models          map[string]string `toml:"models,omitempty" json:"models"`
}
