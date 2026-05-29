package gigachat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
)

// ToGigaChatChatRequest converts a Bifrost chat request to GigaChat v1 format.
func ToGigaChatChatRequest(_ *schemas.BifrostContext, bifrostReq *schemas.BifrostChatRequest) (*GigaChatChatRequest, error) {
	if bifrostReq == nil {
		return nil, fmt.Errorf("bifrost chat request is nil")
	}
	if strings.TrimSpace(bifrostReq.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if len(bifrostReq.Input) == 0 {
		return nil, fmt.Errorf("messages are required")
	}

	toolCallNamesByID := collectGigaChatChatToolCallNames(bifrostReq.Input)
	messages := make([]GigaChatChatMessage, 0, len(bifrostReq.Input))
	for index, message := range bifrostReq.Input {
		convertedMessage, err := toGigaChatChatMessage(message, toolCallNamesByID)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", index, err)
		}
		messages = append(messages, convertedMessage)
	}

	gigaChatReq := &GigaChatChatRequest{
		Model:    bifrostReq.Model,
		Messages: messages,
		Stream:   schemas.Ptr(false),
	}
	if bifrostReq.Params == nil {
		return gigaChatReq, nil
	}

	if unsupportedParams := unsupportedGigaChatChatParams(bifrostReq.Params); len(unsupportedParams) > 0 {
		return nil, fmt.Errorf("GigaChat v1 chat completions do not support parameter(s): %s", strings.Join(unsupportedParams, ", "))
	}

	gigaChatReq.Temperature = bifrostReq.Params.Temperature
	gigaChatReq.TopP = bifrostReq.Params.TopP
	gigaChatReq.MaxTokens = bifrostReq.Params.MaxCompletionTokens
	gigaChatReq.N = bifrostReq.Params.N
	gigaChatReq.Stop = bifrostReq.Params.Stop
	gigaChatReq.ExtraParams = bifrostReq.Params.ExtraParams
	functions, functionNames, err := toGigaChatChatFunctions(bifrostReq.Params.Tools)
	if err != nil {
		return nil, err
	}
	gigaChatReq.Functions = functions
	functionCall, err := toGigaChatChatFunctionCall(bifrostReq.Params.ToolChoice, functionNames)
	if err != nil {
		return nil, err
	}
	gigaChatReq.FunctionCall = functionCall

	return gigaChatReq, nil
}

// ToGigaChatChatStreamRequest converts a Bifrost chat request to a streaming GigaChat v1 request.
func ToGigaChatChatStreamRequest(ctx *schemas.BifrostContext, bifrostReq *schemas.BifrostChatRequest) (*GigaChatChatRequest, error) {
	gigaChatReq, err := ToGigaChatChatRequest(ctx, bifrostReq)
	if err != nil {
		return nil, err
	}
	gigaChatReq.Stream = schemas.Ptr(true)
	return gigaChatReq, nil
}

// ToBifrostChatResponse converts a GigaChat v1 chat response to Bifrost format.
func ToBifrostChatResponse(providerName schemas.ModelProvider, response *GigaChatChatResponse) *schemas.BifrostChatResponse {
	if response == nil {
		return nil
	}

	choices := make([]schemas.BifrostResponseChoice, 0, len(response.Choices))
	for _, choice := range response.Choices {
		choices = append(choices, schemas.BifrostResponseChoice{
			Index:                       choice.Index,
			FinishReason:                toBifrostGigaChatFinishReason(choice.FinishReason),
			LogProbs:                    choice.LogProbs,
			ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{Message: toBifrostGigaChatMessage(choice.Message)},
		})
	}

	return &schemas.BifrostChatResponse{
		ID:                response.ID,
		Choices:           choices,
		Created:           response.Created,
		Model:             response.Model,
		Object:            response.Object,
		SystemFingerprint: response.SystemFingerprint,
		Usage:             toBifrostGigaChatUsage(response.Usage),
		ExtraParams:       response.ExtraParams,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Provider: providerName,
		},
	}
}

