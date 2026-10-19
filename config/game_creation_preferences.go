package config

// GameCreationPreferences remembers the last successful creation in one Project.
// It is a form preference, never a Story recovery source or an exported resource
// recommendation. Only successful, non-preview creation writes this snapshot.
// Model preferences remain user-owned; opening content is not remembered.
type GameCreationPreferences struct {
	Resources                GameCreationDefaults     `json:"resources" toml:"resources"`
	Protagonist              *GameCreationProtagonist `json:"protagonist,omitempty" toml:"protagonist,omitempty"`
	DisabledModules          []string                 `json:"disabled_modules" toml:"disabled_modules"`
	CustomAgentID            string                   `json:"custom_agent_id" toml:"custom_agent_id"`
	PlanningEnabled          bool                     `json:"planning_enabled" toml:"planning_enabled"`
	ReplyTargetChars         int                      `json:"reply_target_chars" toml:"reply_target_chars"`
	ChoiceCount              int                      `json:"choice_count" toml:"choice_count"`
	ImageMode                string                   `json:"image_mode" toml:"image_mode"`
	ImageIntervalTurns       int                      `json:"image_interval_turns" toml:"image_interval_turns"`
	ImagePresetID            string                   `json:"image_preset_id" toml:"image_preset_id"`
	DifficultyShift          int                      `json:"difficulty_shift" toml:"difficulty_shift"`
	RollModifier             int                      `json:"roll_modifier" toml:"roll_modifier"`
	RuleStateConsumptionMode string                   `json:"rule_state_consumption_mode" toml:"rule_state_consumption_mode"`
	RuleVisibilityMode       string                   `json:"rule_visibility_mode" toml:"rule_visibility_mode"`
	StateSchemaMode          string                   `json:"state_schema_mode" toml:"state_schema_mode"`
	Background               bool                     `json:"background" toml:"background"`
	Characters               bool                     `json:"characters" toml:"characters"`
	BackgroundFocus          *GameCreationImageFocus  `json:"background_focus,omitempty" toml:"background_focus,omitempty"`
}

// GameCreationProtagonist retains a Lore identity or a custom character draft,
// never a Lore body snapshot. Mode follows the Story protagonist selection API.
type GameCreationProtagonist struct {
	Mode             string `json:"mode" toml:"mode"`
	Name             string `json:"name,omitempty" toml:"name,omitempty"`
	Profile          string `json:"profile,omitempty" toml:"profile,omitempty"`
	SourceLoreItemID string `json:"source_lore_item_id,omitempty" toml:"source_lore_item_id,omitempty"`
}

// GameCreationImageFocus retains the user's framing without storing a file path.
type GameCreationImageFocus struct {
	X float64 `json:"x" toml:"x"`
	Y float64 `json:"y" toml:"y"`
}
