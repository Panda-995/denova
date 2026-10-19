package execution

import (
	"errors"
	"strings"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestValidateIdempotencyKeyExposesProviderNeutralAdmissionBoundary(t *testing.T) {
	t.Parallel()

	if err := ValidateIdempotencyKey(" caller-owned-retry "); err != nil {
		t.Fatalf("valid idempotency key: %v", err)
	}
	for _, key := range []string{"", " \t\n", strings.Repeat("x", 4<<10+1)} {
		if err := ValidateIdempotencyKey(key); !errors.Is(err, agentschema.ErrInvalidInput) {
			t.Fatalf("ValidateIdempotencyKey(%d bytes) error = %v, want ErrInvalidInput", len(key), err)
		}
	}
}
