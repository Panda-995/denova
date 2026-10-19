package model

import (
	"context"
	"errors"
	"sync"

	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type blockingModelStart struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (model *blockingModelStart) Generate(context.Context, []*agentschema.Message, ...ModelOption) (*agentschema.Message, error) {
	return nil, errors.New("unexpected Generate")
}

func (model *blockingModelStart) Stream(context.Context, []*agentschema.Message, ...ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	model.once.Do(func() { close(model.started) })
	<-model.release
	return agentstream.StreamReaderFromArray([]*agentschema.Message{agentschema.AssistantMessage("late", nil)}), nil
}

type blockingGenerateModel struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (model *blockingGenerateModel) Generate(context.Context, []*agentschema.Message, ...ModelOption) (*agentschema.Message, error) {
	model.once.Do(func() { close(model.started) })
	<-model.release
	return agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "1", Type: "function", Function: agentschema.FunctionCall{Name: "never", Arguments: `{}`}}}), nil
}

func (model *blockingGenerateModel) Stream(context.Context, []*agentschema.Message, ...ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	return nil, errors.New("unexpected Stream")
}
