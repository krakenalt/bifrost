package gigachat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestGigaChatResponsesRequestConversion(t *testing.T) {
	testGigaChatResponsesRequestConversion(t)
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

func testGigaChatResponsesRequest() *schemas.BifrostResponsesRequest {
	return &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2",
		Input: []schemas.ResponsesMessage{{
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("hello")},
		}},
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
