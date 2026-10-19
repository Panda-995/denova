// Package webfetch provides the optional provider-neutral web_fetch Toolset.
package webfetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type Request struct {
	URL      string `json:"url"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type Response struct {
	URL         string                       `json:"url"`
	Status      int                          `json:"status"`
	ContentType string                       `json:"content_type,omitempty"`
	Title       string                       `json:"title,omitempty"`
	Content     string                       `json:"content,omitempty"`
	Truncated   bool                         `json:"truncated,omitempty"`
	Artifact    *agentschema.ToolArtifactRef `json:"artifact,omitempty"`
}

type Provider interface {
	Identity() agentschema.CapabilityIdentity
	Fetch(context.Context, Request) (Response, error)
}

type input struct {
	Requests []Request `json:"requests" jsonschema:"minItems=1,maxItems=16"`
}

type itemResult struct {
	Index    int       `json:"index"`
	Request  Request   `json:"request"`
	Response *Response `json:"response,omitempty"`
	Error    string    `json:"error,omitempty"`
}

func New(provider Provider) (agenttool.Toolset, error) {
	if provider == nil {
		return nil, errors.New("web_fetch requires a Provider")
	}
	identity := provider.Identity()
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		return nil, errors.New("web_fetch Provider requires a stable Identity")
	}
	tool, err := agenttool.InferTool("web_fetch", "Fetch and extract web resources through the configured network policy. Batch requests return independent outcomes.", func(ctx context.Context, request input) (agentschema.ToolResult, error) {
		if len(request.Requests) == 0 || len(request.Requests) > 16 {
			return agentschema.ToolResult{}, errors.New("web_fetch requires 1..16 requests")
		}
		results := make([]itemResult, len(request.Requests))
		artifacts := make([]agentschema.ToolArtifactRef, 0)
		for index, item := range request.Requests {
			results[index] = itemResult{Index: index, Request: item}
			item.URL = strings.TrimSpace(item.URL)
			if item.URL == "" || len(item.URL) > 64<<10 || item.MaxBytes < 0 || item.MaxBytes > 32<<20 {
				results[index].Error = "invalid web fetch request"
				continue
			}
			response, fetchErr := provider.Fetch(ctx, item)
			if fetchErr != nil {
				results[index].Error = fetchErr.Error()
				continue
			}
			results[index].Response = &response
			if response.Artifact != nil {
				artifacts = append(artifacts, *response.Artifact)
			}
		}
		encoded, err := json.Marshal(struct {
			Results []itemResult `json:"results"`
		}{results})
		if err != nil {
			return agentschema.ToolResult{}, fmt.Errorf("encode web_fetch result: %w", err)
		}
		result := agentschema.TextToolResult(string(encoded))
		result.Artifacts = artifacts
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return agenttool.StaticToolsIdentified(agentschema.CapabilityIdentity{
		Kind: "tools.plugin.webfetch", Version: 1, ConfigHash: hex.EncodeToString(digest[:]),
	}, agenttool.ToolDefinition{Tool: tool, Descriptor: agenttool.ToolDescriptor{
		Source: agenttool.ToolSourceWeb, Capability: "web_fetch", Execution: agenttool.ToolExecutionParallelRead,
		MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone,
		Recovery: agenttool.ToolRecoveryReadOnly, ResultRecoveryKind: agentschema.ToolResultRecoveryRefetch,
		ResultProjection: agentschema.ToolResultBoundedModelContext, ResultRetention: agentschema.ToolResultEagerCandidate,
		Steering: agenttool.SteeringInterruptibleWait, MaxResultBytes: 8 << 20,
		Presentation: agenttool.UniformToolPresentation(agenttool.ToolPresentationWeb),
	}})
}
