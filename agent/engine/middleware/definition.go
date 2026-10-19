package middleware

import (
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// IdentifiedMiddleware gives behavior-changing middleware a stable identity
// for Definition validation and traceability.
type IdentifiedMiddleware interface {
	Middleware
	Identity() agentschema.CapabilityIdentity
}

type identifiedMiddleware struct {
	Middleware
	identity agentschema.CapabilityIdentity
}

func (middleware identifiedMiddleware) Identity() agentschema.CapabilityIdentity {
	return middleware.identity
}

func (middleware identifiedMiddleware) unwrap() Middleware { return middleware.Middleware }

// IdentifyMiddleware associates a stable behavior identity with an existing
// Middleware. It is useful for host-owned middleware whose concrete type is
// intentionally unaware of Agent Sessions.
func IdentifyMiddleware(middleware Middleware, identity agentschema.CapabilityIdentity) IdentifiedMiddleware {
	if middleware == nil {
		return nil
	}
	return identifiedMiddleware{Middleware: middleware, identity: identity}
}

// MiddlewareImplementation returns the concrete host middleware beneath an
// identity decorator. It is intended for code that still needs concrete-type
// diagnostics; Agent execution itself never unwraps middleware.
func MiddlewareImplementation(middleware Middleware) Middleware {
	for middleware != nil {
		wrapped, ok := middleware.(interface{ unwrap() Middleware })
		if !ok {
			return middleware
		}
		next := wrapped.unwrap()
		if next == middleware {
			return middleware
		}
		middleware = next
	}
	return nil
}
