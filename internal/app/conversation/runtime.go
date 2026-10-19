// Package conversation owns the shared project conversation runtime policy
// used by foreground Writing and independent AgentChat sessions.
package conversationapp

import (
	"context"
	"denova/config"
	"denova/internal/agents/execution"
	"denova/internal/agents/prompts"
	"denova/internal/agents/session"
	"denova/internal/book"
	projectdomain "denova/internal/project"
)

// Runtime is an immutable project adapter snapshot. Request preparation may
// replace Config and IDETeller in the returned value but never re-resolves host
// state, preserving one project generation throughout admission.
type Runtime struct {
	// PageInstructions is supplied by the application host, never by a plugin frame.
	PageInstructions func(context.Context, string, string, string) (string, error)

	ProjectID        string
	ProjectType      projectdomain.Type
	ProjectStore     string
	AgentKind        string
	Session          *session.Session
	State            *book.State
	BookService      *book.Service
	ExecutionRuntime *execution.Runtime
	Workspace        string
	VersionService   *book.VersionService
	Config           config.Config
	IDETeller        prompts.IDEStoryTeller
}
