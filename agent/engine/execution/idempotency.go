package execution

import (
	"fmt"
	"strings"

	"github.com/alfredxw/denova/agent/internal/protocol"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ValidateIdempotencyKey applies the Agent command-identity boundary.
// Callers that require an explicit retry identity should validate before using
// it in a registry key, hash, or product record. Agent operations whose key is
// optional may continue to omit it and let the Session generate one.
func ValidateIdempotencyKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("%w: idempotency key is required for stable retry", agentschema.ErrInvalidInput)
	}
	if len(key) > protocol.MaxIdempotencyKeyBytes {
		return fmt.Errorf("%w: idempotency key exceeds %d bytes", agentschema.ErrInvalidInput, protocol.MaxIdempotencyKeyBytes)
	}
	return nil
}
