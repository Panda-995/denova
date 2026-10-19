package googlegenerativeai

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alfredxw/denova/agent/model/providers"
	agentschema "github.com/alfredxw/denova/agent/schema"
	"google.golang.org/genai"
)

const (
	extraKeyRequestID  = "google-request-id"
	extraKeyProvider   = "provider"
	extraKeyProtocol   = "protocol"
	extraKeyResponseID = "response_id"
	extraKeyModel      = "model"
)

func responseMessage(response *genai.GenerateContentResponse, config providers.ModelConfig) (*agentschema.Message, error) {
	if response == nil {
		return nil, fmt.Errorf("google generative AI: empty response")
	}
	candidate := firstCandidate(response)
	if candidate == nil || candidate.Content == nil {
		if response.PromptFeedback != nil && response.PromptFeedback.BlockReasonMessage != "" {
			return nil, fmt.Errorf("google generative AI: %s", response.PromptFeedback.BlockReasonMessage)
		}
		return nil, fmt.Errorf("google generative AI: response has no candidate")
	}
	message := &agentschema.Message{
		Role: agentschema.Assistant,
		ResponseMeta: &agentschema.ResponseMeta{
			FinishReason: string(candidate.FinishReason),
			Usage:        responseUsage(response.UsageMetadata),
		},
		Extra: responseExtra(response, config),
	}
	var content strings.Builder
	var reasoning strings.Builder
	toolIndex := 0
	for index, original := range candidate.Content.Parts {
		if original == nil {
			continue
		}
		part := responsePart(original)
		candidate.Content.Parts[index] = part
		if part.Thought {
			reasoning.WriteString(part.Text)
		} else if part.Text != "" {
			content.WriteString(part.Text)
		}
		if part.FunctionCall != nil {
			arguments, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("google generative AI: encode tool call %q: %w", part.FunctionCall.Name, err)
			}
			index := toolIndex
			toolIndex++
			message.ToolCalls = append(message.ToolCalls, agentschema.ToolCall{
				Index: &index,
				ID:    part.FunctionCall.ID,
				Type:  "function",
				Function: agentschema.FunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(arguments),
				},
			})
		}
	}
	message.Content = content.String()
	message.ReasoningContent = reasoning.String()
	continuation, err := responseContinuation(candidate.Content.Parts, config)
	if err != nil {
		return nil, err
	}
	if continuation != nil {
		message.Extra[providers.ExtraKeyContinuation] = continuation
	}
	return message, nil
}

func firstCandidate(response *genai.GenerateContentResponse) *genai.Candidate {
	if response == nil {
		return nil
	}
	for _, candidate := range response.Candidates {
		if candidate != nil && candidate.Index == 0 {
			return candidate
		}
	}
	if len(response.Candidates) != 0 {
		return response.Candidates[0]
	}
	return nil
}

func responseContinuation(parts []*genai.Part, config providers.ModelConfig) (any, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	return providers.NewContinuation(config, parts)
}

func responseUsage(usage *genai.GenerateContentResponseUsageMetadata) *agentschema.TokenUsage {
	if usage == nil {
		return nil
	}
	return &agentschema.TokenUsage{
		PromptTokens:     int(usage.PromptTokenCount),
		CompletionTokens: int(usage.CandidatesTokenCount + usage.ThoughtsTokenCount),
		TotalTokens:      int(usage.TotalTokenCount),
		PromptTokenDetails: agentschema.PromptTokenDetails{
			CachedTokens: int(usage.CachedContentTokenCount),
		},
		CompletionTokensDetails: agentschema.CompletionTokensDetails{
			ReasoningTokens: int(usage.ThoughtsTokenCount),
		},
	}
}

func responseExtra(response *genai.GenerateContentResponse, config providers.ModelConfig) map[string]any {
	result := map[string]any{
		extraKeyProvider: string(config.Provider),
		extraKeyProtocol: string(providers.ProtocolGoogleGenerativeAI),
	}
	if response == nil {
		return result
	}
	if response.ResponseID != "" {
		result[extraKeyResponseID] = response.ResponseID
	}
	if response.ModelVersion != "" {
		result[extraKeyModel] = response.ModelVersion
	}
	if response.SDKHTTPResponse != nil {
		for _, name := range []string{"x-request-id", "request-id", "x-goog-request-id"} {
			if value := strings.TrimSpace(response.SDKHTTPResponse.Headers.Get(name)); value != "" {
				result[extraKeyRequestID] = value
				break
			}
		}
	}
	return result
}

