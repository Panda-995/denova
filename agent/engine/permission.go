package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

type permissionMiddleware struct {
	*agentmiddleware.BaseMiddleware
	policy      agentpermission.PermissionPolicy
	session     agentschema.SessionView
	run         agentschema.RunView
	attachments []agentschema.Attachment
}

func (middleware *permissionMiddleware) WrapToolCall(
	_ context.Context,
	endpoint agentmiddleware.ToolCallEndpoint,
	tool *agentmiddleware.ToolContext,
) (agentmiddleware.ToolCallEndpoint, error) {
	if middleware == nil || middleware.policy == nil || endpoint == nil || tool == nil {
		return nil, errors.New("Permission middleware is incomplete")
	}
	return func(ctx context.Context, arguments string, options ...agenttool.ToolOption) (agentschema.ToolResult, error) {
		request := agentpermission.PermissionRequest{
			Session: middleware.session, Run: middleware.run,
			CallID: tool.ExecutionID, Tool: tool.Name, Arguments: json.RawMessage(arguments),
			Descriptor: tool.Definition.Descriptor, Attachments: agentschema.CloneAttachments(middleware.attachments),
		}
		decision, err := middleware.policy.Evaluate(ctx, request)
		if err != nil {
			return agentschema.ToolResult{}, fmt.Errorf("evaluate permission for tool %q: %w", tool.Name, err)
		}
		switch decision.Kind {
		case agentpermission.PermissionAllow:
			return endpoint(ctx, arguments, options...)
		case agentpermission.PermissionBlock:
			return agentschema.ToolResult{}, fmt.Errorf("%w: tool %q", agentschema.ErrPermissionDenied, tool.Name)
		case agentpermission.PermissionAsk:
			if err := agentinteraction.ValidateLocalizedText(decision.Reason); err != nil {
				return agentschema.ToolResult{}, fmt.Errorf("permission reason for tool %q: %w", tool.Name, err)
			}
			interactionID := "permission-" + strings.TrimSpace(tool.ExecutionID)
			if strings.TrimSpace(tool.ExecutionID) == "" {
				return agentschema.ToolResult{}, errors.New("Permission requires a durable tool execution ID")
			}
			presentation := agentpermission.PermissionPresentation(request, decision)
			presentation.ToolDefinitionHash, err = agentschema.HashCanonical(tool.Definition)
			if err != nil {
				return agentschema.ToolResult{}, fmt.Errorf("capture permission tool %q: %w", tool.Name, err)
			}
			resolution, err := agentinteraction.RequestInteraction(ctx, agentinteraction.InteractionRequest{
				ID: interactionID, Kind: agentinteraction.InteractionPermission,
				Permission: &presentation,
			})
			if err != nil {
				return agentschema.ToolResult{}, err
			}
			if resolution.Cancelled || resolution.Permission == agentinteraction.PermissionDeny {
				return agentschema.ToolResult{}, fmt.Errorf("%w: tool %q", agentschema.ErrPermissionDenied, tool.Name)
			}
			if resolution.Permission != agentinteraction.PermissionAllowOnce && resolution.Permission != agentinteraction.PermissionRemember {
				return agentschema.ToolResult{}, errors.New("Permission Interaction returned an invalid resolution")
			}
			return endpoint(ctx, arguments, options...)
		default:
			return agentschema.ToolResult{}, fmt.Errorf("Permission Policy returned unsupported decision %q", decision.Kind)
		}
	}, nil
}
