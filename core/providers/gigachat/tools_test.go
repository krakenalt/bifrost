package gigachat

import (
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestGigaChatTools(t *testing.T) {
	testGigaChatTools(t)
}

func testGigaChatTools(t *testing.T) {
	t.Parallel()

	t.Run("ChatMapsFunctionToolsAndHistory", testGigaChatToolsChatMapsFunctionToolsAndHistory)
	t.Run("ChatToolChoiceVariants", testGigaChatToolsChatToolChoiceVariants)
	t.Run("ChatRejectsUnsupportedPolicy", testGigaChatToolsChatRejectsUnsupportedPolicy)
	t.Run("ResponsesToolChoiceVariants", testGigaChatToolsResponsesToolChoiceVariants)
	t.Run("ResponsesRejectsUnsupportedPolicy", testGigaChatToolsResponsesRejectsUnsupportedPolicy)
}

func testGigaChatToolsChatMapsFunctionToolsAndHistory(t *testing.T) {
	t.Parallel()

	toolName := "get_weather"
	toolCallID := "state-weather"
	toolCallType := string(schemas.ChatToolTypeFunction)
	toolArguments := `{"city":"Moscow"}`
	result := `{"temperature":5}`
	request := &schemas.BifrostChatRequest{
		Model: "GigaChat",
		Input: []schemas.ChatMessage{
			{
				Role:    schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("Weather?")},
			},
			{
				Role:    schemas.ChatMessageRoleAssistant,
				Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("")},
				ChatAssistantMessage: &schemas.ChatAssistantMessage{
					ToolCalls: []schemas.ChatAssistantMessageToolCall{{
						Type: &toolCallType,
						ID:   &toolCallID,
						Function: schemas.ChatAssistantMessageToolCallFunction{
							Name:      &toolName,
							Arguments: toolArguments,
						},
					}},
				},
			},
			{
				Role:            schemas.ChatMessageRoleTool,
				Content:         &schemas.ChatMessageContent{ContentStr: &result},
				ChatToolMessage: &schemas.ChatToolMessage{ToolCallID: &toolCallID},
			},
			{
				Role:    schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("What should I wear?")},
			},
		},
		Params: &schemas.ChatParameters{
			Tools: []schemas.ChatTool{testGigaChatChatFunctionTool(t, toolName)},
			ToolChoice: &schemas.ChatToolChoice{
				ChatToolChoiceStruct: &schemas.ChatToolChoiceStruct{
					Type:     schemas.ChatToolChoiceTypeFunction,
					Function: &schemas.ChatToolChoiceFunction{Name: toolName},
				},
			},
		},
	}

	gigaChatReq, err := ToGigaChatChatRequest(testBifrostContext(), request)
	if err != nil {
		t.Fatalf("ToGigaChatChatRequest returned error: %v", err)
	}
	if len(gigaChatReq.Functions) != 1 || gigaChatReq.Functions[0].Name != toolName {
		t.Fatalf("functions mismatch: %#v", gigaChatReq.Functions)
	}
	choice, ok := gigaChatReq.FunctionCall.(GigaChatFunctionCallChoice)
	if !ok || choice.Name != toolName {
		t.Fatalf("function_call mismatch: %#v", gigaChatReq.FunctionCall)
	}
	if len(gigaChatReq.Messages) != 4 {
		t.Fatalf("message count mismatch: got %d", len(gigaChatReq.Messages))
	}
	assistant := gigaChatReq.Messages[1]
	if assistant.FunctionCall == nil || assistant.FunctionCall.Name != toolName || string(assistant.FunctionCall.Arguments) != toolArguments {
		t.Fatalf("assistant function_call mismatch: %#v", assistant)
	}
	if assistant.FunctionsStateID == nil || *assistant.FunctionsStateID != toolCallID {
		t.Fatalf("functions_state_id mismatch: %#v", assistant.FunctionsStateID)
	}
	functionResult := gigaChatReq.Messages[2]
	if functionResult.Role != "function" || functionResult.Name == nil || *functionResult.Name != toolName {
		t.Fatalf("function result message mismatch: %#v", functionResult)
	}
	if functionResult.Content == nil || functionResult.Content.ContentStr == nil || *functionResult.Content.ContentStr != result {
		t.Fatalf("function result content mismatch: %#v", functionResult.Content)
	}
}

