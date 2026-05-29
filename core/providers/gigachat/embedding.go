package gigachat

import (
	"fmt"
	"sort"
	"strings"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

// ToGigaChatEmbeddingRequest converts a Bifrost embedding request to GigaChat v1 format.
func ToGigaChatEmbeddingRequest(bifrostReq *schemas.BifrostEmbeddingRequest) (*GigaChatEmbeddingRequest, error) {
	if bifrostReq == nil {
		return nil, fmt.Errorf("bifrost embedding request is nil")
	}
	if strings.TrimSpace(bifrostReq.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if bifrostReq.Input == nil {
		return nil, fmt.Errorf("input is required")
	}
	if bifrostReq.Input.Text == nil && bifrostReq.Input.Texts == nil {
		return nil, fmt.Errorf("GigaChat embeddings support only string or array-of-string input")
	}

	if unsupportedParams := unsupportedGigaChatEmbeddingParams(bifrostReq.Params); len(unsupportedParams) > 0 {
		return nil, fmt.Errorf("GigaChat embeddings do not support parameter(s): %s", strings.Join(unsupportedParams, ", "))
	}

	return &GigaChatEmbeddingRequest{
		Model: bifrostReq.Model,
		Input: bifrostReq.Input,
	}, nil
}

// ToBifrostEmbeddingResponse converts a GigaChat v1 embeddings response to Bifrost format.
func ToBifrostEmbeddingResponse(providerName schemas.ModelProvider, response *GigaChatEmbeddingResponse) *schemas.BifrostEmbeddingResponse {
	if response == nil {
		return nil
	}

	data := make([]schemas.EmbeddingData, 0, len(response.Data))
	itemUsage := &GigaChatEmbeddingUsage{}
	hasItemUsage := false
	for _, item := range response.Data {
		embedding := append([]float64(nil), item.Embedding...)
		object := item.Object
		if object == "" {
			object = "embedding"
		}
		data = append(data, schemas.EmbeddingData{
			Index:  item.Index,
			Object: object,
			Embedding: schemas.EmbeddingStruct{
				EmbeddingArray: embedding,
			},
		})
		if item.Usage != nil {
			hasItemUsage = true
			itemUsage.PromptTokens += item.Usage.PromptTokens
			itemUsage.TotalTokens += item.Usage.TotalTokens
		}
	}

	object := response.Object
	if object == "" {
		object = "list"
	}

	bifrostResponse := &schemas.BifrostEmbeddingResponse{
		Data:   data,
		Model:  response.Model,
		Object: object,
		Usage:  toBifrostGigaChatEmbeddingUsage(response.Usage),
		ExtraFields: schemas.BifrostResponseExtraFields{
			Provider: providerName,
		},
	}
	if bifrostResponse.Usage == nil && hasItemUsage {
		bifrostResponse.Usage = toBifrostGigaChatEmbeddingUsage(itemUsage)
	}
	return bifrostResponse
}

func unsupportedGigaChatEmbeddingParams(params *schemas.EmbeddingParameters) []string {
	if params == nil {
		return nil
	}

	unsupported := make([]string, 0)
	if params.EncodingFormat != nil {
		unsupported = append(unsupported, "encoding_format")
	}
	if params.Dimensions != nil {
		unsupported = append(unsupported, "dimensions")
	}
	for name := range params.ExtraParams {
		if strings.TrimSpace(name) != "" {
			unsupported = append(unsupported, name)
		}
	}

	sort.Strings(unsupported)
	return unsupported
}

func toBifrostGigaChatEmbeddingUsage(usage *GigaChatEmbeddingUsage) *schemas.BifrostLLMUsage {
	if usage == nil {
		return nil
	}
	totalTokens := usage.TotalTokens
	if totalTokens == 0 && usage.PromptTokens > 0 {
		totalTokens = usage.PromptTokens
	}
	return &schemas.BifrostLLMUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: 0,
		TotalTokens:      totalTokens,
	}
}