// ToBifrostChatStreamResponse converts a GigaChat v1 chat SSE chunk to Bifrost format.
func ToBifrostChatStreamResponse(providerName schemas.ModelProvider, response *GigaChatChatStreamResponse) *schemas.BifrostChatResponse {
	if response == nil {
		return nil
	}

	choices := make([]schemas.BifrostResponseChoice, 0, len(response.Choices))
	for _, choice := range response.Choices {
		choices = append(choices, schemas.BifrostResponseChoice{
			Index:        choice.Index,
			FinishReason: toBifrostGigaChatFinishReason(choice.FinishReason),
			LogProbs:     choice.LogProbs,
			ChatStreamResponseChoice: &schemas.ChatStreamResponseChoice{
				Delta: toBifrostGigaChatStreamDelta(choice.Index, choice.Delta),
			},
		})
	}

	return &schemas.BifrostChatResponse{
		ID:                response.ID,
		Choices:           choices,
		Created:           response.Created,
		Model:             response.Model,
		Object:            response.Object,
		SystemFingerprint: response.SystemFingerprint,
		Usage:             toBifrostGigaChatUsage(response.Usage),
		ExtraParams:       response.ExtraParams,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Provider: providerName,
		},
	}
}

func handleGigaChatChatStreamResponse(providerName schemas.ModelProvider) func([]byte, *schemas.BifrostChatResponse, []byte, bool, bool) (interface{}, interface{}, *schemas.BifrostError) {
	return func(responseBody []byte, response *schemas.BifrostChatResponse, requestBody []byte, sendBackRawRequest bool, sendBackRawResponse bool) (interface{}, interface{}, *schemas.BifrostError) {
		if bifrostErr := parseGigaChatStreamError(responseBody, providerName); bifrostErr != nil {
			rawRequest, rawResponse, _ := providerUtils.HandleProviderResponse(responseBody, &GigaChatErrorResponse{}, requestBody, sendBackRawRequest, sendBackRawResponse)
			return rawRequest, rawResponse, bifrostErr
		}

		var gigaChatResponse GigaChatChatStreamResponse
		rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, &gigaChatResponse, requestBody, sendBackRawRequest, sendBackRawResponse)
		if bifrostErr != nil {
			return rawRequest, rawResponse, bifrostErr
		}

		converted := ToBifrostChatStreamResponse(providerName, &gigaChatResponse)
		if converted == nil {
			return rawRequest, rawResponse, newGigaChatProviderResponseError("GigaChat chat completion stream response is empty", nil)
		}
		*response = *converted
		return rawRequest, rawResponse, nil
	}
}

func withGigaChatChatResponseProvider(providerName schemas.ModelProvider) func(*schemas.BifrostChatResponse) *schemas.BifrostChatResponse {
	return func(response *schemas.BifrostChatResponse) *schemas.BifrostChatResponse {
		if response != nil {
			response.ExtraFields.Provider = providerName
		}
		return response
	}
}

func toGigaChatChatMessage(message schemas.ChatMessage, toolCallNamesByID map[string]string) (GigaChatChatMessage, error) {
	switch message.Role {
	case schemas.ChatMessageRoleSystem, schemas.ChatMessageRoleUser, schemas.ChatMessageRoleAssistant:
	case schemas.ChatMessageRoleTool:
		return toGigaChatFunctionResultMessage(message, toolCallNamesByID)
	case schemas.ChatMessageRoleDeveloper:
		return GigaChatChatMessage{}, fmt.Errorf("developer messages are not supported by GigaChat v1 chat completions")
	default:
		return GigaChatChatMessage{}, fmt.Errorf("unsupported role %q", message.Role)
	}
	if message.ChatToolMessage != nil {
		return GigaChatChatMessage{}, fmt.Errorf("tool message fields are not supported by GigaChat v1 chat completions")
	}
	if message.ChatAssistantMessage != nil {
		if len(message.ChatAssistantMessage.ToolCalls) > 0 {
			return toGigaChatAssistantFunctionCallMessage(message)
		}
		if message.ChatAssistantMessage.Refusal != nil ||
			message.ChatAssistantMessage.Audio != nil ||
			message.ChatAssistantMessage.Reasoning != nil ||
			len(message.ChatAssistantMessage.ReasoningDetails) > 0 ||
			len(message.ChatAssistantMessage.Annotations) > 0 {
			return GigaChatChatMessage{}, fmt.Errorf("assistant-only OpenAI metadata is not supported by GigaChat v1 chat completions")
		}
	}

	content, err := toGigaChatChatMessageContent(message.Content)
	if err != nil {
		return GigaChatChatMessage{}, err
	}

	return GigaChatChatMessage{
		Role:    string(message.Role),
		Content: content,
		Name:    message.Name,
	}, nil
}

