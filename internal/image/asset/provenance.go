package asset

import (
	"fmt"

	"denova/internal/assetstore"
)

// ProvenanceStorage identifies the existing owner of generation details.
// Tools use their canonical journal; direct UI calls need optional file metadata.
type ProvenanceStorage uint8

const (
	ProvenanceJournal ProvenanceStorage = iota
	ProvenanceDirectory
)

type generationMeta struct {
	Prompt        string `json:"prompt"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
	ImagePresetID string `json:"image_preset_id,omitempty"`
	ProfileID     string `json:"profile_id"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	Size          string `json:"size,omitempty"`
	Quality       string `json:"quality,omitempty"`
	OutputFormat  string `json:"output_format,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
}

func (storage ProvenanceStorage) file(name string, data []byte, detail generationMeta) (assetstore.File, error) {
	file := assetstore.File{Path: name, Data: data}
	switch storage {
	case ProvenanceJournal:
	case ProvenanceDirectory:
		file.Generation = detail
	default:
		return assetstore.File{}, fmt.Errorf("unknown generation provenance storage: %d", storage)
	}
	return file, nil
}
