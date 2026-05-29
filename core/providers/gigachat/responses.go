package gigachat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

// ToGigaChatResponsesRequest converts a Bifrost Responses request to GigaChat v2 chat completions format.
func ToGigaChatResponsesRequest(bifrostReq *schemas.BifrostResponsesRequest) (*GigaChatResponsesRequest, error) {
	if bifrostReq == nil {
		return nil, fmt.Errorf("bifrost responses request is nil")
	}
	if strings.TrimSpace(bifrostReq.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}

	messages := make([]GigaChatResponsesMessage, 0, len(bifrostReq.Input)+1)
	if bifrostReq.Params != nil && bifrostReq.Params.Instructions != nil && strings.TrimSpace(*bifrostReq.Params.Instructions) != "" {
		messages = append(messages, GigaChatResponsesMessage{
			Role: string(schemas.ResponsesInputMessageRoleSystem),
			Content: []GigaChatResponsesContentPart{{
				Text: schemas.Ptr(*bifrostReq.Params.Instructions),
			}},
		})
	}

	for index, message := range bifrostReq.Input {
		convertedMessages, err := toGigaChatResponsesMessages(message)
		if err != nil {
			return nil, fmt.Errorf("input[%d]: %w", index, err)
		}
		messages = append(messages, convertedMessages...)
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages are required")
	}

	gigaChatReq := &GigaChatResponsesRequest{
		Model:    bifrostReq.Model,
		Messages: messages,
	}
	if bifrostReq.Params == nil {
		return gigaChatReq, nil
	}

	if unsupportedParams := unsupportedGigaChatResponsesParams(bifrostReq.Params); len(unsupportedParams) > 0 {
		return nil, fmt.Errorf("GigaChat Responses do not support parameter(s): %s", strings.Join(unsupportedParams, ", "))
	}
	if err := applyGigaChatResponsesParams(gigaChatReq, bifrostReq.Params); err != nil {
		return nil, err
	}
	return gigaChatReq, nil
}

// ToBifrostResponsesResponse converts a GigaChat v2 chat completions response to Bifrost Responses format.
func ToBifrostResponsesResponse(providerName schemas.ModelProvider, response *GigaChatResponsesResponse) *schemas.BifrostResponsesResponse {
	if response == nil {
		return nil
	}

	output := make([]schemas.ResponsesMessage, 0, len(response.Choices))
	var status *string
	var incompleteDetails *schemas.ResponsesResponseIncompleteDetails
	var stopReason *string
	for _, choice := range response.Choices {
		output = append(output, toBifrostGigaChatResponsesChoiceOutput(choice, response.MessageID)...)

		finishReason := choice.FinishReason
		if finishReason == nil && choice.Message != nil {
			finishReason = choice.Message.FinishReason
		}
		if mappedStatus, mappedIncompleteDetails, mappedStopReason := toBifrostGigaChatResponsesStatus(finishReason); mappedStatus != nil {
			status = mappedStatus
			incompleteDetails = mappedIncompleteDetails
			stopReason = mappedStopReason
			if *mappedStatus == "incomplete" {
				break
			}
		}
	}

	bifrostResponse := &schemas.BifrostResponsesResponse{
		Object:            "response",
		CreatedAt:         response.Created,
		Model:             response.Model,
		Output:            output,
		Status:            status,
		IncompleteDetails: incompleteDetails,
		StopReason:        stopReason,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Provider: providerName,
		},
	}
	if strings.TrimSpace(response.ID) != "" {
		bifrostResponse.ID = &response.ID
	}
	if usage := toBifrostGigaChatUsage(response.Usage); usage != nil {
		bifrostResponse.Usage = usage.ToResponsesResponseUsage()
	}
	bifrostResponse.ProviderExtraFields = toBifrostGigaChatResponsesProviderExtraFields(response)

	return bifrostResponse
}

