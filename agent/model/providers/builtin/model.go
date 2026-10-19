package builtin

import (
	"context"
	"errors"
	"sync"

	"github.com/alfredxw/denova/agent"
	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type modelDefinition struct {
	config    providers.ModelConfig
	configErr error

	once     sync.Once
	model    agentmodel.ToolCallingChatModel
	identity agentschema.CapabilityIdentity
	err      error
}

// Model declares a model from the built-in provider catalog. Agent resolves
// the protocol adapter and stable credential-free identity in agent.New.
func Model(config providers.ModelConfig) agentmodel.BaseChatModel {
	cloned, err := config.Clone()
	return &modelDefinition{config: cloned, configErr: err}
}

func (definition *modelDefinition) InitializeDefinition(ctx context.Context) error {
	if definition == nil {
		return errors.New("built-in Model Definition is nil")
	}
	definition.once.Do(func() {
		if definition.configErr != nil {
			definition.err = definition.configErr
			return
		}
		registry, err := NewRegistry()
		if err != nil {
			definition.err = err
			return
		}
		definition.model, definition.config, definition.err = registry.NewChatModelWithResolvedConfig(ctx, definition.config)
		if definition.err != nil {
			return
		}
		definition.identity, definition.err = providers.ModelIdentity(definition.config)
	})
	return definition.err
}

func (definition *modelDefinition) ModelIdentity() agentschema.CapabilityIdentity {
	if err := definition.InitializeDefinition(context.Background()); err != nil {
		return agentschema.CapabilityIdentity{}
	}
	return definition.identity
}

func (definition *modelDefinition) InputEstimator() agentmodel.InputEstimator {
	if err := definition.InitializeDefinition(context.Background()); err != nil {
		return agentmodel.InputEstimator{}
	}
	return definition.config.InputEstimator()
}

func (definition *modelDefinition) Generate(
	ctx context.Context,
	input []*agentschema.Message,
	options ...agentmodel.ModelOption,
) (*agentschema.Message, error) {
	if err := definition.InitializeDefinition(ctx); err != nil {
		return nil, err
	}
	return definition.model.Generate(ctx, input, options...)
}

func (definition *modelDefinition) Stream(
	ctx context.Context,
	input []*agentschema.Message,
	options ...agentmodel.ModelOption,
) (*agentstream.StreamReader[*agentschema.Message], error) {
	if err := definition.InitializeDefinition(ctx); err != nil {
		return nil, err
	}
	return definition.model.Stream(ctx, input, options...)
}

func (definition *modelDefinition) WithTools(tools []*agentschema.ToolInfo) (agentmodel.ToolCallingChatModel, error) {
	if err := definition.InitializeDefinition(context.Background()); err != nil {
		return nil, err
	}
	return definition.model.WithTools(tools)
}

var _ agent.DefinitionInitializer = (*modelDefinition)(nil)
var _ agentmodel.DefinitionModel = (*modelDefinition)(nil)
var _ agentmodel.ToolCallingChatModel = (*modelDefinition)(nil)