func collectGigaChatChatToolCallNames(messages []schemas.ChatMessage) map[string]string {
	toolCallNamesByID := make(map[string]string)
	for _, message := range messages {
		if message.ChatAssistantMessage == nil {
			continue
		}
		for _, toolCall := range message.ChatAssistantMessage.ToolCalls {
			if toolCall.ID == nil || strings.TrimSpace(*toolCall.ID) == "" || toolCall.Function.Name == nil || strings.TrimSpace(*toolCall.Function.Name) == "" {
				continue
			}
			toolCallNamesByID[strings.TrimSpace(*toolCall.ID)] = strings.TrimSpace(*toolCall.Function.Name)
		}
	}
	return toolCallNamesByID
}

func toGigaChatAssistantFunctionCallMessage(message schemas.ChatMessage) (GigaChatChatMessage, error) {
	if message.ChatAssistantMessage == nil || len(message.ChatAssistantMessage.ToolCalls) == 0 {
		return GigaChatChatMessage{}, fmt.Errorf("assistant function_call is required")
	}
	if len(message.ChatAssistantMessage.ToolCalls) > 1 {
		return GigaChatChatMessage{}, fmt.Errorf("GigaChat v1 chat completions support one function call per assistant message")
	}
	toolCall := message.ChatAssistantMessage.ToolCalls[0]
	if toolCall.Type != nil && *toolCall.Type != "" && *toolCall.Type != string(schemas.ChatToolTypeFunction) {
		return GigaChatChatMessage{}, fmt.Errorf("assistant tool call type %q is not supported by GigaChat v1 chat completions", *toolCall.Type)
	}
	if toolCall.Function.Name == nil || strings.TrimSpace(*toolCall.Function.Name) == "" {
		return GigaChatChatMessage{}, fmt.Errorf("assistant function_call name is required")
	}
	arguments, err := parseGigaChatChatFunctionArguments(toolCall.Function.Arguments)
	if err != nil {
		return GigaChatChatMessage{}, err
	}

	content, err := toGigaChatChatMessageContent(message.Content)
	if err != nil {
		return GigaChatChatMessage{}, err
	}
	if content == nil {
		content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr("")}
	}

	return GigaChatChatMessage{
		Role:    string(schemas.ChatMessageRoleAssistant),
		Content: content,
		Name:    message.Name,
		FunctionCall: &GigaChatFunctionCall{
			Name:      strings.TrimSpace(*toolCall.Function.Name),
			Arguments: arguments,
		},
		FunctionsStateID: toolCall.ID,
	}, nil
}

func toGigaChatFunctionResultMessage(message schemas.ChatMessage, toolCallNamesByID map[string]string) (GigaChatChatMessage, error) {
	if message.ChatToolMessage == nil {
		return GigaChatChatMessage{}, fmt.Errorf("function result message requires tool message fields")
	}
	name := ""
	if message.Name != nil {
		name = strings.TrimSpace(*message.Name)
	}
	if name == "" && message.ChatToolMessage.ToolCallID != nil {
		name = toolCallNamesByID[strings.TrimSpace(*message.ChatToolMessage.ToolCallID)]
	}
	if name == "" {
		return GigaChatChatMessage{}, fmt.Errorf("function result message requires function name or matching tool_call_id")
	}
	content, err := toGigaChatChatMessageContent(message.Content)
	if err != nil {
		return GigaChatChatMessage{}, err
	}
	if content == nil || content.ContentStr == nil || strings.TrimSpace(*content.ContentStr) == "" {
		return GigaChatChatMessage{}, fmt.Errorf("function result message content is required")
	}
	trimmedContent := bytes.TrimSpace([]byte(*content.ContentStr))
	if !json.Valid(trimmedContent) || len(trimmedContent) == 0 || trimmedContent[0] != '{' {
		return GigaChatChatMessage{}, fmt.Errorf("function result message content must be a JSON object string")
	}

	return GigaChatChatMessage{
		Role:    "function",
		Content: content,
		Name:    &name,
	}, nil
}

