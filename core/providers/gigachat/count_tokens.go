package gigachat

import (
	"encoding/json"
	"fmt"
	"strings"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

// GigaChatCountTokensRequest is the v1 /tokens/count request body.
type GigaChatCountTokensRequest struct {
	Model       string                 `json:"model"`
	Input       []string               `json:"input"`
	ExtraParams map[string]interface{} `json:"-"`
}

// GetExtraParams returns provider-specific passthrough fields.
func (request *GigaChatCountTokensRequest) GetExtraParams() map[string]interface{} {
	if request == nil || request.ExtraParams == nil {
		return make(map[string]interface{}, 0)
	}
	return request.ExtraParams
}

// GigaChatCountTokensResponse accepts the documented SDK/API response shapes for
// /tokens/count: a root array, a {data:[...]} wrapper, or an aggregate object.
type GigaChatCountTokensResponse struct {
	Object     string                    `json:"object,omitempty"`
	Model      string                    `json:"model,omitempty"`
	Data       []GigaChatCountTokensItem `json:"data,omitempty"`
	Tokens     *int                      `json:"tokens,omitempty"`
	Characters *int                      `json:"characters,omitempty"`
	Items      []GigaChatCountTokensItem `json:"-"`
}

// GigaChatCountTokensItem is one token count result for one input string.
type GigaChatCountTokensItem struct {
	Tokens     int `json:"tokens"`
	Characters int `json:"characters,omitempty"`
}

func (response *GigaChatCountTokensResponse) UnmarshalJSON(data []byte) error {
	var items []GigaChatCountTokensItem
	if err := json.Unmarshal(data, &items); err == nil {
		response.Items = items
		response.Data = items
		return nil
	}

	type Alias GigaChatCountTokensResponse
	var object Alias
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}

	*response = GigaChatCountTokensResponse(object)
	if len(response.Data) > 0 {
		response.Items = response.Data
		return nil
	}
	if response.Tokens != nil {
		item := GigaChatCountTokensItem{Tokens: *response.Tokens}
		if response.Characters != nil {
			item.Characters = *response.Characters
		}
		response.Items = []GigaChatCountTokensItem{item}
	}
	return nil
}

// ToGigaChatCountTokensRequest converts a Bifrost Responses request to the
// GigaChat /tokens/count payload.
func ToGigaChatCountTokensRequest(bifrostReq *schemas.BifrostResponsesRequest) (*GigaChatCountTokensRequest, error) {
	if bifrostReq == nil {
		return nil, fmt.Errorf("bifrost count tokens request is nil")
	}
	if strings.TrimSpace(bifrostReq.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}

	input, err := toGigaChatCountTokensInput(bifrostReq.Input)
	if err != nil {
		return nil, err
	}

	return &GigaChatCountTokensRequest{
		Model: bifrostReq.Model,
		Input: input,
	}, nil
}

// ToBifrostCountTokensResponse converts a GigaChat /tokens/count response to the
// shared Bifrost count tokens response.
func ToBifrostCountTokensResponse(providerName schemas.ModelProvider, response *GigaChatCountTokensResponse, model string) *schemas.BifrostCountTokensResponse {
	if response == nil {
		return nil
	}

	counts := response.Items
	if len(counts) == 0 && len(response.Data) > 0 {
		counts = response.Data
	}
	if len(counts) == 0 && response.Tokens != nil {
		counts = []GigaChatCountTokensItem{{Tokens: *response.Tokens}}
	}
	if len(counts) == 0 {
		return nil
	}

	tokens := make([]int, 0, len(counts))
	inputTokens := 0
	for _, count := range counts {
		tokens = append(tokens, count.Tokens)
		inputTokens += count.Tokens
	}
	totalTokens := inputTokens
	object := response.Object
	if strings.TrimSpace(object) == "" {
		object = "response.input_tokens"
	}
	if strings.TrimSpace(model) == "" {
		model = response.Model
	}

	return &schemas.BifrostCountTokensResponse{
		Object:      object,
		Model:       model,
		InputTokens: inputTokens,
		InputTokensDetails: &schemas.ResponsesResponseInputTokens{
			TextTokens: inputTokens,
		},
		Tokens:      tokens,
		TotalTokens: &totalTokens,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Provider: providerName,
		},
	}
}

func toGigaChatCountTokensInput(messages []schemas.ResponsesMessage) ([]string, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("count tokens input is required")
	}

	input := make([]string, 0, len(messages))
	for index, message := range messages {
		parts, err := toGigaChatCountTokensMessageInput(index, message)
		if err != nil {
			return nil, err
		}
		input = append(input, parts...)
	}
	if len(input) == 0 {
		return nil, fmt.Errorf("count tokens text input is empty after conversion")
	}
	return input, nil
}

