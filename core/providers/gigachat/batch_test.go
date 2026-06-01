package gigachat

import (
	"encoding/json"
	"strings"
	"testing"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

func TestGigaChatBatchTypesJSON(t *testing.T) {
	t.Parallel()

	resultFileID := "file-result"
	raw, err := json.Marshal(GigaChatBatches{Data: []GigaChatBatch{{
		ID:           "batch-1",
		Object:       "batch",
		Method:       GigaChatBatchMethodChatCompletions,
		Status:       GigaChatBatchStatusCompleted,
		ResultFileID: &resultFileID,
		RequestCounts: &GigaChatBatchRequestCounts{
			Total:     3,
			Completed: 2,
			Failed:    1,
		},
	}}})
	if err != nil {
		t.Fatalf("marshal GigaChatBatches returned error: %v", err)
	}

	var decoded GigaChatBatches
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal GigaChatBatches returned error: %v", err)
	}
	if len(decoded.Data) != 1 {
		t.Fatalf("decoded %d batches, want 1", len(decoded.Data))
	}
	batch := decoded.Data[0]
	if batch.ID != "batch-1" || batch.Method != GigaChatBatchMethodChatCompletions || batch.Status != GigaChatBatchStatusCompleted {
		t.Fatalf("decoded batch mismatch: %#v", batch)
	}
	if batch.RequestCounts == nil || batch.RequestCounts.Total != 3 || batch.RequestCounts.Completed != 2 || batch.RequestCounts.Failed != 1 {
		t.Fatalf("request counts mismatch: %#v", batch.RequestCounts)
	}
}

func TestToGigaChatBatchMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint schemas.BatchEndpoint
		want     GigaChatBatchMethod
		wantErr  string
	}{
		{name: "chat completions", endpoint: schemas.BatchEndpointChatCompletions, want: GigaChatBatchMethodChatCompletions},
		{name: "chat completions without version", endpoint: "/chat/completions", want: GigaChatBatchMethodChatCompletions},
		{name: "responses", endpoint: schemas.BatchEndpointResponses, want: GigaChatBatchMethodChatCompletions},
		{name: "responses without version", endpoint: "/responses", want: GigaChatBatchMethodChatCompletions},
		{name: "embeddings", endpoint: schemas.BatchEndpointEmbeddings, want: GigaChatBatchMethodEmbedder},
		{name: "embeddings without version", endpoint: "/embeddings", want: GigaChatBatchMethodEmbedder},
		{name: "unknown", endpoint: schemas.BatchEndpointCompletions, wantErr: "do not support endpoint"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := toGigaChatBatchMethod(tt.endpoint)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("toGigaChatBatchMethod() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("toGigaChatBatchMethod() returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("toGigaChatBatchMethod() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToBifrostGigaChatBatchStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status GigaChatBatchStatus
		want   schemas.BatchStatus
	}{
		{name: "created maps to validating", status: GigaChatBatchStatusCreated, want: schemas.BatchStatusValidating},
		{name: "in progress", status: GigaChatBatchStatusInProgress, want: schemas.BatchStatusInProgress},
		{name: "completed", status: GigaChatBatchStatusCompleted, want: schemas.BatchStatusCompleted},
		{name: "unknown preserved", status: GigaChatBatchStatus("queued"), want: schemas.BatchStatus("queued")},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := toBifrostGigaChatBatchStatus(tt.status); got != tt.want {
				t.Fatalf("toBifrostGigaChatBatchStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}

	extra := toBifrostGigaChatBatchProviderExtraFields(GigaChatBatch{
		Method: GigaChatBatchMethodEmbedder,
		Status: GigaChatBatchStatus("queued"),
	})
	if extra["gigachat_batch_status"] != "queued" {
		t.Fatalf("raw unknown status was not preserved: %#v", extra)
	}
	if extra["gigachat_batch_method"] != string(GigaChatBatchMethodEmbedder) {
		t.Fatalf("batch method was not preserved: %#v", extra)
	}
}

func TestConvertGigaChatBatchInputJSONL(t *testing.T) {
	t.Parallel()

	t.Run("ChatCompletions", testConvertGigaChatBatchInputJSONLChatCompletions)
	t.Run("Responses", testConvertGigaChatBatchInputJSONLResponses)
	t.Run("Embeddings", testConvertGigaChatBatchInputJSONLEmbeddings)
	t.Run("InlineRequestItems", testConvertGigaChatBatchRequestItemsToJSONL)
	t.Run("UnsupportedEndpoint", testConvertGigaChatBatchInputJSONLUnsupportedEndpoint)
}

func testConvertGigaChatBatchInputJSONLChatCompletions(t *testing.T) {
	t.Parallel()

	input := []byte(`{"custom_id":"chat-1","method":"POST","url":"/v1/chat/completions","body":{"model":"GigaChat","messages":[{"role":"user","content":"Hello"}],"max_tokens":64,"temperature":0.2}}` + "\n")
	output, err := convertGigaChatBatchInputJSONL(schemas.BatchEndpointChatCompletions, input)
	if err != nil {
		t.Fatalf("convertGigaChatBatchInputJSONL returned error: %v", err)
	}

	row := decodeGigaChatBatchTestRow(t, output)
	if row.ID != "chat-1" {
		t.Fatalf("row id mismatch: got %q", row.ID)
	}
	var request GigaChatChatRequest
	if err := json.Unmarshal(row.Request, &request); err != nil {
		t.Fatalf("unmarshal GigaChat chat request: %v", err)
	}
	if request.Model != "GigaChat" || len(request.Messages) != 1 || request.Messages[0].Role != "user" {
		t.Fatalf("chat request mismatch: %#v", request)
	}
	if request.Messages[0].Content == nil || request.Messages[0].Content.ContentStr == nil || *request.Messages[0].Content.ContentStr != "Hello" {
		t.Fatalf("chat content mismatch: %#v", request.Messages[0].Content)
	}
	if request.MaxTokens == nil || *request.MaxTokens != 64 {
		t.Fatalf("max_tokens mismatch: %#v", request.MaxTokens)
	}
	if request.Temperature == nil || *request.Temperature != 0.2 {
		t.Fatalf("temperature mismatch: %#v", request.Temperature)
	}
	if request.Stream == nil || *request.Stream {
		t.Fatalf("stream mismatch: %#v", request.Stream)
	}
}

func testConvertGigaChatBatchInputJSONLResponses(t *testing.T) {
	t.Parallel()

	input := []byte(`{"custom_id":"resp-1","method":"POST","url":"/v1/responses","body":{"model":"GigaChat-2","input":"Summarize this","instructions":"Be concise.","max_output_tokens":32}}` + "\n")
	output, err := convertGigaChatBatchInputJSONL(schemas.BatchEndpointResponses, input)
	if err != nil {
		t.Fatalf("convertGigaChatBatchInputJSONL returned error: %v", err)
	}

	row := decodeGigaChatBatchTestRow(t, output)
	if row.ID != "resp-1" {
		t.Fatalf("row id mismatch: got %q", row.ID)
	}
	var request GigaChatResponsesRequest
	if err := json.Unmarshal(row.Request, &request); err != nil {
		t.Fatalf("unmarshal GigaChat responses request: %v", err)
	}
	if request.Model != "GigaChat-2" {
		t.Fatalf("model mismatch: got %q", request.Model)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("message count mismatch: got %d", len(request.Messages))
	}
	if request.Messages[0].Role != "system" || request.Messages[0].Content[0].Text == nil || *request.Messages[0].Content[0].Text != "Be concise." {
		t.Fatalf("instruction message mismatch: %#v", request.Messages[0])
	}
	if request.Messages[1].Role != "user" || request.Messages[1].Content[0].Text == nil || *request.Messages[1].Content[0].Text != "Summarize this" {
		t.Fatalf("input message mismatch: %#v", request.Messages[1])
	}
	if request.ModelOptions == nil || request.ModelOptions.MaxTokens == nil || *request.ModelOptions.MaxTokens != 32 {
		t.Fatalf("max output tokens mismatch: %#v", request.ModelOptions)
	}
}

func testConvertGigaChatBatchInputJSONLEmbeddings(t *testing.T) {
	t.Parallel()

	input := []byte(`{"custom_id":"emb-1","method":"POST","url":"/v1/embeddings","body":{"model":"Embeddings","input":["first","second"]}}` + "\n")
	output, err := convertGigaChatBatchInputJSONL(schemas.BatchEndpointEmbeddings, input)
	if err != nil {
		t.Fatalf("convertGigaChatBatchInputJSONL returned error: %v", err)
	}

	row := decodeGigaChatBatchTestRow(t, output)
	if row.ID != "emb-1" {
		t.Fatalf("row id mismatch: got %q", row.ID)
	}
	var request GigaChatEmbeddingRequest
	if err := json.Unmarshal(row.Request, &request); err != nil {
		t.Fatalf("unmarshal GigaChat embedding request: %v", err)
	}
	if request.Model != "Embeddings" {
		t.Fatalf("model mismatch: got %q", request.Model)
	}
	if request.Input == nil || len(request.Input.Texts) != 2 || request.Input.Texts[0] != "first" || request.Input.Texts[1] != "second" {
		t.Fatalf("embedding input mismatch: %#v", request.Input)
	}
}

func testConvertGigaChatBatchRequestItemsToJSONL(t *testing.T) {
	t.Parallel()

	output, err := convertGigaChatBatchRequestItemsToJSONL(schemas.BatchEndpointChatCompletions, []schemas.BatchRequestItem{{
		CustomID: "inline-1",
		Body: map[string]interface{}{
			"model": "GigaChat",
			"messages": []map[string]string{
				{"role": "user", "content": "Hello from inline"},
			},
		},
	}})
	if err != nil {
		t.Fatalf("convertGigaChatBatchRequestItemsToJSONL returned error: %v", err)
	}

	row := decodeGigaChatBatchTestRow(t, output)
	if row.ID != "inline-1" {
		t.Fatalf("row id mismatch: got %q", row.ID)
	}
	var request GigaChatChatRequest
	if err := json.Unmarshal(row.Request, &request); err != nil {
		t.Fatalf("unmarshal GigaChat chat request: %v", err)
	}
	if len(request.Messages) != 1 || request.Messages[0].Content == nil || request.Messages[0].Content.ContentStr == nil || *request.Messages[0].Content.ContentStr != "Hello from inline" {
		t.Fatalf("inline request mismatch: %#v", request)
	}
}

func testConvertGigaChatBatchInputJSONLUnsupportedEndpoint(t *testing.T) {
	t.Parallel()

	input := []byte(`{"custom_id":"bad-1","method":"POST","url":"/v1/completions","body":{"model":"GigaChat","prompt":"Hello"}}` + "\n")
	_, err := convertGigaChatBatchInputJSONL(schemas.BatchEndpointCompletions, input)
	if err == nil {
		t.Fatal("expected unsupported endpoint error, got nil")
	}
	if !strings.Contains(err.Error(), "line 1") || !strings.Contains(err.Error(), "do not support endpoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func decodeGigaChatBatchTestRow(t *testing.T, output []byte) GigaChatBatchInputRow {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d JSONL lines, want 1: %q", len(lines), string(output))
	}
	var row GigaChatBatchInputRow
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("unmarshal GigaChat batch row: %v", err)
	}
	if len(row.Request) == 0 {
		t.Fatalf("row request is empty: %#v", row)
	}
	return row
}