func toGigaChatChatMessageContent(content *schemas.ChatMessageContent) (*schemas.ChatMessageContent, error) {
	if content == nil {
		return nil, nil
	}
	if content.ContentStr != nil {
		return content, nil
	}
	if len(content.ContentBlocks) == 0 {
		return content, nil
	}

	var textBuilder strings.Builder
	for index, block := range content.ContentBlocks {
		if block.Type != schemas.ChatContentBlockTypeText {
			return nil, fmt.Errorf("content block %d with type %q is not supported by GigaChat v1 chat completions", index, block.Type)
		}
		if block.Text != nil {
			textBuilder.WriteString(*block.Text)
		}
	}
	text := textBuilder.String()
	return &schemas.ChatMessageContent{ContentStr: &text}, nil
}

func unsupportedGigaChatChatParams(params *schemas.ChatParameters) []string {
	if params == nil {
		return nil
	}

	unsupported := make([]string, 0)
	addIf := func(condition bool, name string) {
		if condition {
			unsupported = append(unsupported, name)
		}
	}

	addIf(params.Audio != nil, "audio")
	addIf(params.FrequencyPenalty != nil, "frequency_penalty")
	addIf(params.LogitBias != nil, "logit_bias")
	addIf(params.LogProbs != nil && *params.LogProbs, "logprobs")
	addIf(params.Metadata != nil && len(*params.Metadata) > 0, "metadata")
	addIf(len(params.Modalities) > 0, "modalities")
	addIf(params.ParallelToolCalls != nil && *params.ParallelToolCalls, "parallel_tool_calls")
	addIf(params.Prediction != nil, "prediction")
	addIf(params.PresencePenalty != nil, "presence_penalty")
	addIf(params.PromptCacheKey != nil, "prompt_cache_key")
	addIf(params.PromptCacheRetention != nil, "prompt_cache_retention")
	addIf(params.Reasoning != nil, "reasoning")
	addIf(params.ResponseFormat != nil, "response_format")
	addIf(params.SafetyIdentifier != nil, "safety_identifier")
	addIf(params.Seed != nil, "seed")
	addIf(params.ServiceTier != nil, "service_tier")
	addIf(params.StreamOptions != nil, "stream_options")
	addIf(params.Store != nil && *params.Store, "store")
	addIf(params.TopLogProbs != nil, "top_logprobs")
	addIf(params.User != nil, "user")
	addIf(params.Verbosity != nil, "verbosity")
	addIf(params.WebSearchOptions != nil, "web_search_options")
	addIf(params.TopK != nil, "top_k")
	addIf(params.Speed != nil, "speed")
	addIf(params.InferenceGeo != nil, "inference_geo")
	addIf(len(params.MCPServers) > 0, "mcp_servers")
	addIf(params.Container != nil, "container")
	addIf(params.CacheControl != nil, "cache_control")
	addIf(params.TaskBudget != nil, "task_budget")
	addIf(len(bytes.TrimSpace(params.ContextManagement)) > 0, "context_management")
	unsupported = append(unsupported, unsupportedGigaChatToolControlExtraParams(params.ExtraParams, "functions", "function_call", "tools", "tool_config", "parallel_tool_calls")...)

	sort.Strings(unsupported)
	return unsupported
}

func parseGigaChatChatFunctionArguments(arguments string) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace([]byte(arguments))
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(trimmed) || trimmed[0] != '{' {
		return nil, fmt.Errorf("function_call arguments must be a JSON object")
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, trimmed); err != nil {
		return nil, fmt.Errorf("function_call arguments must be valid JSON: %w", err)
	}
	return json.RawMessage(compacted.Bytes()), nil
}

func toBifrostGigaChatMessage(message *GigaChatChatMessage) *schemas.ChatMessage {
	if message == nil {
		return nil
	}

	role := schemas.ChatMessageRole(message.Role)
	if role == "" {
		role = schemas.ChatMessageRoleAssistant
	}

	bifrostMessage := &schemas.ChatMessage{
		Role:    role,
		Content: message.Content,
		Name:    message.Name,
	}
	if message.FunctionCall != nil {
		arguments := compactGigaChatFunctionArguments(message.FunctionCall.Arguments)
		toolCallType := string(schemas.ChatToolTypeFunction)
		toolCall := schemas.ChatAssistantMessageToolCall{
			Type: &toolCallType,
			ID:   message.FunctionsStateID,
			Function: schemas.ChatAssistantMessageToolCallFunction{
				Name:      &message.FunctionCall.Name,
				Arguments: arguments,
			},
		}
		bifrostMessage.ChatAssistantMessage = &schemas.ChatAssistantMessage{
			ToolCalls: []schemas.ChatAssistantMessageToolCall{toolCall},
		}
	}
	return bifrostMessage
}

