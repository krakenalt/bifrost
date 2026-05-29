package gigachat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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

	messages := make([]GigaChatChatMessage, 0, len(bifrostReq.Input))
	for index, message := range bifrostReq.Input {
		convertedMessage, err := toGigaChatChatMessage(message)
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

func toGigaChatChatMessage(message schemas.ChatMessage) (GigaChatChatMessage, error) {
	switch message.Role {
	case schemas.ChatMessageRoleSystem, schemas.ChatMessageRoleUser, schemas.ChatMessageRoleAssistant:
	case schemas.ChatMessageRoleTool:
		return GigaChatChatMessage{}, fmt.Errorf("tool messages require GigaChat function-call mapping, which is not enabled for v1 chat completions yet")
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
			return GigaChatChatMessage{}, fmt.Errorf("assistant tool calls are not supported by GigaChat v1 chat completions yet")
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
	addIf(params.ToolChoice != nil, "tool_choice")
	addIf(len(params.Tools) > 0, "tools")
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

	sort.Strings(unsupported)
	return unsupported
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
	bifrostUsage := &schemas.BifrostLLMUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.PrecachedPromptTokens > 0 {
		bifrostUsage.PromptTokensDetails = &schemas.ChatPromptTokensDetails{
			CachedReadTokens: usage.PrecachedPromptTokens,
		}
	}
	return bifrostUsage
}
