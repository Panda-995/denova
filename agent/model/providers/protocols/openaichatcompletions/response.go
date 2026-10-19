package openaichatcompletions

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/alfredxw/denova/agent/model/providers"
	agentschema "github.com/alfredxw/denova/agent/schema"
	sdk "github.com/openai/openai-go/v3"
)

const (
	// ExtraKeyRequestID stores the transport request ID as a plain string.
	ExtraKeyRequestID         = "openai-request-id"
	ExtraKeyProvider          = "provider"
	ExtraKeyProtocol          = "protocol"
	ExtraKeyResponseID        = "response_id"
	ExtraKeyModel             = "model"
	ExtraKeyCreated           = "created"
	ExtraKeyServiceTier       = "service_tier"
	ExtraKeySystemFingerprint = "system_fingerprint"
)

func responseMessage(response *sdk.ChatCompletion, rawResponse *http.Response, config providers.ModelConfig, reasoningField string) (*agentschema.Message, error) {
	choice, found := responseChoice(response.Choices)
	if !found {
		return nil, nil
	}
	message := &agentschema.Message{
		Role:             responseRole(string(choice.Message.Role)),
		Content:          choice.Message.Content,
		ReasoningContent: rawReasoningContent(choice.Message.RawJSON(), reasoningField),
		ToolCalls:        responseToolCalls(choice.Message.ToolCalls),
		ResponseMeta: &agentschema.ResponseMeta{
			FinishReason: choice.FinishReason,
		},
		Extra: responseExtra(
			rawResponse,
			string(config.Provider),
			response.ID,
			response.Model,
			response.Created,
			string(response.ServiceTier),
			response.SystemFingerprint,
		),
	}
	if response.JSON.Usage.Valid() {
		message.ResponseMeta.Usage = responseUsage(response.Usage)
	}
	var continuation continuationState
	if err := continuation.add(choice.Message.RawJSON()); err != nil {
		return nil, err
	}
	retained, err := continuation.message(config)
	if err != nil {
		return nil, err
	}
	if retained != nil {
		message.Extra[providers.ExtraKeyContinuation] = retained.Extra[providers.ExtraKeyContinuation]
	}
	return message, nil
}

func responseChoice(choices []sdk.ChatCompletionChoice) (sdk.ChatCompletionChoice, bool) {
	for _, choice := range choices {
		if choice.Index == 0 {
			return choice, true
		}
	}
	return sdk.ChatCompletionChoice{}, false
}

func streamChoice(choices []sdk.ChatCompletionChunkChoice) (sdk.ChatCompletionChunkChoice, bool) {
	for _, choice := range choices {
		if choice.Index == 0 {
			return choice, true
		}
	}
	return sdk.ChatCompletionChunkChoice{}, false
}

func streamMessage(chunk sdk.ChatCompletionChunk, rawResponse *http.Response, includeMetadata bool, provider, reasoningField string) (*agentschema.Message, bool) {
	choice, hasChoice := streamChoice(chunk.Choices)
	hasUsage := chunk.JSON.Usage.Valid()
	if !hasChoice && !hasUsage {
		return nil, false
	}

	message := &agentschema.Message{Role: agentschema.Assistant}
	if hasChoice {
		message.Role = responseRole(choice.Delta.Role)
		message.Content = choice.Delta.Content
		message.ReasoningContent = rawReasoningContent(choice.Delta.RawJSON(), reasoningField)
		message.ToolCalls = streamToolCalls(choice.Delta.ToolCalls)
		if choice.FinishReason != "" {
			message.ResponseMeta = &agentschema.ResponseMeta{FinishReason: choice.FinishReason}
		}
	}
	if hasUsage {
		if message.ResponseMeta == nil {
			message.ResponseMeta = &agentschema.ResponseMeta{}
		}
		message.ResponseMeta.Usage = responseUsage(chunk.Usage)
	}
	if includeMetadata {
		message.Extra = responseExtra(
			rawResponse,
			provider,
			chunk.ID,
			chunk.Model,
			chunk.Created,
			string(chunk.ServiceTier),
			chunk.SystemFingerprint,
		)
	}
	return message, true
}

func responseRole(role string) agentschema.RoleType {
	if role == "" {
		return agentschema.Assistant
	}
	return agentschema.RoleType(role)
}

func responseToolCalls(calls []sdk.ChatCompletionMessageToolCallUnion) []agentschema.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	result := make([]agentschema.ToolCall, 0, len(calls))
	for _, call := range calls {
		callType := call.Type
		if callType == "" {
			callType = "function"
		}
		result = append(result, agentschema.ToolCall{
			ID:   call.ID,
			Type: callType,
			Function: agentschema.FunctionCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return result
}

func streamToolCalls(calls []sdk.ChatCompletionChunkChoiceDeltaToolCall) []agentschema.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	result := make([]agentschema.ToolCall, 0, len(calls))
	for _, call := range calls {
		index := int(call.Index)
		result = append(result, agentschema.ToolCall{
			Index: &index,
			ID:    call.ID,
			Type:  call.Type,
			Function: agentschema.FunctionCall{
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		})
	}
	return result
}

func responseUsage(usage sdk.CompletionUsage) *agentschema.TokenUsage {
	return &agentschema.TokenUsage{
		PromptTokens:     int(usage.PromptTokens),
		CompletionTokens: int(usage.CompletionTokens),
		TotalTokens:      int(usage.TotalTokens),
		PromptTokenDetails: agentschema.PromptTokenDetails{
			CachedTokens: int(usage.PromptTokensDetails.CachedTokens),
		},
		CompletionTokensDetails: agentschema.CompletionTokensDetails{
			ReasoningTokens: int(usage.CompletionTokensDetails.ReasoningTokens),
		},
	}
}

func rawReasoningContent(raw, field string) string {
	if raw == "" || strings.TrimSpace(field) == "" {
		return ""
	}
	value := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ""
	}
	var result string
	if err := json.Unmarshal(value[field], &result); err != nil {
		return ""
	}
	return result
}

func responseExtra(rawResponse *http.Response, provider, responseID, model string, created int64, serviceTier, fingerprint string) map[string]any {
	result := map[string]any{
		ExtraKeyProvider: provider,
		ExtraKeyProtocol: "openai-chat-completions",
	}
	if requestID := responseRequestID(rawResponse); requestID != "" {
		result[ExtraKeyRequestID] = requestID
	}
	if responseID != "" {
		result[ExtraKeyResponseID] = responseID
	}
	if model != "" {
		result[ExtraKeyModel] = model
	}
	if created != 0 {
		result[ExtraKeyCreated] = created
	}
	if serviceTier != "" {
		result[ExtraKeyServiceTier] = serviceTier
	}
	if fingerprint != "" {
		result[ExtraKeySystemFingerprint] = fingerprint
	}
	return result
}

func responseRequestID(response *http.Response) string {
	if response == nil {
		return ""
	}
	for _, name := range []string{"x-request-id", "openai-request-id", "request-id", "x-ms-request-id"} {
		if value := strings.TrimSpace(response.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}
