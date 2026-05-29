package gigachat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestGigaChatResponsesRequestConversion(t *testing.T) {
	testGigaChatResponsesRequestConversion(t)
}

func TestGigaChatResponses(t *testing.T) {
	testGigaChatResponses(t)
}

func testGigaChatResponsesRequestConversion(t *testing.T) {
	t.Parallel()

	t.Run("SimpleTextInput", testGigaChatResponsesSimpleTextInput)
	t.Run("InstructionsAndMultiTurnInput", testGigaChatResponsesInstructionsAndMultiTurnInput)
	t.Run("FunctionToolAndToolHistory", testGigaChatResponsesFunctionToolAndToolHistory)
	t.Run("StructuredOutput", testGigaChatResponsesStructuredOutput)
	t.Run("RejectsUnsupportedBuiltInTools", testGigaChatResponsesRejectsUnsupportedBuiltInTools)
	t.Run("RejectsUnsupportedParams", testGigaChatResponsesRejectsUnsupportedParams)
}

func testGigaChatResponses(t *testing.T) {
	t.Parallel()

	t.Run("ConverterMapsTextAndUsage", testGigaChatResponsesConverterMapsTextAndUsage)
	t.Run("ConverterMapsToolCall", testGigaChatResponsesConverterMapsToolCall)
	t.Run("ExecutesWithOAuthToken", testGigaChatResponsesExecutesWithOAuthToken)
	t.Run("MapsProviderErrors", testGigaChatResponsesMapsProviderErrors)
	t.Run("RefreshesTokenAfterUnauthorized", testGigaChatResponsesRefreshesTokenAfterUnauthorized)
}

func testGigaChatResponsesSimpleTextInput(t *testing.T) {
	t.Parallel()

	temperature := 0.2
	topP := 0.8
	maxOutputTokens := 256
	topLogProbs := 3
	request := &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2",
		Input: []schemas.ResponsesMessage{{
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("hello")},
		}},
		Params: &schemas.ResponsesParameters{
			Temperature:     &temperature,
			TopP:            &topP,
			MaxOutputTokens: &maxOutputTokens,
			TopLogProbs:     &topLogProbs,
		},
	}

	gigaChatReq, err := ToGigaChatResponsesRequest(request)
	if err != nil {
		t.Fatalf("ToGigaChatResponsesRequest returned error: %v", err)
	}
	if gigaChatReq.Model != "GigaChat-2" {
		t.Fatalf("model mismatch: got %q", gigaChatReq.Model)
	}
	if len(gigaChatReq.Messages) != 1 {
		t.Fatalf("message count mismatch: got %d", len(gigaChatReq.Messages))
	}
	if got := gigaChatReq.Messages[0].Role; got != "user" {
		t.Fatalf("role mismatch: got %q", got)
	}
	if got := *gigaChatReq.Messages[0].Content[0].Text; got != "hello" {
		t.Fatalf("content mismatch: got %q", got)
	}
	if gigaChatReq.ModelOptions == nil ||
		gigaChatReq.ModelOptions.Temperature == nil || *gigaChatReq.ModelOptions.Temperature != temperature ||
		gigaChatReq.ModelOptions.TopP == nil || *gigaChatReq.ModelOptions.TopP != topP ||
		gigaChatReq.ModelOptions.MaxTokens == nil || *gigaChatReq.ModelOptions.MaxTokens != maxOutputTokens ||
		gigaChatReq.ModelOptions.TopLogProbs == nil || *gigaChatReq.ModelOptions.TopLogProbs != topLogProbs {
		t.Fatalf("model options mismatch: %#v", gigaChatReq.ModelOptions)
	}

	body, err := json.Marshal(gigaChatReq)
	if err != nil {
		t.Fatalf("failed to marshal GigaChat request: %v", err)
	}
	if strings.Contains(string(body), `"stream"`) {
		t.Fatalf("non-streaming v2 request should omit stream, got %s", body)
	}
}