func toGigaChatCountTokensMessageInput(index int, message schemas.ResponsesMessage) ([]string, error) {
	parts := make([]string, 0, 1)
	if message.Content != nil {
		contentParts, err := toGigaChatCountTokensContentInput(index, message.Content)
		if err != nil {
			return nil, err
		}
		parts = append(parts, contentParts...)
	}

	if message.ResponsesReasoning != nil {
		if message.ResponsesReasoning.EncryptedContent != nil && strings.TrimSpace(*message.ResponsesReasoning.EncryptedContent) != "" {
			return nil, fmt.Errorf("GigaChat count tokens supports only text input; input[%d] contains encrypted reasoning content", index)
		}
		for _, summary := range message.ResponsesReasoning.Summary {
			if text := strings.TrimSpace(summary.Text); text != "" {
				parts = append(parts, summary.Text)
			}
		}
	}

	if len(parts) == 0 && hasGigaChatCountTokensNonTextMessagePayload(message) {
		return nil, fmt.Errorf("GigaChat count tokens supports only text input; input[%d] contains a non-text item", index)
	}
	return parts, nil
}

func toGigaChatCountTokensContentInput(index int, content *schemas.ResponsesMessageContent) ([]string, error) {
	if content.ContentStr != nil && content.ContentBlocks != nil {
		return nil, fmt.Errorf("input[%d].content cannot contain both string content and content blocks", index)
	}

	if content.ContentStr != nil {
		if strings.TrimSpace(*content.ContentStr) == "" {
			return nil, nil
		}
		return []string{*content.ContentStr}, nil
	}

	parts := make([]string, 0, len(content.ContentBlocks))
	for blockIndex, block := range content.ContentBlocks {
		text, ok, err := toGigaChatCountTokensContentBlockText(index, blockIndex, block)
		if err != nil {
			return nil, err
		}
		if ok && strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return parts, nil
}

func toGigaChatCountTokensContentBlockText(index int, blockIndex int, block schemas.ResponsesMessageContentBlock) (string, bool, error) {
	if isGigaChatCountTokensUnsupportedMediaBlock(block) {
		return "", false, fmt.Errorf("GigaChat count tokens supports only text input; input[%d].content[%d] contains file, image, or audio content", index, blockIndex)
	}

	if block.Text != nil {
		return *block.Text, true, nil
	}
	if block.ResponsesOutputMessageContentRefusal != nil && strings.TrimSpace(block.ResponsesOutputMessageContentRefusal.Refusal) != "" {
		return block.ResponsesOutputMessageContentRefusal.Refusal, true, nil
	}

	switch block.Type {
	case schemas.ResponsesInputMessageContentBlockTypeText,
		schemas.ResponsesOutputMessageContentTypeText,
		schemas.ResponsesOutputMessageContentTypeReasoning,
		schemas.ResponsesOutputMessageContentTypeRefusal:
		return "", false, nil
	case "":
		if !hasGigaChatCountTokensNonTextBlockPayload(block) {
			return "", false, nil
		}
	}

	return "", false, fmt.Errorf("GigaChat count tokens supports only text input; input[%d].content[%d] has unsupported content type %q", index, blockIndex, block.Type)
}

func isGigaChatCountTokensUnsupportedMediaBlock(block schemas.ResponsesMessageContentBlock) bool {
	return block.FileID != nil ||
		block.ResponsesInputMessageContentBlockImage != nil ||
		block.ResponsesInputMessageContentBlockFile != nil ||
		block.Audio != nil ||
		block.Type == schemas.ResponsesInputMessageContentBlockTypeImage ||
		block.Type == schemas.ResponsesInputMessageContentBlockTypeFile ||
		block.Type == schemas.ResponsesInputMessageContentBlockTypeAudio
}

func hasGigaChatCountTokensNonTextBlockPayload(block schemas.ResponsesMessageContentBlock) bool {
	return block.Signature != nil ||
		block.ResponsesOutputMessageContentText != nil ||
		block.ResponsesOutputMessageContentRenderedContent != nil ||
		block.ResponsesOutputMessageContentCompaction != nil ||
		block.CacheControl != nil ||
		block.Citations != nil
}

func hasGigaChatCountTokensNonTextMessagePayload(message schemas.ResponsesMessage) bool {
	return message.ResponsesToolMessage != nil ||
		message.CacheControl != nil ||
		message.Type != nil && *message.Type != schemas.ResponsesMessageTypeMessage
}
