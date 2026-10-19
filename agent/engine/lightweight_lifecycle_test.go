package engine

import (
	"context"
	"errors"
	"sync"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type lifecycleModel struct {
	mu        sync.Mutex
	responses []*agentschema.Message
	inputs    [][]*agentschema.Message
	options   []*agentmodel.Options
}

func (model *lifecycleModel) Generate(_ context.Context, input []*agentschema.Message, options ...agentmodel.ModelOption) (*agentschema.Message, error) {
	return model.next(input, options...)
}

func (model *lifecycleModel) Stream(_ context.Context, input []*agentschema.Message, options ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	message, err := model.next(input, options...)
	if err != nil {
		return nil, err
	}
	return agentstream.StreamReaderFromArray([]*agentschema.Message{message}), nil
}

func (model *lifecycleModel) next(input []*agentschema.Message, options ...agentmodel.ModelOption) (*agentschema.Message, error) {
	model.mu.Lock()
	defer model.mu.Unlock()
	model.inputs = append(model.inputs, agentschema.CloneMessages(input))
	model.options = append(model.options, agentmodel.GetCommonOptions(&agentmodel.Options{}, options...))
	if len(model.responses) == 0 {
		return nil, errors.New("lifecycle model exhausted")
	}
	message := agentschema.CloneMessage(model.responses[0])
	model.responses = model.responses[1:]
	return message, nil
}

func (model *lifecycleModel) calls() [][]*agentschema.Message {
	model.mu.Lock()
	defer model.mu.Unlock()
	result := make([][]*agentschema.Message, len(model.inputs))
	for index := range model.inputs {
		result[index] = agentschema.CloneMessages(model.inputs[index])
	}
	return result
}