func toBifrostGigaChatResponsesChoiceOutput(choice GigaChatResponsesChoice, fallbackMessageID *string) []schemas.ResponsesMessage {
	if choice.Message == nil {
		return nil
	}

	message := choice.Message
	messageID := message.MessageID
	if messageID == nil || strings.TrimSpace(*messageID) == "" {
		messageID = fallbackMessageID
	}
	output := make([]schemas.ResponsesMessage, 0, len(message.Content)+1)
	contentBlocks := make([]schemas.ResponsesMessageContentBlock, 0, len(message.Content))
	hasFunctionCall := false
	for index, part := range message.Content {
		if part.Text != nil {
			contentBlocks = append(contentBlocks, schemas.ResponsesMessageContentBlock{
				Type: schemas.ResponsesOutputMessageContentTypeText,
				Text: part.Text,
				ResponsesOutputMessageContentText: &schemas.ResponsesOutputMessageContentText{
					Annotations: []schemas.ResponsesOutputMessageContentTextAnnotation{},
					LogProbs:    []schemas.ResponsesOutputMessageContentTextLogProb{},
				},
			})
		}
		if part.FunctionCall != nil {
			hasFunctionCall = true
			if toolCall := toBifrostGigaChatResponsesFunctionCall(messageID, index, part.FunctionCall); toolCall != nil {
				output = append(output, *toolCall)
			}
		}
		if part.FunctionResult != nil {
			if toolResult := toBifrostGigaChatResponsesFunctionResult(messageID, index, part.FunctionResult); toolResult != nil {
				output = append(output, *toolResult)
			}
		}
	}
	if len(contentBlocks) > 0 {
		messageType := schemas.ResponsesMessageTypeMessage
		role := schemas.ResponsesInputMessageRoleAssistant
		if strings.TrimSpace(message.Role) != "" {
			role = schemas.ResponsesMessageRoleType(message.Role)
		}
		output = append([]schemas.ResponsesMessage{{
			ID:     messageID,
			Type:   &messageType,
			Role:   &role,
			Status: schemas.Ptr("completed"),
			Content: &schemas.ResponsesMessageContent{
				ContentBlocks: contentBlocks,
			},
		}}, output...)
	}
	if !hasFunctionCall && message.FunctionCall != nil {
		if toolCall := toBifrostGigaChatResponsesFunctionCall(messageID, 0, message.FunctionCall); toolCall != nil {
			output = append(output, *toolCall)
		}
	}
	return output
}

func toBifrostGigaChatResponsesFunctionCall(messageID *string, index int, functionCall *GigaChatResponsesFunctionCall) *schemas.ResponsesMessage {
	if functionCall == nil || strings.TrimSpace(functionCall.Name) == "" {
		return nil
	}

	itemID := toBifrostGigaChatResponsesItemID("fc", messageID, index)
	arguments := stringifyGigaChatResponsesPayload(functionCall.Arguments)
	messageType := schemas.ResponsesMessageTypeFunctionCall
	role := schemas.ResponsesInputMessageRoleAssistant
	return &schemas.ResponsesMessage{
		ID:     &itemID,
		Type:   &messageType,
		Role:   &role,
		Status: schemas.Ptr("completed"),
		ResponsesToolMessage: &schemas.ResponsesToolMessage{
			CallID:    &itemID,
			Name:      schemas.Ptr(strings.TrimSpace(functionCall.Name)),
			Arguments: &arguments,
		},
	}
}

func toBifrostGigaChatResponsesFunctionResult(messageID *string, index int, functionResult *GigaChatResponsesFunctionResult) *schemas.ResponsesMessage {
	if functionResult == nil || strings.TrimSpace(functionResult.Name) == "" {
		return nil
	}

	itemID := toBifrostGigaChatResponsesItemID("fr", messageID, index)
	output := stringifyGigaChatResponsesPayload(functionResult.Result)
	messageType := schemas.ResponsesMessageTypeFunctionCallOutput
	return &schemas.ResponsesMessage{
		ID:     &itemID,
		Type:   &messageType,
		Status: schemas.Ptr("completed"),
		ResponsesToolMessage: &schemas.ResponsesToolMessage{
			CallID: &itemID,
			Name:   schemas.Ptr(strings.TrimSpace(functionResult.Name)),
			Output: &schemas.ResponsesToolMessageOutputStruct{
				ResponsesToolCallOutputStr: &output,
			},
		},
	}
}

func toBifrostGigaChatResponsesItemID(prefix string, messageID *string, index int) string {
	if messageID != nil && strings.TrimSpace(*messageID) != "" {
		if index == 0 {
			return strings.TrimSpace(*messageID)
		}
		return fmt.Sprintf("%s_%s_%d", prefix, strings.TrimSpace(*messageID), index)
	}
	return fmt.Sprintf("%s_%d", prefix, index)
}

