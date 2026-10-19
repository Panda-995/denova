package openaichatcompletions

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	"github.com/openai/openai-go/v3/option"
)

// Generate returns choice index zero from one Chat Completions response.
func (model *ChatModel) Generate(ctx context.Context, input []*agentschema.Message, opts ...agentmodel.ModelOption) (*agentschema.Message, error) {
	opts = agentmodel.BindContextSessionKey(ctx, model.options, opts...)
	params, requestOptions, err := model.request(input, false, opts...)
	if err != nil {
		return nil, err
	}
	var rawResponse *http.Response
	requestOptions = append(requestOptions, option.WithResponseInto(&rawResponse))
	response, err := model.client.Chat.Completions.New(ctx, params, requestOptions...)
	if err != nil {
		return nil, modelCallError(ctx, err)
	}
	message, err := responseMessage(response, rawResponse, model.config, model.compatibility.ReasoningContentField)
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, fmt.Errorf("openai chat completion: response has no choice with index 0")
	}
	return message, nil
}

// Stream converts the SDK stream directly into a capacity-one agent core pipe. The
// producer closes both streams and recovers panics so no provider goroutine can
// terminate the process.
func (model *ChatModel) Stream(ctx context.Context, input []*agentschema.Message, opts ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	opts = agentmodel.BindContextSessionKey(ctx, model.options, opts...)
	params, requestOptions, err := model.request(input, true, opts...)
	if err != nil {
		return nil, err
	}
	var rawResponse *http.Response
	requestOptions = append(requestOptions, option.WithResponseInto(&rawResponse))
	providerStream := model.client.Chat.Completions.NewStreaming(ctx, params, requestOptions...)
	if err := providerStream.Err(); err != nil {
		_ = providerStream.Close()
		return nil, modelCallError(ctx, err)
	}

	reader, writer := agentstream.Pipe[*agentschema.Message](1)
	go func() {
		defer writer.Close()
		defer providerStream.Close()
		defer func() {
			if value := recover(); value != nil {
				writer.Send(nil, &agentstream.PanicError{Value: value, Stack: append([]byte(nil), debug.Stack()...)})
			}
		}()

		metadataPending := true
		var continuation continuationState
		for providerStream.Next() {
			chunk := providerStream.Current()
			if choice, found := streamChoice(chunk.Choices); found {
				if err := continuation.add(choice.Delta.RawJSON()); err != nil {
					writer.Send(nil, err)
					return
				}
			}
			message, emit := streamMessage(
				chunk,
				rawResponse,
				metadataPending,
				string(model.config.Provider),
				model.compatibility.ReasoningContentField,
			)
			if !emit {
				continue
			}
			metadataPending = false
			if writer.Send(message, nil) {
				return
			}
		}
		if err := providerStream.Err(); err != nil {
			writer.Send(nil, modelCallError(ctx, err))
			return
		}
		message, err := continuation.message(model.config)
		if err != nil {
			writer.Send(nil, err)
			return
		}
		if message != nil {
			writer.Send(message, nil)
		}
	}()
	return reader, nil
}

// WithTools returns a detached model; neither the receiver nor the caller's
// tool definitions can be mutated through the returned instance.
func (model *ChatModel) WithTools(tools []*agentschema.ToolInfo) (agentmodel.ToolCallingChatModel, error) {
	if model == nil {
		return nil, fmt.Errorf("openai chat model: nil receiver")
	}
	clone := *model
	clone.options = agentmodel.GetCommonOptions(model.options, agentmodel.WithTools(tools))
	return &clone, nil
}

func modelCallError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return adaptAPIError(err)
}
