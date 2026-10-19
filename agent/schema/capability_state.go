package schema

import (
	"encoding/json"
)

func CloneRawStateMap(states map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(states))
	for name, state := range states {
		cloned[name] = append(json.RawMessage(nil), state...)
	}
	return cloned
}
