package openaichatcompletions

import (
	"fmt"
	"sort"
	"strings"

	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	agentschema "github.com/alfredxw/denova/agent/schema"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

func (model *ChatModel) request(input []*agentschema.Message, stream bool, opts ...agentmodel.ModelOption) (sdk.ChatCompletionNewParams, []option.RequestOption, error) {
	messages, err := requestMessages(input, model.compatibility, model.config)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, nil, err
	}
	common := agentmodel.GetCommonOptions(model.options, opts...)
	tools := common.Tools
	if common.ToolChoice != nil && len(common.AllowedToolNames) != 0 {
		tools = filterTools(tools, common.AllowedToolNames)
	}
	requestTools, err := requestTools(tools)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, nil, err
	}
	toolChoice, err := requestToolChoice(common.ToolChoice, len(requestTools), *model.compatibility.SupportsToolChoice)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, nil, err
	}

	params := sdk.ChatCompletionNewParams{
		Messages:   messages,
		Model:      shared.ChatModel(model.config.Model),
		Tools:      requestTools,
		ToolChoice: toolChoice,
	}
	if model.config.Temperature != nil {
		params.Temperature = sdk.Float(float64(*model.config.Temperature))
	}
	maxTokens := model.config.MaxOutputTokens
	if common.MaxTokens != nil {
		maxTokens = common.MaxTokens
	}
	if maxTokens != nil {
		switch model.compatibility.MaxTokensField {
		case MaxTokensFieldMaxCompletionTokens:
			params.MaxCompletionTokens = sdk.Int(int64(*maxTokens))
		case MaxTokensFieldMaxTokens:
			params.MaxTokens = sdk.Int(int64(*maxTokens))
		}
	}
	if stream && *model.compatibility.SupportsStreamUsage {
		params.StreamOptions.IncludeUsage = sdk.Bool(true)
	}

	return params, model.requestOptions(common.SessionKey), nil
}

func requestMessages(messages []*agentschema.Message, compatibility Compatibility, config providers.ModelConfig) ([]sdk.ChatCompletionMessageParamUnion, error) {
	result := make([]sdk.ChatCompletionMessageParamUnion, 0, len(messages))
	imageCount := providers.NativeImageCount(messages)
	var toolImages []sdk.ChatCompletionContentPartUnionParam
	for index, message := range messages {
		if message == nil {
			return nil, fmt.Errorf("openai request message %d: nil message", index)
		}
		// Strict templates accept one leading system message. Merge only the
		// initial instruction block, preserving the order of later context.
		if message.Role == agentschema.System && len(result) == 1 && result[0].OfSystem != nil {
			content := result[0].OfSystem.Content.OfString.Value + "\n\n" + message.Content
			result[0].OfSystem.Content.OfString = sdk.String(content)
			continue
		}
		mapped, err := requestMessage(message, compatibility, config, imageCount)
		if err != nil {
			return nil, fmt.Errorf("openai request message %d: %w", index, err)
		}
		result = append(result, mapped)
		if message.Role == agentschema.ToolRole {
			for _, attachment := range message.Attachments {
				prepared, err := config.PrepareImage(attachment, imageCount)
				if err != nil {
					return nil, fmt.Errorf("openai request tool image %d: %w", index, err)
				}
				toolImages = append(toolImages,
					sdk.TextContentPart(fmt.Sprintf("Image from tool call %q (%s).", message.ToolCallID, attachment.Name)),
					sdk.ImageContentPart(sdk.ChatCompletionContentPartImageImageURLParam{URL: prepared.DataURL(), Detail: "auto"}),
				)
			}
		}
		// Chat Completions only accepts text in tool messages. Project images
		// after the complete result batch so no user message splits call pairing.
		if len(toolImages) > 0 && (index+1 == len(messages) || messages[index+1] == nil || messages[index+1].Role != agentschema.ToolRole) {
			result = append(result, sdk.UserMessage(toolImages))
			toolImages = nil
		}
	}
	return result, nil
}

