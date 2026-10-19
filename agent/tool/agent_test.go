package tool

import (
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func testToolDefinition(tool Tool) ToolDefinition {
	return ToolDefinition{Tool: tool, Descriptor: ToolDescriptor{
		Source: ToolSourceRead, Execution: ToolExecutionParallelRead,
		MutationScope: ToolMutationNone, PostCheck: ToolPostCheckNone,
		Recovery: ToolRecoveryReadOnly, ResultProjection: agentschema.ToolResultBoundedModelContext,
		ResultRetention: agentschema.ToolResultDeferred,
		Steering:        SteeringFinishCurrent, MaxResultBytes: 1 << 20,
	}}
}
