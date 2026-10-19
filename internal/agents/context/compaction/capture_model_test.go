package compaction

import (
	"context"
	"io"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type compactionForkCaptureModel struct {
	response *agentschema.Message
	inputs   [][]*agentschema.Message
	options  []*agentmodel.Options
	streams  int
	requests int
}

func (model *compactionForkCaptureModel) Generate(_ context.Context, input []*agentschema.Message, opts ...agentmodel.ModelOption) (*agentschema.Message, error) {
	model.capture(input, opts)
	if model.response == nil {
		return nil, io.EOF
	}
	return model.response.Clone(), nil
}

func (model *compactionForkCaptureModel) Stream(_ context.Context, input []*agentschema.Message, opts ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	model.capture(input, opts)
	model.streams++
	if model.response == nil {
		return agentstream.StreamReaderFromArray([]*agentschema.Message{}), nil
	}
	return agentstream.StreamReaderFromArray([]*agentschema.Message{model.response.Clone()}), nil
}

func (model *compactionForkCaptureModel) capture(input []*agentschema.Message, opts []agentmodel.ModelOption) {
	model.requests++
	messages := make([]*agentschema.Message, len(input))
	for index, message := range input {
		if message != nil {
			messages[index] = message.Clone()
		}
	}
	model.inputs = append(model.inputs, messages)
	model.options = append(model.options, agentmodel.GetCommonOptions(nil, opts...))
}

func cloneMessages(messages []*agentschema.Message) []*agentschema.Message {
	result := make([]*agentschema.Message, len(messages))
	for index, message := range messages {
		result[index] = message.Clone()
	}
	return result
}
