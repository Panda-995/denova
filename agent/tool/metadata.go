package tool

import (
	"encoding/json"
	"fmt"
)

// EncodeExecutionMetadata preserves display presentation alongside the execution
// descriptor without adding presentation to the descriptor's behavior identity.
func EncodeExecutionMetadata(descriptor ToolDescriptor) (json.RawMessage, error) {
	if descriptor.Execution == "" {
		return nil, nil
	}
	presentation, err := descriptor.Presentation.Normalize()
	if err != nil {
		return nil, fmt.Errorf("normalize tool live presentation: %w", err)
	}
	// Keep descriptor fields flat so existing event metadata readers can
	// continue decoding execution semantics while presentation remains a
	// separate, model-invisible concern.
	metadata, err := json.Marshal(struct {
		ToolDescriptor
		Presentation ToolPresentation `json:"presentation"`
	}{ToolDescriptor: descriptor, Presentation: presentation})
	if err != nil {
		return nil, fmt.Errorf("encode tool live metadata: %w", err)
	}
	return metadata, nil
}

func DecodeExecutionMetadata(metadata json.RawMessage) *ToolDescriptor {
	if len(metadata) == 0 {
		return nil
	}
	var decoded struct {
		ToolDescriptor
		Presentation ToolPresentation `json:"presentation"`
	}
	if err := json.Unmarshal(metadata, &decoded); err != nil || decoded.Execution == "" {
		return nil
	}
	presentation, err := decoded.Presentation.Normalize()
	if err != nil {
		return nil
	}
	decoded.ToolDescriptor.Presentation = presentation
	return &decoded.ToolDescriptor
}