type streamState struct {
	parts         []*genai.Part
	finishReason  genai.FinishReason
	nextToolIndex int
	sentMetadata  bool
}

func (state *streamState) convert(response *genai.GenerateContentResponse, config providers.ModelConfig) ([]*agentschema.Message, error) {
	if response == nil {
		return nil, nil
	}
	candidate := firstCandidate(response)
	result := make([]*agentschema.Message, 0)
	if !state.sentMetadata {
		result = append(result, &agentschema.Message{Role: agentschema.Assistant, Extra: responseExtra(response, config)})
		state.sentMetadata = true
	}
	if candidate != nil && candidate.Content != nil {
		for _, part := range candidate.Content.Parts {
			if part == nil {
				continue
			}
			part = responsePart(part)
			state.appendPart(part)
			message := &agentschema.Message{Role: agentschema.Assistant}
			if part.Thought {
				message.ReasoningContent = part.Text
			} else {
				message.Content = part.Text
			}
			if part.FunctionCall != nil {
				arguments, err := json.Marshal(part.FunctionCall.Args)
				if err != nil {
					return nil, fmt.Errorf("google generative AI stream: encode tool call %q: %w", part.FunctionCall.Name, err)
				}
				index := state.nextToolIndex
				state.nextToolIndex++
				message.ToolCalls = []agentschema.ToolCall{{
					Index: &index,
					ID:    part.FunctionCall.ID,
					Type:  "function",
					Function: agentschema.FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: string(arguments),
					},
				}}
			}
			if message.Content != "" || message.ReasoningContent != "" || len(message.ToolCalls) != 0 {
				result = append(result, message)
			}
		}
	}
	if candidate != nil && candidate.FinishReason != "" && candidate.FinishReason != genai.FinishReasonUnspecified {
		state.finishReason = candidate.FinishReason
	}
	if state.finishReason != "" || response.UsageMetadata != nil {
		result = append(result, &agentschema.Message{
			Role: agentschema.Assistant,
			ResponseMeta: &agentschema.ResponseMeta{
				FinishReason: string(state.finishReason),
				Usage:        responseUsage(response.UsageMetadata),
			},
		})
	}
	return result, nil
}

func (state *streamState) finish(config providers.ModelConfig) (*agentschema.Message, error) {
	if state.finishReason == "" {
		return nil, fmt.Errorf("google generative AI stream ended without a finish reason")
	}
	continuation, err := responseContinuation(state.parts, config)
	if err != nil {
		return nil, err
	}
	if continuation == nil {
		return nil, nil
	}
	return &agentschema.Message{
		Role:  agentschema.Assistant,
		Extra: map[string]any{providers.ExtraKeyContinuation: continuation},
	}, nil
}

// Keep generated IDs in both the execution message and its opaque continuation.
// Some Gemini models omit IDs, but Native requires stable tool/result pairing.
func responsePart(original *genai.Part) *genai.Part {
	part := *original
	if original.FunctionCall != nil {
		call := *original.FunctionCall
		if call.Args == nil {
			call.Args = map[string]any{}
		}
		if call.ID == "" {
			call.ID = "gemini_" + rand.Text()
		}
		part.FunctionCall = &call
	}
	return &part
}

// SSE text fragments and trailing signature events form one signed content
// part. Persist the assembled part, while emitting text deltas independently.
func (state *streamState) appendPart(part *genai.Part) {
	if len(state.parts) > 0 && part.FunctionCall == nil && part.InlineData == nil && part.FileData == nil {
		last := state.parts[len(state.parts)-1]
		if part.Text == "" && len(part.ThoughtSignature) > 0 {
			last.ThoughtSignature = part.ThoughtSignature
			return
		}
		if last.FunctionCall == nil && last.Text != "" && last.Thought == part.Thought && len(last.ThoughtSignature) == 0 {
			last.Text += part.Text
			last.ThoughtSignature = part.ThoughtSignature
			return
		}
	}
	state.parts = append(state.parts, part)
}