func testGigaChatResponsesInstructionsAndMultiTurnInput(t *testing.T) {
	t.Parallel()

	instructions := "Answer in Russian."
	request := &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2-Pro",
		Input: []schemas.ResponsesMessage{
			{
				Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleSystem),
				Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("Be concise.")},
			},
			{
				Role: schemas.Ptr(schemas.ResponsesInputMessageRoleAssistant),
				Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{{
					Type: schemas.ResponsesOutputMessageContentTypeText,
					Text: schemas.Ptr("Previous answer."),
				}}},
			},
			{
				Role: schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
				Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{{
					Type: schemas.ResponsesInputMessageContentBlockTypeText,
					Text: schemas.Ptr("Continue."),
				}}},
			},
		},
		Params: &schemas.ResponsesParameters{Instructions: &instructions},
	}

	gigaChatReq, err := ToGigaChatResponsesRequest(request)
	if err != nil {
		t.Fatalf("ToGigaChatResponsesRequest returned error: %v", err)
	}
	if len(gigaChatReq.Messages) != 4 {
		t.Fatalf("message count mismatch: got %d", len(gigaChatReq.Messages))
	}
	wantRoles := []string{"system", "system", "assistant", "user"}
	wantText := []string{"Answer in Russian.", "Be concise.", "Previous answer.", "Continue."}
	for index := range wantRoles {
		if got := gigaChatReq.Messages[index].Role; got != wantRoles[index] {
			t.Fatalf("message %d role mismatch: got %q, want %q", index, got, wantRoles[index])
		}
		if got := *gigaChatReq.Messages[index].Content[0].Text; got != wantText[index] {
			t.Fatalf("message %d text mismatch: got %q, want %q", index, got, wantText[index])
		}
	}
}

