package book

import "strings"

const (
	// AgentInstructionsFileName contains workspace-level project and workflow instructions.
	AgentInstructionsFileName = "AGENTS.md"
	// CreatorFileName contains workspace-level creative instructions.
	CreatorFileName = "CREATOR.md"
	// CreatorInstructionsHeading labels the shared creative instruction fragment.
	CreatorInstructionsHeading = "# Creative instructions"
)

// ProjectInstructionsContent is the shared instruction envelope used for context
// injection and import capacity checks. It never truncates user instructions.
func ProjectInstructionsContent(heading, raw string) string {
	return heading + "\n\n" + strings.TrimSpace(raw) + "\n\nA later explicit user request takes precedence."
}
