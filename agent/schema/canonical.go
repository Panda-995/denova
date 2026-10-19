package schema

import (
	"encoding/json"
)

type Effect struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}
