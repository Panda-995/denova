package resourceexchange

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"denova/config"
	"denova/internal/book"
)

func validateCreator(raw []byte) error {
	if len(raw) > config.MaxAgentContextFragmentBytes || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return ErrCreatorInvalid
	}
	content := book.ProjectInstructionsContent(book.CreatorInstructionsHeading, string(raw))
	if len(bytes.TrimSpace(raw)) == 0 || len(content) > config.MaxAgentContextFragmentBytes {
		return ErrCreatorInvalid
	}
	return nil
}

// Validate against the destination's existing context policy, not a new package
// setting. The file remains project-owned and is injected by the normal runtime.
func (s *Service) validateCreatorForProject(projectID string, raw []byte) error {
	if err := validateCreator(raw); err != nil {
		return err
	}
	_, layout, err := s.registry.Resolve(projectID, true)
	if err != nil {
		return err
	}
	settings, err := config.LoadLayeredWithGlobalAt(s.root, layout.ContentRoot, layout.ConfigPath(), config.Settings{})
	if err != nil {
		return err
	}
	content := book.ProjectInstructionsContent(book.CreatorInstructionsHeading, string(raw))
	for kind, policy := range settings.ResolvedAgentContexts {
		if len(content) > policy.MaxFragmentBytes {
			return fmt.Errorf("%w: %s limit is %d bytes", ErrCreatorInvalid, kind, policy.MaxFragmentBytes)
		}
	}
	return nil
}
