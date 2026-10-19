package schema

import (
	agentsession "github.com/alfredxw/denova/agent/session"
)

// CacheKeyGenerator derives an opaque, stable provider cache-routing key.
type CacheKeyGenerator func(agentsession.Key) (string, error)