func testGigaChatToolsChatToolChoiceVariants(t *testing.T) {
	t.Parallel()

	toolName := "get_weather"
	tests := []struct {
		name       string
		choice     *schemas.ChatToolChoice
		wantMode   string
		wantForced string
	}{
		{
			name:     "StringAuto",
			choice:   &schemas.ChatToolChoice{ChatToolChoiceStr: schemas.Ptr("auto")},
			wantMode: "auto",
		},
		{
			name:     "StringNone",
			choice:   &schemas.ChatToolChoice{ChatToolChoiceStr: schemas.Ptr("none")},
			wantMode: "none",
		},
		{
			name: "StructAuto",
			choice: &schemas.ChatToolChoice{ChatToolChoiceStruct: &schemas.ChatToolChoiceStruct{
				Type: schemas.ChatToolChoiceTypeAuto,
			}},
			wantMode: "auto",
		},
		{
			name: "StructNone",
			choice: &schemas.ChatToolChoice{ChatToolChoiceStruct: &schemas.ChatToolChoiceStruct{
				Type: schemas.ChatToolChoiceTypeNone,
			}},
			wantMode: "none",
		},
		{
			name: "StructFunction",
			choice: &schemas.ChatToolChoice{ChatToolChoiceStruct: &schemas.ChatToolChoiceStruct{
				Type:     schemas.ChatToolChoiceTypeFunction,
				Function: &schemas.ChatToolChoiceFunction{Name: toolName},
			}},
			wantForced: toolName,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := testGigaChatChatToolRequest(t, toolName)
			request.Params.ToolChoice = test.choice
			gigaChatReq, err := ToGigaChatChatRequest(testBifrostContext(), request)
			if err != nil {
				t.Fatalf("ToGigaChatChatRequest returned error: %v", err)
			}
			if test.wantForced != "" {
				choice, ok := gigaChatReq.FunctionCall.(GigaChatFunctionCallChoice)
				if !ok || choice.Name != test.wantForced {
					t.Fatalf("forced function_call mismatch: %#v", gigaChatReq.FunctionCall)
				}
				return
			}
			mode, ok := gigaChatReq.FunctionCall.(string)
			if !ok || mode != test.wantMode {
				t.Fatalf("function_call mode mismatch: got %#v, want %q", gigaChatReq.FunctionCall, test.wantMode)
			}
		})
	}
}