func testGigaChatResponsesFunctionToolAndToolHistory(t *testing.T) {
	t.Parallel()

	toolName := "get_weather"
	arguments := `{"city":"Moscow"}`
	toolOutput := `{"temperature":5}`
	request := &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2-Max",
		Input: []schemas.ResponsesMessage{
			{
				Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
				Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("Weather?")},
			},
			{
				Type: schemas.Ptr(schemas.ResponsesMessageTypeFunctionCall),
				ResponsesToolMessage: &schemas.ResponsesToolMessage{
					Name:      &toolName,
					Arguments: &arguments,
				},
			},
			{
				Type: schemas.Ptr(schemas.ResponsesMessageTypeFunctionCallOutput),
				ResponsesToolMessage: &schemas.ResponsesToolMessage{
					Name: &toolName,
					Output: &schemas.ResponsesToolMessageOutputStruct{
						ResponsesToolCallOutputStr: &toolOutput,
					},
				},
			},
		},
		Params: &schemas.ResponsesParameters{
			Tools: []schemas.ResponsesTool{{
				Type:        schemas.ResponsesToolTypeFunction,
				Name:        &toolName,
				Description: schemas.Ptr("Gets current weather."),
				ResponsesToolFunction: &schemas.ResponsesToolFunction{
					Parameters: mustGigaChatToolParameters(t, `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
				},
			}},
			ToolChoice: &schemas.ResponsesToolChoice{
				ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
					Type: schemas.ResponsesToolChoiceTypeFunction,
					Name: &toolName,
				},
			},
		},
	}

	gigaChatReq, err := ToGigaChatResponsesRequest(request)
	if err != nil {
		t.Fatalf("ToGigaChatResponsesRequest returned error: %v", err)
	}
	if len(gigaChatReq.Tools) != 1 || gigaChatReq.Tools[0].Functions == nil || len(gigaChatReq.Tools[0].Functions.Specifications) != 1 {
		t.Fatalf("function tools mismatch: %#v", gigaChatReq.Tools)
	}
	specification := gigaChatReq.Tools[0].Functions.Specifications[0]
	if specification.Name != toolName || specification.Description == nil || *specification.Description != "Gets current weather." {
		t.Fatalf("function specification mismatch: %#v", specification)
	}
	if gigaChatReq.ToolConfig == nil || gigaChatReq.ToolConfig.Mode != "forced" || gigaChatReq.ToolConfig.FunctionName == nil || *gigaChatReq.ToolConfig.FunctionName != toolName {
		t.Fatalf("tool config mismatch: %#v", gigaChatReq.ToolConfig)
	}
	if gigaChatReq.Messages[1].FunctionCall == nil {
		t.Fatalf("expected function call message, got %#v", gigaChatReq.Messages[1])
	}
	argumentsMap, ok := gigaChatReq.Messages[1].FunctionCall.Arguments.(map[string]interface{})
	if !ok || argumentsMap["city"] != "Moscow" {
		t.Fatalf("function arguments mismatch: %#v", gigaChatReq.Messages[1].FunctionCall.Arguments)
	}
	if gigaChatReq.Messages[2].Content[0].FunctionResult == nil || gigaChatReq.Messages[2].Content[0].FunctionResult.Result != toolOutput {
		t.Fatalf("function result mismatch: %#v", gigaChatReq.Messages[2].Content)
	}
}

func testGigaChatResponsesStructuredOutput(t *testing.T) {
	t.Parallel()

	strict := true
	formatName := "WeatherAnswer"
	formatDescription := "Weather response."
	request := &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2-Pro",
		Input: []schemas.ResponsesMessage{{
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("Return JSON.")},
		}},
		Params: &schemas.ResponsesParameters{
			Text: &schemas.ResponsesTextConfig{
				Format: &schemas.ResponsesTextConfigFormat{
					Type:        "json_schema",
					Name:        &formatName,
					Description: &formatDescription,
					Strict:      &strict,
					JSONSchema: schemas.JSONSchemaFromMap(map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"answer": map[string]interface{}{"type": "string"},
						},
						"required": []interface{}{"answer"},
					}),
				},
			},
		},
	}

	gigaChatReq, err := ToGigaChatResponsesRequest(request)
	if err != nil {
		t.Fatalf("ToGigaChatResponsesRequest returned error: %v", err)
	}
	responseFormat := gigaChatReq.ModelOptions.ResponseFormat
	if responseFormat == nil || responseFormat.Type != "json_schema" || responseFormat.Strict == nil || !*responseFormat.Strict {
		t.Fatalf("response format mismatch: %#v", responseFormat)
	}
	schemaMap, ok := responseFormat.Schema.(map[string]interface{})
	if !ok {
		t.Fatalf("response schema has unexpected type: %#v", responseFormat.Schema)
	}
	if schemaMap["title"] != formatName || schemaMap["description"] != formatDescription {
		t.Fatalf("schema metadata mismatch: %#v", schemaMap)
	}
	if schemaMap["type"] != "object" {
		t.Fatalf("schema type mismatch: %#v", schemaMap)
	}
}

func testGigaChatResponsesRejectsUnsupportedBuiltInTools(t *testing.T) {
	t.Parallel()

	request := testGigaChatResponsesRequest()
	request.Params = &schemas.ResponsesParameters{
		Tools: []schemas.ResponsesTool{{
			Type:                   schemas.ResponsesToolTypeWebSearch,
			ResponsesToolWebSearch: &schemas.ResponsesToolWebSearch{},
		}},
	}

	_, err := ToGigaChatResponsesRequest(request)
	if err == nil {
		t.Fatal("expected unsupported built-in tool error, got nil")
	}
	if !strings.Contains(err.Error(), "function tools only") || !strings.Contains(err.Error(), "web_search") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testGigaChatResponsesRejectsUnsupportedParams(t *testing.T) {
	t.Parallel()

	parallelToolCalls := true
	request := testGigaChatResponsesRequest()
	request.Params = &schemas.ResponsesParameters{ParallelToolCalls: &parallelToolCalls}
	_, err := ToGigaChatResponsesRequest(request)
	if err == nil || !strings.Contains(err.Error(), "parallel_tool_calls") {
		t.Fatalf("expected parallel_tool_calls error, got %v", err)
	}

	request = testGigaChatResponsesRequest()
	request.Params = &schemas.ResponsesParameters{
		Text: &schemas.ResponsesTextConfig{
			Format: &schemas.ResponsesTextConfigFormat{Type: "json_object"},
		},
	}
	_, err = ToGigaChatResponsesRequest(request)
	if err == nil || !strings.Contains(err.Error(), "json_object") {
		t.Fatalf("expected json_object error, got %v", err)
	}

	request = &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2",
		Input: []schemas.ResponsesMessage{{
			Role: schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{{
				Type: schemas.ResponsesInputMessageContentBlockTypeImage,
				ResponsesInputMessageContentBlockImage: &schemas.ResponsesInputMessageContentBlockImage{
					ImageURL: schemas.Ptr("https://example.test/image.png"),
				},
			}}},
		}},
	}
	_, err = ToGigaChatResponsesRequest(request)
	if err == nil || !strings.Contains(err.Error(), "input_image") {
		t.Fatalf("expected input_image error, got %v", err)
	}
}

func testGigaChatResponsesConverterMapsTextAndUsage(t *testing.T) {
	t.Parallel()

	response := &GigaChatResponsesResponse{
		ID:      "resp-test",
		Created: 1700000000,
		Model:   "GigaChat-2",
		Choices: []GigaChatResponsesChoice{{
			Index: 0,
			Message: &GigaChatResponsesMessage{
				Role:      "assistant",
				MessageID: schemas.Ptr("msg-test"),
				Content: []GigaChatResponsesContentPart{{
					Text: schemas.Ptr("Здравствуйте"),
				}},
			},
			FinishReason: schemas.Ptr("stop"),
		}},
		Usage: &GigaChatChatUsage{
			PromptTokens:          7,
			CompletionTokens:      3,
			TotalTokens:           10,
			PrecachedPromptTokens: 2,
		},
	}

	converted := ToBifrostResponsesResponse(schemas.GigaChat, response)
	if converted == nil {
		t.Fatal("expected response, got nil")
	}
	if converted.ID == nil || *converted.ID != "resp-test" {
		t.Fatalf("id mismatch: %#v", converted.ID)
	}
	if converted.Object != "response" || converted.CreatedAt != 1700000000 || converted.Model != "GigaChat-2" {
		t.Fatalf("metadata mismatch: %#v", converted)
	}
	if converted.Status == nil || *converted.Status != "completed" {
		t.Fatalf("status mismatch: %#v", converted.Status)
	}
	if converted.StopReason == nil || *converted.StopReason != "stop" {
		t.Fatalf("stop reason mismatch: %#v", converted.StopReason)
	}
	if converted.Usage == nil || converted.Usage.InputTokens != 7 || converted.Usage.OutputTokens != 3 || converted.Usage.TotalTokens != 10 {
		t.Fatalf("usage mismatch: %#v", converted.Usage)
	}
	if converted.Usage.InputTokensDetails == nil || converted.Usage.InputTokensDetails.CachedReadTokens != 2 {
		t.Fatalf("cached tokens mismatch: %#v", converted.Usage.InputTokensDetails)
	}
	if len(converted.Output) != 1 {
		t.Fatalf("output count mismatch: got %d", len(converted.Output))
	}
	output := converted.Output[0]
	if output.Type == nil || *output.Type != schemas.ResponsesMessageTypeMessage {
		t.Fatalf("output type mismatch: %#v", output.Type)
	}
	if output.Role == nil || *output.Role != schemas.ResponsesInputMessageRoleAssistant {
		t.Fatalf("output role mismatch: %#v", output.Role)
	}
	if output.Content == nil || len(output.Content.ContentBlocks) != 1 {
		t.Fatalf("content mismatch: %#v", output.Content)
	}
	block := output.Content.ContentBlocks[0]
	if block.Type != schemas.ResponsesOutputMessageContentTypeText || block.Text == nil || *block.Text != "Здравствуйте" {
		t.Fatalf("text block mismatch: %#v", block)
	}
	if converted.ExtraFields.Provider != schemas.GigaChat {
		t.Fatalf("provider mismatch: got %q, want %q", converted.ExtraFields.Provider, schemas.GigaChat)
	}
}

func testGigaChatResponsesConverterMapsToolCall(t *testing.T) {
	t.Parallel()

	response := &GigaChatResponsesResponse{
		Model: "GigaChat-2-Max",
		Choices: []GigaChatResponsesChoice{{
			Index: 0,
			Message: &GigaChatResponsesMessage{
				Role:      "assistant",
				MessageID: schemas.Ptr("call-message"),
				Content: []GigaChatResponsesContentPart{{
					FunctionCall: &GigaChatResponsesFunctionCall{
						Name: "get_weather",
						Arguments: map[string]interface{}{
							"city": "Moscow",
						},
					},
				}},
			},
			FinishReason: schemas.Ptr("function_call"),
		}},
	}

	converted := ToBifrostResponsesResponse(schemas.GigaChat, response)
	if converted == nil {
		t.Fatal("expected response, got nil")
	}
	if converted.Status == nil || *converted.Status != "completed" {
		t.Fatalf("status mismatch: %#v", converted.Status)
	}
	if converted.StopReason == nil || *converted.StopReason != "tool_calls" {
		t.Fatalf("stop reason mismatch: %#v", converted.StopReason)
	}
	if len(converted.Output) != 1 {
		t.Fatalf("output count mismatch: got %d", len(converted.Output))
	}
	output := converted.Output[0]
	if output.Type == nil || *output.Type != schemas.ResponsesMessageTypeFunctionCall {
		t.Fatalf("output type mismatch: %#v", output.Type)
	}
	if output.ResponsesToolMessage == nil || output.ResponsesToolMessage.Name == nil || *output.ResponsesToolMessage.Name != "get_weather" {
		t.Fatalf("tool message mismatch: %#v", output.ResponsesToolMessage)
	}
	if output.ResponsesToolMessage.Arguments == nil || *output.ResponsesToolMessage.Arguments != `{"city":"Moscow"}` {
		t.Fatalf("arguments mismatch: %#v", output.ResponsesToolMessage.Arguments)
	}
	if output.ResponsesToolMessage.CallID == nil || *output.ResponsesToolMessage.CallID != "call-message" {
		t.Fatalf("call id mismatch: %#v", output.ResponsesToolMessage.CallID)
	}
}

func testGigaChatResponsesExecutesWithOAuthToken(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	var responsesRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth":
			tokenRequests.Add(1)
			if got := request.Header.Get("Authorization"); got != "Basic super-secret-credentials" {
				t.Fatalf("token authorization header mismatch: got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"responses-access-token","expires_at":1893456000}`))
		case "/v2/chat/completions":
			responsesRequests.Add(1)
			if got := request.Header.Get("Authorization"); got != "Bearer responses-access-token" {
				t.Fatalf("responses authorization header mismatch: got %q", got)
			}
			if strings.Contains(request.Header.Get("Authorization"), "super-secret-credentials") {
				t.Fatal("responses request leaked OAuth credentials")
			}
			assertGigaChatResponsesRequestBody(t, request)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-ID", "responses-request-id")
			_, _ = w.Write([]byte(`{
				"id":"resp-test",
				"choices":[{"index":0,"message":{"role":"assistant","message_id":"msg-test","content":[{"text":"Здравствуйте"}]},"finish_reason":"stop"}],
				"created":1700000000,
				"model":"GigaChat-2",
				"object":"chat.completion",
				"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"precached_prompt_tokens":2}
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	provider.sendBackRawRequest = true
	provider.sendBackRawResponse = true
	response, bifrostErr := provider.Responses(testBifrostContext(), testGigaChatOAuthKey(server.URL+"/oauth", "", "super-secret-credentials"), testGigaChatResponsesExecutionRequest())
	if bifrostErr != nil {
		t.Fatalf("Responses returned error: %v", bifrostErr)
	}
	if tokenRequests.Load() != 1 {
		t.Fatalf("token request count mismatch: got %d, want 1", tokenRequests.Load())
	}
	if responsesRequests.Load() != 1 {
		t.Fatalf("responses request count mismatch: got %d, want 1", responsesRequests.Load())
	}
	if response.ID == nil || *response.ID != "resp-test" {
		t.Fatalf("id mismatch: %#v", response.ID)
	}
	if response.ExtraFields.Provider != schemas.GigaChat {
		t.Fatalf("provider mismatch: got %q, want %q", response.ExtraFields.Provider, schemas.GigaChat)
	}
	if response.Usage == nil || response.Usage.TotalTokens != 10 {
		t.Fatalf("usage mismatch: %#v", response.Usage)
	}
	if len(response.Output) != 1 || response.Output[0].Content == nil || len(response.Output[0].Content.ContentBlocks) != 1 {
		t.Fatalf("output mismatch: %#v", response.Output)
	}
	if got := *response.Output[0].Content.ContentBlocks[0].Text; got != "Здравствуйте" {
		t.Fatalf("content mismatch: got %q", got)
	}
	if response.ExtraFields.RawRequest == nil || response.ExtraFields.RawResponse == nil {
		t.Fatalf("expected raw request and response, got request=%#v response=%#v", response.ExtraFields.RawRequest, response.ExtraFields.RawResponse)
	}
	if got := response.ExtraFields.ProviderResponseHeaders["X-Request-Id"]; got != "responses-request-id" {
		t.Fatalf("provider response header mismatch: %#v", response.ExtraFields.ProviderResponseHeaders)
	}
}

func testGigaChatResponsesMapsProviderErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":400,"code":123,"message":"bad responses request"}`))
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	response, bifrostErr := provider.Responses(testBifrostContext(), testGigaChatAccessTokenKey("provider-error-token"), testGigaChatResponsesExecutionRequest())
	if response != nil {
		t.Fatalf("expected nil response, got %#v", response)
	}
	if bifrostErr == nil {
		t.Fatal("expected provider error, got nil")
	}
	if bifrostErr.StatusCode == nil || *bifrostErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status mismatch: %#v", bifrostErr.StatusCode)
	}
	if bifrostErr.Error == nil || bifrostErr.Error.Message != "bad responses request" {
		t.Fatalf("message mismatch: %#v", bifrostErr.Error)
	}
	if bifrostErr.Error.Code == nil || *bifrostErr.Error.Code != "123" {
		t.Fatalf("code mismatch: %#v", bifrostErr.Error)
	}
	assertNoGigaChatSecretLeak(t, bifrostErr.String())
}

func testGigaChatResponsesRefreshesTokenAfterUnauthorized(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	var responsesRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth":
			tokenIndex := tokenRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"access_token":"responses-token-%d","expires_at":1893456000}`, tokenIndex)))
		case "/v2/chat/completions":
			responsesIndex := responsesRequests.Add(1)
			wantAuthorization := fmt.Sprintf("Bearer responses-token-%d", responsesIndex)
			if got := request.Header.Get("Authorization"); got != wantAuthorization {
				t.Fatalf("authorization header mismatch on request %d: got %q, want %q", responsesIndex, got, wantAuthorization)
			}
			w.Header().Set("Content-Type", "application/json")
			if responsesIndex == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"status":401,"message":"expired token"}`))
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":[{"text":"ok"}]},"finish_reason":"stop"}],"model":"GigaChat-2","object":"chat.completion"}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	response, bifrostErr := provider.Responses(testBifrostContext(), testGigaChatOAuthKey(server.URL+"/oauth", "", "test-credentials"), testGigaChatResponsesExecutionRequest())
	if bifrostErr != nil {
		t.Fatalf("Responses returned error: %v", bifrostErr)
	}
	if response == nil || len(response.Output) != 1 {
		t.Fatalf("unexpected response: %#v", response)
	}
	if tokenRequests.Load() != 2 {
		t.Fatalf("token request count mismatch: got %d, want 2", tokenRequests.Load())
	}
	if responsesRequests.Load() != 2 {
		t.Fatalf("responses request count mismatch: got %d, want 2", responsesRequests.Load())
	}
}

