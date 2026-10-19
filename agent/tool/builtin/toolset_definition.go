package builtin

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/alfredxw/denova/agent"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// definitionToolset delays construction until Agent accepts its Definition.
// This keeps a complete capability composition declarative while preserving
// ordinary errors instead of panicking from convenience constructors.
type definitionToolset struct {
	build func(context.Context) (agenttool.Toolset, error)

	once    sync.Once
	toolset agenttool.Toolset
	err     error
}

func defineToolset(build func(context.Context) (agenttool.Toolset, error)) agenttool.Toolset {
	return &definitionToolset{build: build}
}

func (definition *definitionToolset) InitializeDefinition(ctx context.Context) error {
	if definition == nil {
		return errors.New("toolset Definition is nil")
	}
	definition.once.Do(func() {
		if definition.build == nil {
			definition.err = errors.New("toolset Definition builder is nil")
			return
		}
		definition.toolset, definition.err = definition.build(ctx)
		if definition.err == nil && definition.toolset == nil {
			definition.err = errors.New("toolset Definition returned nil")
		}
	})
	return definition.err
}

func (definition *definitionToolset) Identity() agentschema.CapabilityIdentity {
	if err := definition.InitializeDefinition(context.Background()); err != nil {
		return agentschema.CapabilityIdentity{}
	}
	return definition.toolset.Identity()
}

func (definition *definitionToolset) PrepareTools(
	ctx context.Context,
	request agenttool.ToolRequest,
) ([]agenttool.ToolDefinition, error) {
	if err := definition.InitializeDefinition(ctx); err != nil {
		return nil, err
	}
	return definition.toolset.PrepareTools(ctx, request)
}

func initializeToolset(ctx context.Context, index int, toolset agenttool.Toolset) error {
	if toolset == nil {
		return nil
	}
	if initializer, ok := toolset.(agent.DefinitionInitializer); ok {
		if err := initializer.InitializeDefinition(ctx); err != nil {
			return fmt.Errorf("Toolset[%d]: %w", index, err)
		}
	}
	return nil
}

var _ agent.DefinitionInitializer = (*definitionToolset)(nil)
var _ agenttool.Toolset = (*definitionToolset)(nil)
