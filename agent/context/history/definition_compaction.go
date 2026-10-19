package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

const (
	maxCompactionContextDataBytes = 8 << 20
)

func ValidateCompactionContextData(data *agentschema.HostData) error {
	if data == nil {
		return nil
	}
	if strings.TrimSpace(data.Type) == "" || data.Version == 0 || !json.Valid(data.Data) {
		return errors.New("Compaction ContextData requires Type, Version, and valid JSON Data")
	}
	if len(data.Data) > maxCompactionContextDataBytes {
		return fmt.Errorf("Compaction ContextData exceeds %d bytes", maxCompactionContextDataBytes)
	}
	return nil
}

// compactionExecutionPlan adds authenticated raw coverage to a semantic plan.
type CompactionExecutionPlan struct {
	CompactionPlan
	SourceFrom int
	SourceTo   int
}

func compactionMessagesBytes(messages []*agentschema.Message) int {
	encoded, _ := json.Marshal(messages)
	return len(encoded)
}
