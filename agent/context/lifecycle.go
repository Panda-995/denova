package context

import (
	"errors"
	"fmt"
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

const DefaultLifecycleHardLimit = 64 << 10

// ExportLifecycleFragments is the single bridge from the standalone bounded
// Assembler to Agent.ContextSource's lifecycle vocabulary. Assembler remains a
// pure composition utility; ContextSource remains the only dynamic extension
// seam and therefore the only place that needs a CapabilityIdentity.
//
// Final-user-prefix fragments are exported as audit evidence because Result
// already contains their localized rendering. A host that uses that exact
// rendered request should append one ContextFinalUserMessage fragment. Leading
// fragments are exported with their exact rendered message so model input does
// not change between assembly and lifecycle execution.
func ExportLifecycleFragments(result Result) ([]agentschema.ContextFragment, error) {
	leading := renderedLeadingMessages(result.Messages)
	leadingIndex := 0
	fragments := make([]agentschema.ContextFragment, 0, len(result.Fragments))
	for index, fragment := range result.Fragments {
		if !fragment.Included || strings.TrimSpace(fragment.Content) == "" {
			continue
		}
		resource := strings.TrimSpace(fragment.ID)
		if resource == "" {
			resource = fmt.Sprintf("%s:%d", strings.TrimSpace(fragment.Source), index+1)
		}
		content := fragment.Content
		placement := agentschema.ContextAuditOnly
		stability := agentschema.ContextAudit
		rendering := agentschema.ContextRenderAttributed
		role := agentschema.RoleType("")
		if fragment.Placement == PlacementLeadingMessage {
			if leadingIndex >= len(leading) {
				return nil, errors.New("context assembly is missing a rendered leading message")
			}
			content = leading[leadingIndex].Content
			leadingIndex++
			placement = agentschema.ContextLeadingMessage
			stability = agentschema.ContextStablePrefix
			rendering = agentschema.ContextRenderVerbatim
			role = agentschema.User
		}
		if fragment.Stability == agentschema.ContextSessionState {
			placement = agentschema.ContextStateMessage
			stability = agentschema.ContextSessionState
			rendering = agentschema.ContextRenderVerbatim
			role = ""
		}
		hardLimit := max(DefaultLifecycleHardLimit, fragment.Limit, len(content))
		fragments = append(fragments, agentschema.ContextFragment{
			Source: strings.TrimSpace(fragment.Source), Purpose: strings.TrimSpace(fragment.Purpose),
			Resource: resource, Revision: fragment.Hash, StateID: fragment.StateID, Stability: stability, Placement: placement,
			Rendering: rendering, Role: role, Content: content, HardLimit: hardLimit,
		})
	}
	if leadingIndex != len(leading) {
		return nil, errors.New("context assembly contains an unaccounted rendered leading message")
	}
	return fragments, nil
}

func renderedLeadingMessages(messages []*agentschema.Message) []*agentschema.Message {
	result := make([]*agentschema.Message, 0)
	for _, message := range messages {
		if message == nil || message.Extra == nil ||
			message.Extra[MessageExtraPlacement] != string(PlacementLeadingMessage) {
			break
		}
		result = append(result, message)
	}
	return result
}