func requestMessage(message *agentschema.Message, compatibility Compatibility, config providers.ModelConfig, imageCount int) (sdk.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case agentschema.System:
		result := sdk.SystemMessage(message.Content)
		if message.Name != "" {
			result.OfSystem.Name = sdk.String(message.Name)
		}
		return result, nil
	case agentschema.User:
		content := agentschema.ModelUserContent(message)
		var result sdk.ChatCompletionMessageParamUnion
		hasNativeImage := false
		for _, attachment := range message.Attachments {
			if agentschema.IsNativeImageMediaType(attachment.MediaType) {
				hasNativeImage = true
				break
			}
		}
		if !hasNativeImage {
			result = sdk.UserMessage(content)
		} else {
			parts := []sdk.ChatCompletionContentPartUnionParam{sdk.TextContentPart(content)}
			for _, attachment := range message.Attachments {
				if !agentschema.IsNativeImageMediaType(attachment.MediaType) {
					continue
				}
				image, err := config.PrepareImage(attachment, imageCount)
				if err != nil {
					return sdk.ChatCompletionMessageParamUnion{}, err
				}
				parts = append(parts, sdk.ImageContentPart(sdk.ChatCompletionContentPartImageImageURLParam{URL: image.DataURL(), Detail: "auto"}))
			}
			result = sdk.UserMessage(parts)
		}
		if message.Name != "" {
			result.OfUser.Name = sdk.String(message.Name)
		}
		return result, nil
	case agentschema.Assistant:
		var continuation chatContinuation
		if _, err := providers.DecodeContinuation(message.Extra, config, &continuation); err != nil {
			return sdk.ChatCompletionMessageParamUnion{}, err
		}
		assistant := sdk.ChatCompletionAssistantMessageParam{}
		if message.Content != "" || len(message.ToolCalls) == 0 || compatibility.RequiresAssistantToolContent {
			assistant.Content.OfString = sdk.String(message.Content)
		}
		if message.Name != "" {
			assistant.Name = sdk.String(message.Name)
		}
		extraFields := map[string]any{}
		if compatibility.shouldReplayReasoning(message, config.ThinkingLevel) && message.ReasoningContent != "" {
			extraFields[compatibility.ReasoningContentField] = message.ReasoningContent
		}
		if len(continuation.ExtraContent) != 0 {
			extraFields["extra_content"] = continuation.ExtraContent
		}
		assistant.SetExtraFields(extraFields)
		for callIndex, call := range message.ToolCalls {
			if call.Type != "" && call.Type != "function" {
				return sdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool call %d has unsupported type %q", callIndex, call.Type)
			}
			function := sdk.ChatCompletionMessageFunctionToolCallParam{
				ID: call.ID,
				Function: sdk.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				},
			}
			if extraContent := continuation.ToolCalls[call.ID]; len(extraContent) != 0 {
				function.SetExtraFields(map[string]any{"extra_content": extraContent})
			}
			assistant.ToolCalls = append(assistant.ToolCalls, sdk.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &function,
			})
		}
		return sdk.ChatCompletionMessageParamUnion{OfAssistant: &assistant}, nil
	case agentschema.ToolRole:
		// The string constructor is deliberate: tool outputs that happen to be
		// valid JSON must remain JSON strings in Chat Completions history.
		return sdk.ToolMessage(message.Content, message.ToolCallID), nil
	default:
		return sdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("unsupported role %q", message.Role)
	}
}