func stringifyGigaChatResponsesPayload(payload interface{}) string {
	if payload == nil {
		return "{}"
	}
	if text, ok := payload.(string); ok {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return "{}"
		}
		var compacted bytes.Buffer
		if json.Compact(&compacted, []byte(trimmed)) == nil {
			return compacted.String()
		}
		return trimmed
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	var compacted bytes.Buffer
	if json.Compact(&compacted, raw) == nil {
		return compacted.String()
	}
	return string(raw)
}

func toBifrostGigaChatResponsesStatus(finishReason *string) (*string, *schemas.ResponsesResponseIncompleteDetails, *string) {
	mappedFinishReason := toBifrostGigaChatFinishReason(finishReason)
	if mappedFinishReason == nil || strings.TrimSpace(*mappedFinishReason) == "" {
		return nil, nil, nil
	}

	stopReason := strings.TrimSpace(*mappedFinishReason)
	switch stopReason {
	case string(schemas.BifrostFinishReasonLength):
		return schemas.Ptr("incomplete"), &schemas.ResponsesResponseIncompleteDetails{Reason: "max_output_tokens"}, &stopReason
	default:
		return schemas.Ptr("completed"), nil, &stopReason
	}
}

func toBifrostGigaChatResponsesProviderExtraFields(response *GigaChatResponsesResponse) map[string]interface{} {
	if response == nil {
		return nil
	}

	fields := make(map[string]interface{})
	if response.ThreadID != nil && strings.TrimSpace(*response.ThreadID) != "" {
		fields["thread_id"] = *response.ThreadID
	}
	if response.MessageID != nil && strings.TrimSpace(*response.MessageID) != "" {
		fields["message_id"] = *response.MessageID
	}
	if response.ToolsStateID != nil && strings.TrimSpace(*response.ToolsStateID) != "" {
		fields["tools_state_id"] = *response.ToolsStateID
	}
	if strings.TrimSpace(response.SystemFingerprint) != "" {
		fields["system_fingerprint"] = response.SystemFingerprint
	}
	if len(response.ExtraParams) > 0 {
		fields["gigachat_extra"] = response.ExtraParams
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func applyGigaChatResponsesParams(gigaChatReq *GigaChatResponsesRequest, params *schemas.ResponsesParameters) error {
	modelOptions := &GigaChatResponsesModelOptions{
		Temperature: params.Temperature,
		TopP:        params.TopP,
		MaxTokens:   params.MaxOutputTokens,
		TopLogProbs: params.TopLogProbs,
	}

	if params.Reasoning != nil && params.Reasoning.Effort != nil && strings.TrimSpace(*params.Reasoning.Effort) != "" && *params.Reasoning.Effort != "none" {
		modelOptions.Reasoning = &GigaChatResponsesReasoning{Effort: *params.Reasoning.Effort}
	}
	if params.Text != nil && params.Text.Format != nil {
		responseFormat, err := toGigaChatResponsesResponseFormat(params.Text.Format)
		if err != nil {
			return err
		}
		modelOptions.ResponseFormat = responseFormat
	}
	if hasGigaChatResponsesModelOptions(modelOptions) {
		gigaChatReq.ModelOptions = modelOptions
	}

	tools, err := toGigaChatResponsesTools(params.Tools)
	if err != nil {
		return err
	}
	gigaChatReq.Tools = tools

	toolConfig, err := toGigaChatResponsesToolConfig(params.ToolChoice)
	if err != nil {
		return err
	}
	gigaChatReq.ToolConfig = toolConfig

	return applyGigaChatResponsesExtraParams(gigaChatReq, params.ExtraParams)
}

func toGigaChatResponsesMessages(message schemas.ResponsesMessage) ([]GigaChatResponsesMessage, error) {
	messageType := schemas.ResponsesMessageTypeMessage
	if message.Type != nil {
		messageType = *message.Type
	}

	switch messageType {
	case schemas.ResponsesMessageTypeMessage:
		return toGigaChatResponsesChatMessages(message)
	case schemas.ResponsesMessageTypeFunctionCall:
		return toGigaChatResponsesFunctionCallMessage(message)
	case schemas.ResponsesMessageTypeFunctionCallOutput:
		return toGigaChatResponsesFunctionResultMessage(message)
	case schemas.ResponsesMessageTypeReasoning:
		return toGigaChatResponsesReasoningMessage(message)
	default:
		return nil, fmt.Errorf("item type %q is not supported by GigaChat Responses", messageType)
	}
}

func toGigaChatResponsesChatMessages(message schemas.ResponsesMessage) ([]GigaChatResponsesMessage, error) {
	role := schemas.ResponsesInputMessageRoleUser
	if message.Role != nil {
		role = *message.Role
	}
	switch role {
	case schemas.ResponsesInputMessageRoleSystem, schemas.ResponsesInputMessageRoleUser, schemas.ResponsesInputMessageRoleAssistant:
	case schemas.ResponsesInputMessageRoleDeveloper:
		return nil, fmt.Errorf("developer messages are not supported by GigaChat Responses")
	default:
		return nil, fmt.Errorf("role %q is not supported by GigaChat Responses", role)
	}

	content, err := toGigaChatResponsesContentParts(message.Content)
	if err != nil {
		return nil, err
	}
	return []GigaChatResponsesMessage{{
		Role:      string(role),
		MessageID: message.ID,
		Content:   content,
	}}, nil
}

func toGigaChatResponsesFunctionCallMessage(message schemas.ResponsesMessage) ([]GigaChatResponsesMessage, error) {
	if message.ResponsesToolMessage == nil {
		return nil, fmt.Errorf("function_call item requires tool message fields")
	}
	if message.ResponsesToolMessage.Name == nil || strings.TrimSpace(*message.ResponsesToolMessage.Name) == "" {
		return nil, fmt.Errorf("function_call item name is required")
	}

	arguments, err := parseGigaChatFunctionArguments(message.ResponsesToolMessage.Arguments)
	if err != nil {
		return nil, err
	}
	functionCall := &GigaChatResponsesFunctionCall{
		Name:      strings.TrimSpace(*message.ResponsesToolMessage.Name),
		Arguments: arguments,
	}
	return []GigaChatResponsesMessage{{
		Role:      string(schemas.ResponsesInputMessageRoleAssistant),
		MessageID: message.ID,
		Content: []GigaChatResponsesContentPart{{
			FunctionCall: functionCall,
		}},
		FunctionCall: functionCall,
	}}, nil
}

func toGigaChatResponsesFunctionResultMessage(message schemas.ResponsesMessage) ([]GigaChatResponsesMessage, error) {
	if message.ResponsesToolMessage == nil {
		return nil, fmt.Errorf("function_call_output item requires tool message fields")
	}
	if message.ResponsesToolMessage.Name == nil || strings.TrimSpace(*message.ResponsesToolMessage.Name) == "" {
		return nil, fmt.Errorf("function_call_output item name is required")
	}

	result, err := toGigaChatFunctionResultPayload(message)
	if err != nil {
		return nil, err
	}
	return []GigaChatResponsesMessage{{
		Role:      "tool",
		MessageID: message.ID,
		Content: []GigaChatResponsesContentPart{{
			FunctionResult: &GigaChatResponsesFunctionResult{
				Name:   strings.TrimSpace(*message.ResponsesToolMessage.Name),
				Result: result,
			},
		}},
	}}, nil
}

func toGigaChatResponsesReasoningMessage(message schemas.ResponsesMessage) ([]GigaChatResponsesMessage, error) {
	content := make([]GigaChatResponsesContentPart, 0)
	if message.ResponsesReasoning != nil {
		for _, summary := range message.ResponsesReasoning.Summary {
			text := summary.Text
			content = append(content, GigaChatResponsesContentPart{Text: &text})
		}
	}
	if message.Content != nil {
		parts, err := toGigaChatResponsesContentParts(message.Content)
		if err != nil {
			return nil, err
		}
		content = append(content, parts...)
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("reasoning item content is required")
	}
	return []GigaChatResponsesMessage{{
		Role:      "reasoning",
		MessageID: message.ID,
		Content:   content,
	}}, nil
}

func toGigaChatResponsesContentParts(content *schemas.ResponsesMessageContent) ([]GigaChatResponsesContentPart, error) {
	if content == nil {
		return nil, nil
	}
	if content.ContentStr != nil {
		return []GigaChatResponsesContentPart{{Text: content.ContentStr}}, nil
	}
	if content.ContentBlocks == nil {
		return nil, nil
	}

	parts := make([]GigaChatResponsesContentPart, 0, len(content.ContentBlocks))
	for index, block := range content.ContentBlocks {
		switch block.Type {
		case schemas.ResponsesInputMessageContentBlockTypeText,
			schemas.ResponsesOutputMessageContentTypeText,
			schemas.ResponsesOutputMessageContentTypeReasoning:
			if block.Text != nil {
				parts = append(parts, GigaChatResponsesContentPart{Text: block.Text})
			}
		case schemas.ResponsesInputMessageContentBlockTypeFile:
			if block.FileID == nil || strings.TrimSpace(*block.FileID) == "" {
				return nil, fmt.Errorf("content block %d: GigaChat file content requires file_id", index)
			}
			file := GigaChatResponsesContentFile{
				ID:     strings.TrimSpace(*block.FileID),
				MIME:   nil,
				Target: nil,
			}
			if block.ResponsesInputMessageContentBlockFile != nil {
				if block.FileData != nil || block.FileURL != nil {
					return nil, fmt.Errorf("content block %d: GigaChat Responses supports pre-uploaded file_id references only", index)
				}
				file.MIME = block.FileType
			}
			parts = append(parts, GigaChatResponsesContentPart{Files: []GigaChatResponsesContentFile{file}})
		case schemas.ResponsesInputMessageContentBlockTypeImage:
			return nil, fmt.Errorf("content block %d: input_image is not supported by GigaChat Responses request conversion yet", index)
		case schemas.ResponsesInputMessageContentBlockTypeAudio:
			return nil, fmt.Errorf("content block %d: input_audio is not supported by GigaChat Responses request conversion yet", index)
		default:
			return nil, fmt.Errorf("content block %d: type %q is not supported by GigaChat Responses", index, block.Type)
		}
	}
	return parts, nil
}

func parseGigaChatFunctionArguments(arguments *string) (interface{}, error) {
	if arguments == nil || strings.TrimSpace(*arguments) == "" {
		return map[string]interface{}{}, nil
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(*arguments), &parsed); err != nil {
		return nil, fmt.Errorf("function_call arguments must be a JSON object: %w", err)
	}
	return parsed, nil
}

func toGigaChatFunctionResultPayload(message schemas.ResponsesMessage) (interface{}, error) {
	if message.ResponsesToolMessage != nil && message.ResponsesToolMessage.Output != nil {
		output := message.ResponsesToolMessage.Output
		if output.ResponsesToolCallOutputStr != nil {
			return *output.ResponsesToolCallOutputStr, nil
		}
		if output.ResponsesFunctionToolCallOutputBlocks != nil {
			return textFromGigaChatResponsesBlocks(output.ResponsesFunctionToolCallOutputBlocks)
		}
	}
	if message.Content != nil {
		if message.Content.ContentStr != nil {
			return *message.Content.ContentStr, nil
		}
		if message.Content.ContentBlocks != nil {
			return textFromGigaChatResponsesBlocks(message.Content.ContentBlocks)
		}
	}
	return "", nil
}

func textFromGigaChatResponsesBlocks(blocks []schemas.ResponsesMessageContentBlock) (string, error) {
	var builder strings.Builder
	for index, block := range blocks {
		switch block.Type {
		case schemas.ResponsesInputMessageContentBlockTypeText, schemas.ResponsesOutputMessageContentTypeText:
			if block.Text != nil {
				builder.WriteString(*block.Text)
			}
		default:
			return "", fmt.Errorf("function result block %d with type %q is not supported by GigaChat Responses", index, block.Type)
		}
	}
	return builder.String(), nil
}

func toGigaChatResponsesResponseFormat(format *schemas.ResponsesTextConfigFormat) (*GigaChatResponsesResponseFormat, error) {
	switch format.Type {
	case "text":
		return &GigaChatResponsesResponseFormat{Type: "text"}, nil
	case "json_schema":
		if format.JSONSchema == nil {
			return nil, fmt.Errorf("response_format json_schema requires schema")
		}
		schema := format.JSONSchema.ToMap()
		if schema == nil {
			return nil, fmt.Errorf("response_format json_schema requires non-empty schema")
		}
		schema = withGigaChatResponseFormatSchemaMetadata(schema, format.Name, format.Description)
		strict := format.Strict
		if strict == nil && format.JSONSchema.Strict != nil {
			strict = format.JSONSchema.Strict
		}
		return &GigaChatResponsesResponseFormat{
			Type:   "json_schema",
			Schema: schema,
			Strict: strict,
		}, nil
	default:
		return nil, fmt.Errorf("response_format type %q is not supported by GigaChat Responses", format.Type)
	}
}

func withGigaChatResponseFormatSchemaMetadata(schema interface{}, name *string, description *string) interface{} {
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}
	if name != nil && strings.TrimSpace(*name) != "" {
		if _, exists := schemaMap["title"]; !exists {
			schemaMap["title"] = strings.TrimSpace(*name)
		}
	}
	if description != nil && strings.TrimSpace(*description) != "" {
		if _, exists := schemaMap["description"]; !exists {
			schemaMap["description"] = strings.TrimSpace(*description)
		}
	}
	return schemaMap
}

func applyGigaChatResponsesExtraParams(gigaChatReq *GigaChatResponsesRequest, extraParams map[string]interface{}) error {
	if len(extraParams) == 0 {
		return nil
	}

	remaining := make(map[string]interface{}, len(extraParams))
	for name, value := range extraParams {
		remaining[name] = value
	}

	if value, ok, err := consumeStringExtraParam(remaining, "assistant_id"); err != nil {
		return err
	} else if ok {
		gigaChatReq.AssistantID = &value
	}
	if value, ok, err := consumeStringExtraParam(remaining, "tools_state_id"); err != nil {
		return err
	} else if ok {
		gigaChatReq.ToolsStateID = &value
	}
	if value, ok, err := consumeBoolExtraParam(remaining, "disable_filter"); err != nil {
		return err
	} else if ok {
		gigaChatReq.DisableFilter = &value
	}
	if value, ok, err := consumeStringSliceExtraParam(remaining, "flags"); err != nil {
		return err
	} else if ok {
		gigaChatReq.Flags = value
	}
	if value, ok, err := consumeMapExtraParam(remaining, "filter_config"); err != nil {
		return err
	} else if ok {
		gigaChatReq.FilterConfig = value
	}
	if value, ok, err := consumeMapExtraParam(remaining, "ranker_options"); err != nil {
		return err
	} else if ok {
		gigaChatReq.RankerOptions = value
	}
	if value, ok, err := consumeMapExtraParam(remaining, "user_info"); err != nil {
		return err
	} else if ok {
		gigaChatReq.UserInfo = value
	}
	if value, ok := remaining["storage"]; ok {
		gigaChatReq.Storage = value
		delete(remaining, "storage")
	}

	modelOptions := ensureGigaChatResponsesModelOptions(gigaChatReq)
	if value, ok, err := consumeStringExtraParam(remaining, "preset"); err != nil {
		return err
	} else if ok {
		modelOptions.Preset = &value
	}
	if value, ok, err := consumeFloatExtraParam(remaining, "repetition_penalty"); err != nil {
		return err
	} else if ok {
		modelOptions.RepetitionPenalty = &value
	}
	if value, ok, err := consumeFloatExtraParam(remaining, "update_interval"); err != nil {
		return err
	} else if ok {
		modelOptions.UpdateInterval = &value
	}
	if value, ok, err := consumeBoolExtraParam(remaining, "unnormalized_history"); err != nil {
		return err
	} else if ok {
		modelOptions.UnnormalizedHistory = &value
	}
	if !hasGigaChatResponsesModelOptions(modelOptions) {
		gigaChatReq.ModelOptions = nil
	}
	if len(remaining) > 0 {
		gigaChatReq.ExtraParams = remaining
	}
	return nil
}

func unsupportedGigaChatResponsesParams(params *schemas.ResponsesParameters) []string {
	if params == nil {
		return nil
	}

	unsupported := make([]string, 0)
	addIf := func(condition bool, name string) {
		if condition {
			unsupported = append(unsupported, name)
		}
	}

	addIf(params.Background != nil, "background")
	addIf(params.Conversation != nil, "conversation")
	addIf(len(params.Include) > 0, "include")
	addIf(params.MaxToolCalls != nil, "max_tool_calls")
	addIf(params.Metadata != nil && len(*params.Metadata) > 0, "metadata")
	addIf(params.ParallelToolCalls != nil && *params.ParallelToolCalls, "parallel_tool_calls")
	addIf(params.PreviousResponseID != nil, "previous_response_id")
	addIf(params.PromptCacheKey != nil, "prompt_cache_key")
	addIf(params.SafetyIdentifier != nil, "safety_identifier")
	addIf(params.ServiceTier != nil, "service_tier")
	addIf(params.StreamOptions != nil, "stream_options")
	addIf(params.Store != nil && *params.Store, "store")
	addIf(params.Truncation != nil, "truncation")
	addIf(params.User != nil, "user")
	if params.Reasoning != nil {
		addIf(params.Reasoning.GenerateSummary != nil, "reasoning.generate_summary")
		addIf(params.Reasoning.Summary != nil, "reasoning.summary")
		addIf(params.Reasoning.MaxTokens != nil, "reasoning.max_tokens")
	}
	if params.Text != nil {
		addIf(params.Text.Verbosity != nil, "text.verbosity")
	}

	sort.Strings(unsupported)
	return unsupported
}

func ensureGigaChatResponsesModelOptions(gigaChatReq *GigaChatResponsesRequest) *GigaChatResponsesModelOptions {
	if gigaChatReq.ModelOptions == nil {
		gigaChatReq.ModelOptions = &GigaChatResponsesModelOptions{}
	}
	return gigaChatReq.ModelOptions
}

func hasGigaChatResponsesModelOptions(options *GigaChatResponsesModelOptions) bool {
	if options == nil {
		return false
	}
	return options.Preset != nil ||
		options.Temperature != nil ||
		options.TopP != nil ||
		options.MaxTokens != nil ||
		options.RepetitionPenalty != nil ||
		options.UpdateInterval != nil ||
		options.UnnormalizedHistory != nil ||
		options.TopLogProbs != nil ||
		options.Reasoning != nil ||
		options.ResponseFormat != nil ||
		len(options.ExtraParams) > 0
}

func consumeStringExtraParam(params map[string]interface{}, name string) (string, bool, error) {
	value, ok := params[name]
	if !ok {
		return "", false, nil
	}
	converted, ok := schemas.SafeExtractString(value)
	if !ok {
		return "", true, fmt.Errorf("extra parameter %q must be a string", name)
	}
	delete(params, name)
	return strings.TrimSpace(converted), true, nil
}

func consumeBoolExtraParam(params map[string]interface{}, name string) (bool, bool, error) {
	value, ok := params[name]
	if !ok {
		return false, false, nil
	}
	converted, ok := schemas.SafeExtractBool(value)
	if !ok {
		return false, true, fmt.Errorf("extra parameter %q must be a boolean", name)
	}
	delete(params, name)
	return converted, true, nil
}

func consumeFloatExtraParam(params map[string]interface{}, name string) (float64, bool, error) {
	value, ok := params[name]
	if !ok {
		return 0, false, nil
	}
	converted, ok := schemas.SafeExtractFloat64(value)
	if !ok {
		return 0, true, fmt.Errorf("extra parameter %q must be a number", name)
	}
	delete(params, name)
	return converted, true, nil
}

func consumeStringSliceExtraParam(params map[string]interface{}, name string) ([]string, bool, error) {
	value, ok := params[name]
	if !ok {
		return nil, false, nil
	}
	converted, ok := schemas.SafeExtractStringSlice(value)
	if !ok {
		return nil, true, fmt.Errorf("extra parameter %q must be an array of strings", name)
	}
	delete(params, name)
	return converted, true, nil
}

func consumeMapExtraParam(params map[string]interface{}, name string) (map[string]interface{}, bool, error) {
	value, ok := params[name]
	if !ok {
		return nil, false, nil
	}
	converted, ok := value.(map[string]interface{})
	if !ok {
		return nil, true, fmt.Errorf("extra parameter %q must be an object", name)
	}
	delete(params, name)
	return converted, true, nil
}
