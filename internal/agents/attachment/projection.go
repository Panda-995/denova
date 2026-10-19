package attachment

import (
	"fmt"
	"path/filepath"
	"strings"

	"denova/internal/portablepath"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// Descriptor exposes a persisted attachment's preview identity without its
// storage path or host-specific projection. It is shared by live and history UI.
type Descriptor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

// Descriptors projects attachment metadata for browser-facing responses only.
func Descriptors(files []agentschema.Attachment) []Descriptor {
	if len(files) == 0 {
		return nil
	}
	result := make([]Descriptor, 0, len(files))
	for _, file := range files {
		result = append(result, Descriptor{ID: file.ID, Name: file.Name, MediaType: file.MediaType, Size: file.Size})
	}
	return result
}

// ProjectFiles resolves canonical copies only within their owning conversation.
// The returned paths are for the current host and are never serialized.
func ProjectFiles(stateRoot string, scope Scope, files []agentschema.Attachment) ([]agentschema.Attachment, error) {
	projected := append([]agentschema.Attachment(nil), files...)
	prefix := "attachments/v1/" + scopeKey(scope) + "/"
	for index := range projected {
		file := &projected[index]
		if err := portablepath.Validate(file.Path); err != nil {
			return nil, err
		}
		if stateRoot == "" || !strings.HasPrefix(file.Path, prefix) {
			return nil, fmt.Errorf("attachment %q is outside its conversation", file.Name)
		}
		file.RuntimePath = filepath.Join(stateRoot, filepath.FromSlash(file.Path))
	}
	return projected, nil
}