func requestTools(tools []*agentschema.ToolInfo) ([]sdk.ChatCompletionToolUnionParam, error) {
	if tools == nil {
		return nil, nil
	}
	result := make([]sdk.ChatCompletionToolUnionParam, 0, len(tools))
	for index, tool := range tools {
		if tool == nil {
			return nil, fmt.Errorf("openai request tool %d: nil tool", index)
		}
		if strings.TrimSpace(tool.Name) == "" {
			return nil, fmt.Errorf("openai request tool %d: name is required", index)
		}
		definition := shared.FunctionDefinitionParam{Name: tool.Name}
		if tool.Desc != "" {
			definition.Description = sdk.String(tool.Desc)
		}
		if tool.ParamsOneOf != nil {
			parameters, err := tool.ParamsOneOf.ToJSONSchemaMap()
			if err != nil {
				return nil, fmt.Errorf("openai request tool %q schema: %w", tool.Name, err)
			}
			if parameters != nil {
				definition.Parameters = shared.FunctionParameters(parameters)
			}
		}
		result = append(result, sdk.ChatCompletionFunctionTool(definition))
	}
	return result, nil
}

func filterTools(tools []*agentschema.ToolInfo, names []string) []*agentschema.ToolInfo {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	result := make([]*agentschema.ToolInfo, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		if _, ok := allowed[tool.Name]; ok {
			result = append(result, tool)
		}
	}
	return result
}

func requestToolChoice(choice *agentmodel.ToolChoice, toolCount int, supported bool) (sdk.ChatCompletionToolChoiceOptionUnionParam, error) {
	if choice == nil {
		return sdk.ChatCompletionToolChoiceOptionUnionParam{}, nil
	}
	if !supported {
		return sdk.ChatCompletionToolChoiceOptionUnionParam{}, fmt.Errorf("openai request: endpoint does not support tool_choice")
	}
	result := sdk.ChatCompletionToolChoiceOptionUnionParam{}
	switch *choice {
	case agentmodel.ToolChoiceForbidden:
		result.OfAuto = sdk.String("none")
	case agentmodel.ToolChoiceAllowed:
		result.OfAuto = sdk.String("auto")
	case agentmodel.ToolChoiceForced:
		if toolCount == 0 {
			return result, fmt.Errorf("openai request: forced tool choice has no available tools")
		}
		result.OfAuto = sdk.String("required")
	default:
		return result, fmt.Errorf("openai request: unsupported tool choice %q", *choice)
	}
	return result, nil
}

func (model *ChatModel) requestOptions(sessionKey string) []option.RequestOption {
	extraFields := make(map[string]any, len(model.extraFields)+1)
	for key, value := range model.extraFields {
		extraFields[key] = value
	}
	for key, value := range model.compatibility.thinkingFields(model.config.ThinkingLevel) {
		extraFields[key] = value
	}
	result := make([]option.RequestOption, 0, len(extraFields)+2)
	if effort, ok := model.compatibility.mappedEffort(model.config.ThinkingLevel); ok {
		result = append(result, option.WithJSONSet("reasoning_effort", effort))
	}
	if format := chatResponseFormat(model.config.OutputFormat); format != nil {
		result = append(result, option.WithJSONSet("response_format", format))
	}
	keys := make([]string, 0, len(extraFields))
	for key := range extraFields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, option.WithJSONSet(escapeJSONPathKey(key), extraFields[key]))
	}
	if mapping := model.config.SessionKeyMapping; mapping != nil && sessionKey != "" {
		switch mapping.Location {
		case providers.SessionKeyLocationHeader:
			result = append(result, option.WithHeader(mapping.Name, sessionKey))
		case providers.SessionKeyLocationBody:
			result = append(result, option.WithJSONSet(escapeJSONPathKey(mapping.Name), sessionKey))
		}
	}
	return result
}

func chatResponseFormat(format *providers.OutputFormat) any {
	if format == nil || format.Type == "" {
		return nil
	}
	result := map[string]any{"type": string(format.Type)}
	if format.Type != providers.OutputFormatJSONSchema {
		return result
	}
	jsonSchema := map[string]any{
		"name":   format.Name,
		"schema": format.Schema,
		"strict": format.Strict,
	}
	if format.Description != "" {
		jsonSchema["description"] = format.Description
	}
	result["json_schema"] = jsonSchema
	return result
}

func escapeJSONPathKey(key string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`.`, `\.`,
		`:`, `\:`,
		`#`, `\#`,
		`@`, `\@`,
	)
	return replacer.Replace(key)
}
