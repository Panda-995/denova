package lifecycle

import (
	"encoding/json"
	"strings"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestToolPresentationIsDisplayOnlyAndRoundTripsThroughLifecycleMetadata(t *testing.T) {
	descriptor := validDescriptorForScope(agenttool.ToolMutationNone)
	descriptor.Presentation = agenttool.ToolPresentation{Call: agenttool.ToolPresentationSearch}

	encodedDescriptor, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedDescriptor), "presentation") {
		t.Fatalf("display presentation entered descriptor identity JSON: %s", encodedDescriptor)
	}

	metadata, err := agenttool.EncodeExecutionMetadata(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	projected := agenttool.DecodeExecutionMetadata(metadata)
	if projected == nil || projected.Presentation.Call != agenttool.ToolPresentationSearch || projected.Presentation.Result != agenttool.ToolPresentationSearch {
		t.Fatalf("lifecycle presentation = %#v, want uniform search", projected)
	}
}

func validDescriptorForScope(scope agenttool.ToolMutationScope) agenttool.ToolDescriptor {
	descriptor := agenttool.ToolDescriptor{
		Source:           agenttool.ToolSourceOther,
		Execution:        agenttool.ToolExecutionParallelRead,
		MutationScope:    scope,
		PostCheck:        agenttool.ToolPostCheckNone,
		Recovery:         agenttool.ToolRecoveryReadOnly,
		ResultProjection: agentschema.ToolResultBoundedModelContext,
		ResultRetention:  agentschema.ToolResultDeferred,
		Steering:         agenttool.SteeringFinishCurrent,
		MaxResultBytes:   1024,
	}
	switch scope {
	case agenttool.ToolMutationWorkspace, agenttool.ToolMutationExternal:
		descriptor.Execution = agenttool.ToolExecutionWorkspaceExclusive
	case agenttool.ToolMutationSession:
		descriptor.Execution = agenttool.ToolExecutionSessionExclusive
	case agenttool.ToolMutationConfig:
		descriptor.Execution = agenttool.ToolExecutionConfigExclusive
	}
	return descriptor
}
