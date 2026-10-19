package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCallNestedToolsFailsClosedOutsideExecution(t *testing.T) {
	if _, err := CallNestedTools(context.Background(), []NestedToolCall{{Name: "read", Arguments: json.RawMessage(`{}`)}}); err == nil {
		t.Fatal("expected unavailable nested executor")
	}
}