func toBifrostGigaChatStreamDelta(index int, delta *GigaChatChatStreamDelta) *schemas.ChatStreamResponseChoiceDelta {
	if delta == nil {
		return &schemas.ChatStreamResponseChoiceDelta{}
	}

	bifrostDelta := &schemas.ChatStreamResponseChoiceDelta{
		Role:    delta.Role,
		Content: delta.Content,
	}
	if delta.FunctionCall != nil {
		arguments := compactGigaChatFunctionArguments(delta.FunctionCall.Arguments)
		toolCallType := string(schemas.ChatToolTypeFunction)
		bifrostDelta.ToolCalls = []schemas.ChatAssistantMessageToolCall{
			{
				Index: uint16(index),
				Type:  &toolCallType,
				ID:    delta.FunctionsStateID,
				Function: schemas.ChatAssistantMessageToolCallFunction{
					Name:      &delta.FunctionCall.Name,
					Arguments: arguments,
				},
			},
		}
	}
	return bifrostDelta
}

func compactGigaChatFunctionArguments(arguments json.RawMessage) string {
	if len(arguments) == 0 {
		return ""
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, arguments); err != nil {
		return string(arguments)
	}
	return compacted.String()
}

func toBifrostGigaChatFinishReason(finishReason *string) *string {
	if finishReason == nil {
		return nil
	}
	if *finishReason == "function_call" {
		return schemas.Ptr(string(schemas.BifrostFinishReasonToolCalls))
	}
	return finishReason
}

func toBifrostGigaChatUsage(usage *GigaChatChatUsage) *schemas.BifrostLLMUsage {
	if usage == nil {
		return nil
	}
	promptTokens := usage.PromptTokens
	if promptTokens == 0 && usage.InputTokens > 0 {
		promptTokens = usage.InputTokens
	}
	completionTokens := usage.CompletionTokens
	if completionTokens == 0 && usage.OutputTokens > 0 {
		completionTokens = usage.OutputTokens
	}
	totalTokens := usage.TotalTokens
	if totalTokens == 0 && promptTokens+completionTokens > 0 {
		totalTokens = promptTokens + completionTokens
	}

	bifrostUsage := &schemas.BifrostLLMUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}
	cachedTokens := usage.PrecachedPromptTokens
	if cachedTokens == 0 && usage.InputTokensDetails != nil {
		cachedTokens = usage.InputTokensDetails.CachedTokens
		if cachedTokens == 0 {
			cachedTokens = usage.InputTokensDetails.CachedReadTokens
		}
	}
	if cachedTokens > 0 {
		bifrostUsage.PromptTokensDetails = &schemas.ChatPromptTokensDetails{
			CachedReadTokens: cachedTokens,
		}
	}
	return bifrostUsage
}

func parseGigaChatStreamError(responseBody []byte, providerName schemas.ModelProvider) *schemas.BifrostError {
	var errorResp GigaChatErrorResponse
	if err := json.Unmarshal(responseBody, &errorResp); err != nil {
		return nil
	}
	if errorResp.Status == nil && errorResp.Code == nil && strings.TrimSpace(errorResp.Message) == "" {
		return nil
	}

	statusCode := http.StatusBadGateway
	if errorResp.Status != nil {
		statusCode = *errorResp.Status
	}

	bifrostErr := &schemas.BifrostError{
		IsBifrostError: false,
		StatusCode:     &statusCode,
		Error:          &schemas.ErrorField{},
		ExtraFields: schemas.BifrostErrorExtraFields{
			Provider: providerName,
		},
	}
	if strings.TrimSpace(errorResp.Message) != "" {
		bifrostErr.Error.Message = errorResp.Message
	} else {
		bifrostErr.Error.Message = fmt.Sprintf("GigaChat API error (status %d)", statusCode)
	}
	if errorResp.Code != nil {
		code := fmt.Sprintf("%d", *errorResp.Code)
		bifrostErr.Error.Code = &code
	} else if errorResp.Status != nil {
		code := fmt.Sprintf("%d", *errorResp.Status)
		bifrostErr.Error.Code = &code
	}
	return bifrostErr
}
