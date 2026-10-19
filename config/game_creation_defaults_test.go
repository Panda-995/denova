package config

import (
	"encoding/json"
	"reflect"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func TestGameCreationDefaultsPreserveExplicitEmptyAndScope(t *testing.T) {
	patch := json.RawMessage(`{"game_creation_defaults":{"narrative_style_id":"","event_package_ids":[],"default_background":{"mode":"none"}}}`)
	if err := ValidateWorkspaceSettingsPatch(patch); err != nil {
		t.Fatal(err)
	}
	next, err := ApplySettingsMergePatch(Settings{Theme: "dark"}, patch)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := toml.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	var restored Settings
	if err := toml.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, restored) {
		t.Fatalf("explicit choices changed after TOML round trip: %s", raw)
	}
	prepared := PrepareWorkspaceAgentSettingsForWrite(Settings{}, restored)
	if !reflect.DeepEqual(prepared.GameCreationDefaults, next.GameCreationDefaults) {
		t.Fatal("workspace filtering discarded defaults")
	}
	if _, err := PrepareUserSettingsForWrite(Settings{}, next); err == nil {
		t.Fatal("book defaults accepted in global settings")
	}
	updated, err := ApplySettingsMergePatch(next, json.RawMessage(`{"game_creation_defaults":{"image_preset_id":"picture"}}`))
	if err != nil || updated.Theme != "dark" || updated.GameCreationDefaults.NarrativeStyleID == nil || *updated.GameCreationDefaults.NarrativeStyleID != "" || updated.GameCreationDefaults.EventPackageIDs == nil {
		t.Fatalf("partial adoption erased user choices: %+v %v", updated, err)
	}
	updated.GameCreationDefaults.DefaultBackground = &GameDefaultBackground{Mode: "image", ItemID: "scene", AssetID: "picture"}
	cleared, err := ApplySettingsMergePatch(updated, json.RawMessage(`{"game_creation_defaults":{"default_background":{"mode":"none"}}}`))
	if err != nil || cleared.GameCreationDefaults.DefaultBackground.Mode != "none" || cleared.GameCreationDefaults.DefaultBackground.ItemID != "" {
		t.Fatalf("explicit clearing retained a previous background: %+v %v", cleared, err)
	}
}
