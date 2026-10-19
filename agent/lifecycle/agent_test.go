package lifecycle

import (
	"context"
	"errors"
	"sync"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type scriptedModelResponse struct {
	message *agentschema.Message
	chunks  []*agentschema.Message
	err     error
}

type scriptedModel struct {
	mu         sync.Mutex
	responses  []scriptedModelResponse
	inputs     [][]*agentschema.Message
	toolCounts []int
}

func (model *scriptedModel) next(input []*agentschema.Message, opts []agentmodel.ModelOption) (scriptedModelResponse, error) {
	model.mu.Lock()
	defer model.mu.Unlock()
	model.inputs = append(model.inputs, agentschema.CloneMessages(input))
	model.toolCounts = append(model.toolCounts, len(agentmodel.GetCommonOptions(nil, opts...).Tools))
	if len(model.responses) == 0 {
		return scriptedModelResponse{}, errors.New("scripted model exhausted")
	}
	response := model.responses[0]
	model.responses = model.responses[1:]
	return response, nil
}

func (model *scriptedModel) Generate(_ context.Context, input []*agentschema.Message, opts ...agentmodel.ModelOption) (*agentschema.Message, error) {
	response, err := model.next(input, opts)
	if err != nil {
		return nil, err
	}
	if response.err != nil {
		return nil, response.err
	}
	if response.message != nil {
		return response.message.Clone(), nil
	}
	return agentschema.ConcatMessages(agentschema.CloneMessages(response.chunks))
}

func (model *scriptedModel) Stream(_ context.Context, input []*agentschema.Message, opts ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	response, err := model.next(input, opts)
	if err != nil {
		return nil, err
	}
	if response.err != nil {
		return nil, response.err
	}
	if response.message != nil {
		return agentstream.StreamReaderFromArray([]*agentschema.Message{response.message.Clone()}), nil
	}
	return agentstream.StreamReaderFromArray(agentschema.CloneMessages(response.chunks)), nil
}

func (model *scriptedModel) capturedInputs() [][]*agentschema.Message {
	model.mu.Lock()
	defer model.mu.Unlock()
	result := make([][]*agentschema.Message, len(model.inputs))
	for index := range model.inputs {
		result[index] = agentschema.CloneMessages(model.inputs[index])
	}
	return result
}

type functionTool struct {
	name string
	run  func(context.Context, string) (string, error)
}

func (tool *functionTool) Info(context.Context) (*agentschema.ToolInfo, error) {
	return &agentschema.ToolInfo{Name: tool.name, Desc: tool.name}, nil
}

func (tool *functionTool) Run(ctx context.Context, arguments string, _ ...agenttool.ToolOption) (agentschema.ToolResult, error) {
	content, err := tool.run(ctx, arguments)
	if err != nil {
		return agentschema.ToolResult{}, err
	}
	return agentschema.TextToolResult(content), nil
}

func testToolDefinition(tool agenttool.Tool) agenttool.ToolDefinition {
	return agenttool.ToolDefinition{Tool: tool, Descriptor: agenttool.ToolDescriptor{
		Source: agenttool.ToolSourceRead, Execution: agenttool.ToolExecutionParallelRead,
		MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone,
		Recovery: agenttool.ToolRecoveryReadOnly, ResultProjection: agentschema.ToolResultBoundedModelContext,
		ResultRetention: agentschema.ToolResultDeferred,
		Steering:        agenttool.SteeringFinishCurrent, MaxResultBytes: 1 << 20,
	}}
}
