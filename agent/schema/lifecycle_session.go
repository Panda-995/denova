package schema

import (
	"encoding/json"
)

func CloneGoalMutation(mutation *GoalMutation) *GoalMutation {
	if mutation == nil {
		return nil
	}
	cloned := *mutation
	cloned.Data = append(json.RawMessage(nil), mutation.Data...)
	return &cloned
}

func CloneHostData(data *HostData) *HostData {
	if data == nil {
		return nil
	}
	cloned := *data
	cloned.Data = append(json.RawMessage(nil), data.Data...)
	return &cloned
}

func CloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