func testGigaChatToolsChatRejectsUnsupportedPolicy(t *testing.T) {
	t.Parallel()

	parallelToolCalls := true
	strict := true
	tests := []struct {
		name    string
		mutate  func(*schemas.BifrostChatRequest)
		wantErr string
	}{
		{
			name: "InvalidJSONSchema",
			mutate: func(request *schemas.BifrostChatRequest) {
				request.Params.Tools[0].Function.Parameters = invalidGigaChatToolParameters()
			},
			wantErr: "JSON schema is invalid",
		},
		{
			name: "GigaChatBuiltInName",
			mutate: func(request *schemas.BifrostChatRequest) {
				request.Params.Tools[0].Function.Name = "text2image"
			},
			wantErr: "built-in function",
		},
		{
			name: "OpenAIStrictMode",
			mutate: func(request *schemas.BifrostChatRequest) {
				request.Params.Tools[0].Function.Strict = &strict
			},
			wantErr: "strict mode",
		},
		{
			name: "CustomTool",
			mutate: func(request *schemas.BifrostChatRequest) {
				request.Params.Tools = []schemas.ChatTool{{
					Type:   schemas.ChatToolTypeCustom,
					Name:   "custom_tool",
					Custom: &schemas.ChatToolCustom{},
				}}
			},
			wantErr: "function tools only",
		},
		{
			name: "ParallelToolCalls",
			mutate: func(request *schemas.BifrostChatRequest) {
				request.Params.ParallelToolCalls = &parallelToolCalls
			},
			wantErr: "parallel_tool_calls",
		},
		{
			name: "RequiredToolChoice",
			mutate: func(request *schemas.BifrostChatRequest) {
				request.Params.ToolChoice = &schemas.ChatToolChoice{ChatToolChoiceStr: schemas.Ptr("required")}
			},
			wantErr: "tool_choice",
		},
		{
			name: "AutoToolChoiceWithoutFunctions",
			mutate: func(request *schemas.BifrostChatRequest) {
				request.Params.Tools = nil
				request.Params.ToolChoice = &schemas.ChatToolChoice{ChatToolChoiceStr: schemas.Ptr("auto")}
			},
			wantErr: "requires at least one",
		},
		{
			name: "ExtraParamFunctionsBypass",
			mutate: func(request *schemas.BifrostChatRequest) {
				request.Params.ExtraParams = map[string]interface{}{"functions": []interface{}{}}
			},
			wantErr: "extra_params.functions",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := testGigaChatChatToolRequest(t, "get_weather")
			test.mutate(request)
			_, err := ToGigaChatChatRequest(testBifrostContext(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
			}
		})
	}
}

func testGigaChatToolsResponsesToolChoiceVariants(t *testing.T) {
	t.Parallel()

	toolName := "get_weather"
	tests := []struct {
		name       string
		choice     *schemas.ResponsesToolChoice
		wantMode   string
		wantForced string
	}{
		{
			name:     "StringAuto",
			choice:   &schemas.ResponsesToolChoice{ResponsesToolChoiceStr: schemas.Ptr("auto")},
			wantMode: "auto",
		},
		{
			name:     "StringNone",
			choice:   &schemas.ResponsesToolChoice{ResponsesToolChoiceStr: schemas.Ptr("none")},
			wantMode: "none",
		},
		{
			name: "StructAuto",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceTypeAuto,
			}},
			wantMode: "auto",
		},
		{
			name: "StructNone",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceTypeNone,
			}},
			wantMode: "none",
		},
		{
			name: "StructFunction",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceTypeFunction,
				Name: &toolName,
			}},
			wantForced: toolName,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := testGigaChatResponsesToolRequest(t, toolName)
			request.Params.ToolChoice = test.choice
			gigaChatReq, err := ToGigaChatResponsesRequest(request)
			if err != nil {
				t.Fatalf("ToGigaChatResponsesRequest returned error: %v", err)
			}
			if test.wantForced != "" {
				if gigaChatReq.ToolConfig == nil || gigaChatReq.ToolConfig.FunctionName == nil || *gigaChatReq.ToolConfig.FunctionName != test.wantForced || gigaChatReq.ToolConfig.Mode != "forced" {
					t.Fatalf("forced tool_config mismatch: %#v", gigaChatReq.ToolConfig)
				}
				return
			}
			if gigaChatReq.ToolConfig == nil || gigaChatReq.ToolConfig.Mode != test.wantMode {
				t.Fatalf("tool_config mode mismatch: got %#v, want %q", gigaChatReq.ToolConfig, test.wantMode)
			}
		})
	}
}

