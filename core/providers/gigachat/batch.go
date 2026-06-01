package gigachat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	openaiProvider "github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
)

type openAICompatibleBatchInputRow struct {
	CustomID string          `json:"custom_id"`
	Method   string          `json:"method,omitempty"`
	URL      string          `json:"url,omitempty"`
	Body     json.RawMessage `json:"body,omitempty"`
}

func toGigaChatBatchMethod(endpoint schemas.BatchEndpoint) (GigaChatBatchMethod, error) {
	switch normalizeGigaChatBatchEndpoint(string(endpoint)) {
	case "/v1/chat/completions", "/chat/completions":
		return GigaChatBatchMethodChatCompletions, nil
	case "/v1/responses", "/responses":
		return GigaChatBatchMethodChatCompletions, nil
	case "/v1/embeddings", "/embeddings":
		return GigaChatBatchMethodEmbedder, nil
	default:
		return "", fmt.Errorf("GigaChat batches do not support endpoint %q", endpoint)
	}
}

func normalizeGigaChatBatchEndpoint(endpoint string) string {
	normalized := strings.ToLower(strings.TrimSpace(endpoint))
	if normalized == "" {
		return ""
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return strings.TrimRight(normalized, "/")
}

func toBifrostGigaChatBatchStatus(status GigaChatBatchStatus) schemas.BatchStatus {
	switch status {
	case GigaChatBatchStatusCreated:
		return schemas.BatchStatusValidating
	case GigaChatBatchStatusInProgress:
		return schemas.BatchStatusInProgress
	case GigaChatBatchStatusCompleted:
		return schemas.BatchStatusCompleted
	default:
		return schemas.BatchStatus(status)
	}
}

func toBifrostGigaChatBatchProviderExtraFields(batch GigaChatBatch) map[string]interface{} {
	fields := make(map[string]interface{})
	if batch.Method != "" {
		fields["gigachat_batch_method"] = string(batch.Method)
	}
	switch batch.Status {
	case "", GigaChatBatchStatusCreated, GigaChatBatchStatusInProgress, GigaChatBatchStatusCompleted:
	default:
		fields["gigachat_batch_status"] = string(batch.Status)
	}
	if batch.ResultFileID != nil && strings.TrimSpace(*batch.ResultFileID) != "" {
		fields["gigachat_result_file_id"] = strings.TrimSpace(*batch.ResultFileID)
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func convertGigaChatBatchRequestItemsToJSONL(endpoint schemas.BatchEndpoint, requests []schemas.BatchRequestItem) ([]byte, error) {
	var buf bytes.Buffer
	for index, request := range requests {
		row, err := toGigaChatBatchInputRowFromRequestItem(endpoint, request)
		if err != nil {
			return nil, fmt.Errorf("requests[%d]: %w", index, err)
		}
		if err := writeGigaChatBatchInputRow(&buf, row); err != nil {
			return nil, fmt.Errorf("requests[%d]: %w", index, err)
		}
	}
	return buf.Bytes(), nil
}

func convertGigaChatBatchInputJSONL(endpoint schemas.BatchEndpoint, input []byte) ([]byte, error) {
	var buf bytes.Buffer
	parseResult := providerUtils.ParseJSONL(input, func(line []byte) error {
		row, err := toGigaChatBatchInputRowFromJSONLine(endpoint, line)
		if err != nil {
			return err
		}
		return writeGigaChatBatchInputRow(&buf, row)
	})
	if len(parseResult.Errors) > 0 {
		return nil, formatGigaChatBatchJSONLErrors(parseResult.Errors)
	}
	return buf.Bytes(), nil
}

func toGigaChatBatchInputRowFromRequestItem(defaultEndpoint schemas.BatchEndpoint, request schemas.BatchRequestItem) (GigaChatBatchInputRow, error) {
	if len(request.Params) > 0 {
		return GigaChatBatchInputRow{}, fmt.Errorf("params are not supported by GigaChat batch row conversion")
	}
	if request.Body == nil {
		return GigaChatBatchInputRow{}, fmt.Errorf("body is required")
	}
	body, err := schemas.MarshalSorted(request.Body)
	if err != nil {
		return GigaChatBatchInputRow{}, fmt.Errorf("marshal body: %w", err)
	}
	row := openAICompatibleBatchInputRow{
		CustomID: request.CustomID,
		Method:   request.Method,
		URL:      request.URL,
		Body:     body,
	}
	return toGigaChatBatchInputRow(defaultEndpoint, row)
}

func toGigaChatBatchInputRowFromJSONLine(defaultEndpoint schemas.BatchEndpoint, line []byte) (GigaChatBatchInputRow, error) {
	var row openAICompatibleBatchInputRow
	if err := json.Unmarshal(line, &row); err != nil {
		return GigaChatBatchInputRow{}, fmt.Errorf("decode batch row: %w", err)
	}
	return toGigaChatBatchInputRow(defaultEndpoint, row)
}

func toGigaChatBatchInputRow(defaultEndpoint schemas.BatchEndpoint, row openAICompatibleBatchInputRow) (GigaChatBatchInputRow, error) {
	customID := strings.TrimSpace(row.CustomID)
	if customID == "" {
		return GigaChatBatchInputRow{}, fmt.Errorf("custom_id is required")
	}
	if method := strings.TrimSpace(row.Method); method != "" && !strings.EqualFold(method, http.MethodPost) {
		return GigaChatBatchInputRow{}, fmt.Errorf("method %q is not supported by GigaChat batches", row.Method)
	}
	if len(bytes.TrimSpace(row.Body)) == 0 {
		return GigaChatBatchInputRow{}, fmt.Errorf("body is required")
	}

	endpoint := defaultEndpoint
	if strings.TrimSpace(row.URL) != "" {
		endpoint = schemas.BatchEndpoint(row.URL)
	}
	request, err := toGigaChatBatchRequestBody(endpoint, row.Body)
	if err != nil {
		return GigaChatBatchInputRow{}, err
	}
	return GigaChatBatchInputRow{
		ID:      customID,
		Request: request,
	}, nil
}

func toGigaChatBatchRequestBody(endpoint schemas.BatchEndpoint, body json.RawMessage) (json.RawMessage, error) {
	switch normalizeGigaChatBatchEndpoint(string(endpoint)) {
	case "/v1/chat/completions", "/chat/completions":
		var request openaiProvider.OpenAIChatRequest
		if err := json.Unmarshal(body, &request); err != nil {
			return nil, fmt.Errorf("decode chat completion body: %w", err)
		}
		bifrostReq := request.ToBifrostChatRequest(gigaChatBatchConversionContext())
		if request.MaxTokens != nil {
			if bifrostReq.Params == nil {
				bifrostReq.Params = &schemas.ChatParameters{}
			}
			if bifrostReq.Params.MaxCompletionTokens == nil {
				bifrostReq.Params.MaxCompletionTokens = request.MaxTokens
			}
		}
		bifrostReq.Provider = schemas.GigaChat
		gigaChatReq, err := ToGigaChatChatRequest(gigaChatBatchConversionContext(), bifrostReq)
		if err != nil {
			return nil, err
		}
		return marshalGigaChatBatchRequest(gigaChatReq)
	case "/v1/responses", "/responses":
		var request openaiProvider.OpenAIResponsesRequest
		if err := json.Unmarshal(body, &request); err != nil {
			return nil, fmt.Errorf("decode responses body: %w", err)
		}
		bifrostReq := request.ToBifrostResponsesRequest(gigaChatBatchConversionContext())
		bifrostReq.Provider = schemas.GigaChat
		gigaChatReq, err := ToGigaChatResponsesRequest(bifrostReq)
		if err != nil {
			return nil, err
		}
		return marshalGigaChatBatchRequest(gigaChatReq)
	case "/v1/embeddings", "/embeddings":
		var request openaiProvider.OpenAIEmbeddingRequest
		if err := json.Unmarshal(body, &request); err != nil {
			return nil, fmt.Errorf("decode embeddings body: %w", err)
		}
		bifrostReq := request.ToBifrostEmbeddingRequest(gigaChatBatchConversionContext())
		bifrostReq.Provider = schemas.GigaChat
		gigaChatReq, err := ToGigaChatEmbeddingRequest(bifrostReq)
		if err != nil {
			return nil, err
		}
		return marshalGigaChatBatchRequest(gigaChatReq)
	default:
		return nil, fmt.Errorf("GigaChat batches do not support endpoint %q", endpoint)
	}
}

func marshalGigaChatBatchRequest(request providerUtils.RequestBodyWithExtraParams) (json.RawMessage, error) {
	body, err := schemas.MarshalSorted(request)
	if err != nil {
		return nil, fmt.Errorf("marshal GigaChat batch request: %w", err)
	}
	if extraParams := request.GetExtraParams(); len(extraParams) > 0 {
		body, err = providerUtils.MergeExtraParamsIntoJSON(body, extraParams)
		if err != nil {
			return nil, fmt.Errorf("merge GigaChat batch request extra params: %w", err)
		}
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, body); err != nil {
		return nil, fmt.Errorf("compact GigaChat batch request: %w", err)
	}
	return json.RawMessage(compacted.Bytes()), nil
}

func writeGigaChatBatchInputRow(buf *bytes.Buffer, row GigaChatBatchInputRow) error {
	line, err := schemas.MarshalSorted(row)
	if err != nil {
		return err
	}
	buf.Write(line)
	buf.WriteByte('\n')
	return nil
}

func formatGigaChatBatchJSONLErrors(errors []schemas.BatchError) error {
	if len(errors) == 0 {
		return nil
	}
	messages := make([]string, 0, len(errors))
	for _, parseErr := range errors {
		if parseErr.Line != nil {
			messages = append(messages, fmt.Sprintf("line %d: %s", *parseErr.Line, parseErr.Message))
		} else {
			messages = append(messages, parseErr.Message)
		}
	}
	return fmt.Errorf("failed to convert GigaChat batch JSONL: %s", strings.Join(messages, "; "))
}

func gigaChatBatchConversionContext() *schemas.BifrostContext {
	return schemas.NewBifrostContext(nil, schemas.NoDeadline)
}