func testGigaChatResponsesRequest() *schemas.BifrostResponsesRequest {
	return &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2",
		Input: []schemas.ResponsesMessage{{
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("hello")},
		}},
	}
}

func testGigaChatResponsesExecutionRequest() *schemas.BifrostResponsesRequest {
	maxTokens := 128
	return &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2",
		Input: []schemas.ResponsesMessage{{
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("Привет")},
		}},
		Params: &schemas.ResponsesParameters{
			MaxOutputTokens: &maxTokens,
		},
	}
}

func assertGigaChatResponsesRequestBody(t *testing.T, request *http.Request) {
	t.Helper()

	if request.Method != http.MethodPost {
		t.Fatalf("method mismatch: got %s, want POST", request.Method)
	}
	if got := request.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type mismatch: got %q", got)
	}
	if got := request.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("accept header mismatch: got %q", got)
	}
	if got := request.Header.Get(gigaChatUserAgentHeader); got != gigaChatUserAgent {
		t.Fatalf("user-agent mismatch: got %q, want %q", got, gigaChatUserAgent)
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("failed to unmarshal request body %s: %v", body, err)
	}
	if got := payload["model"]; got != "GigaChat-2" {
		t.Fatalf("model mismatch: got %#v", got)
	}
	if _, ok := payload["stream"]; ok {
		t.Fatalf("non-streaming responses request should omit stream: %s", body)
	}
	modelOptions, ok := payload["model_options"].(map[string]interface{})
	if !ok {
		t.Fatalf("model_options mismatch: %#v", payload["model_options"])
	}
	if got := modelOptions["max_tokens"]; got != float64(128) {
		t.Fatalf("max_tokens mismatch: got %#v", got)
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Fatalf("messages mismatch: %#v", payload["messages"])
	}
	message, ok := messages[0].(map[string]interface{})
	if !ok {
		t.Fatalf("message shape mismatch: %#v", messages[0])
	}
	if got := message["role"]; got != "user" {
		t.Fatalf("message role mismatch: got %#v", got)
	}
	content, ok := message["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("message content mismatch: %#v", message["content"])
	}
	contentPart, ok := content[0].(map[string]interface{})
	if !ok || contentPart["text"] != "Привет" {
		t.Fatalf("content part mismatch: %#v", content[0])
	}
}

func mustGigaChatToolParameters(t *testing.T, raw string) *schemas.ToolFunctionParameters {
	t.Helper()

	var parameters schemas.ToolFunctionParameters
	if err := json.Unmarshal([]byte(raw), &parameters); err != nil {
		t.Fatalf("failed to unmarshal tool parameters: %v", err)
	}
	return &parameters
}