func testGigaChatToolsResponsesRejectsUnsupportedPolicy(t *testing.T) {
	t.Parallel()

	parallelToolCalls := true
	strict := true
	tests := []struct {
		name    string
		mutate  func(*schemas.BifrostResponsesRequest)
		wantErr string
	}{
		{
			name: "InvalidJSONSchema",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools[0].ResponsesToolFunction.Parameters = invalidGigaChatToolParameters()
			},
			wantErr: "JSON schema is invalid",
		},
		{
			name: "GigaChatBuiltInName",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools[0].Name = schemas.Ptr("text2image")
			},
			wantErr: "built-in function",
		},
		{
			name: "OpenAIStrictMode",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools[0].ResponsesToolFunction.Strict = &strict
			},
			wantErr: "strict mode",
		},
		{
			name: "OpenAIBuiltInTool",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = []schemas.ResponsesTool{{
					Type:                   schemas.ResponsesToolTypeWebSearch,
					ResponsesToolWebSearch: &schemas.ResponsesToolWebSearch{},
				}}
			},
			wantErr: "function tools only",
		},
		{
			name: "ParallelToolCalls",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.ParallelToolCalls = &parallelToolCalls
			},
			wantErr: "parallel_tool_calls",
		},
		{
			name: "RequiredToolChoice",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.ToolChoice = &schemas.ResponsesToolChoice{ResponsesToolChoiceStr: schemas.Ptr("required")}
			},
			wantErr: "tool_choice",
		},
		{
			name: "AutoToolChoiceWithoutFunctions",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = nil
				request.Params.ToolChoice = &schemas.ResponsesToolChoice{ResponsesToolChoiceStr: schemas.Ptr("auto")}
			},
			wantErr: "requires at least one",
		},
		{
			name: "UnknownForcedToolChoice",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.ToolChoice = &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
					Type: schemas.ResponsesToolChoiceTypeFunction,
					Name: schemas.Ptr("missing_tool"),
				}}
			},
			wantErr: "must match",
		},
		{
			name: "ExtraParamToolConfigBypass",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.ExtraParams = map[string]interface{}{"tool_config": map[string]interface{}{"mode": "auto"}}
			},
			wantErr: "extra_params.tool_config",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := testGigaChatResponsesToolRequest(t, "get_weather")
			test.mutate(request)
			_, err := ToGigaChatResponsesRequest(request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
			}
		})
	}
}

func testGigaChatChatToolRequest(t *testing.T, toolName string) *schemas.BifrostChatRequest {
	t.Helper()

	return &schemas.BifrostChatRequest{
		Model: "GigaChat",
		Input: []schemas.ChatMessage{{
			Role:    schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("Weather?")},
		}},
		Params: &schemas.ChatParameters{
			Tools: []schemas.ChatTool{testGigaChatChatFunctionTool(t, toolName)},
		},
	}
}

func testGigaChatResponsesToolRequest(t *testing.T, toolName string) *schemas.BifrostResponsesRequest {
	t.Helper()

	return &schemas.BifrostResponsesRequest{
		Model: "GigaChat-2",
		Input: []schemas.ResponsesMessage{{
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("Weather?")},
		}},
		Params: &schemas.ResponsesParameters{
			Tools: []schemas.ResponsesTool{{
				Type:        schemas.ResponsesToolTypeFunction,
				Name:        schemas.Ptr(toolName),
				Description: schemas.Ptr("Gets current weather."),
				ResponsesToolFunction: &schemas.ResponsesToolFunction{
					Parameters: mustGigaChatToolParameters(t, `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
				},
			}},
		},
	}
}

func testGigaChatChatFunctionTool(t *testing.T, toolName string) schemas.ChatTool {
	t.Helper()

	return schemas.ChatTool{
		Type: schemas.ChatToolTypeFunction,
		Function: &schemas.ChatToolFunction{
			Name:        toolName,
			Description: schemas.Ptr("Gets current weather."),
			Parameters:  mustGigaChatToolParameters(t, `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		},
	}
}

func invalidGigaChatToolParameters() *schemas.ToolFunctionParameters {
	return &schemas.ToolFunctionParameters{
		Type:                 "object",
		AdditionalProperties: &schemas.AdditionalPropertiesStruct{},
	}
}
