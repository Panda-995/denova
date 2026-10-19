package result

import (
	"context"
	"errors"
	"fmt"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// ToolResultProcessRequest is the immutable post-execution view supplied by
// modelToolLoop. Processors may project, materialize, and attach recovery metadata, but
// cannot change the already-approved tool arguments or execute the tool again.
type ToolResultProcessRequest struct {
	ToolName       string
	Arguments      string
	ExecutionID    string
	ProviderCallID string
	// BatchSize includes every call in the owning assistant response. A
	// processor can divide a batch budget deterministically even when calls
	// finish in parallel. Zero means a direct, single-result invocation.
	BatchSize  int
	Definition agenttool.ToolDefinitionSnapshot
	Result     agentschema.ToolResult
}

// ToolResultProcessor is the fixed post-tool result seam. It runs after the
// approved endpoint and all execution middleware, and before normalization,
// event publication, transcript persistence, cleanup, and compaction.
type ToolResultProcessor interface {
	Identity() agentschema.CapabilityIdentity
	Process(context.Context, ToolResultProcessRequest) (agentschema.ToolResult, error)
}

type toolResultProcessorChain struct {
	processors []ToolResultProcessor
	identity   agentschema.CapabilityIdentity
}

// ChainToolResultProcessors composes processors in source order. Every
// processor observes the previous processor's result; partial results are
// retained when a processor returns an error so a durable diagnostic can still
// be paired with the assistant tool call.
func ChainToolResultProcessors(processors ...ToolResultProcessor) (ToolResultProcessor, error) {
	resolved := make([]ToolResultProcessor, 0, len(processors))
	identities := make([]agentschema.CapabilityIdentity, 0, len(processors))
	for index, processor := range processors {
		if processor == nil {
			continue
		}
		identity := processor.Identity()
		if err := identity.Validate(fmt.Sprintf("ToolResultProcessor %d", index)); err != nil {
			return nil, err
		}
		resolved = append(resolved, processor)
		identities = append(identities, identity)
	}
	if len(resolved) == 0 {
		return nil, errors.New("ToolResultProcessor chain is empty")
	}
	hash, err := agentschema.HashCanonical(identities)
	if err != nil {
		return nil, err
	}
	return &toolResultProcessorChain{
		processors: resolved,
		identity: agentschema.CapabilityIdentity{
			Kind: "tool_result_processor.chain", Version: 1, ConfigHash: hash,
		},
	}, nil
}

func (chain *toolResultProcessorChain) Identity() agentschema.CapabilityIdentity {
	if chain == nil {
		return agentschema.CapabilityIdentity{}
	}
	return chain.identity
}

func (chain *toolResultProcessorChain) Process(ctx context.Context, request ToolResultProcessRequest) (agentschema.ToolResult, error) {
	if chain == nil {
		return request.Result, errors.New("ToolResultProcessor chain is nil")
	}
	result := request.Result
	for _, processor := range chain.processors {
		request.Result = result
		processed, err := processor.Process(ctx, request)
		result = processed
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func IdentityOfToolResultProcessor(processor ToolResultProcessor) agentschema.CapabilityIdentity {
	if processor == nil {
		return agentschema.CapabilityIdentity{Kind: "tool_result_processor.none", Version: 1}
	}
	return processor.Identity()
}
