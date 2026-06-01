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
	t.Run("ResponsesMapsBuiltInTools", testGigaChatToolsResponsesMapsBuiltInTools)
	t.Run("ResponsesToolChoiceVariants", testGigaChatToolsResponsesToolChoiceVariants)
	t.Run("ResponsesRemapsReservedFunctionNames", testGigaChatToolsResponsesRemapsReservedFunctionNames)
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

func testGigaChatToolsResponsesMapsBuiltInTools(t *testing.T) {
	t.Parallel()

	functionName := "web_search"
	imageModel := "Kandinsky"
	imageSize := "1024x1024"
	searchContextSize := "high"
	city := "Moscow"
	country := "RU"
	maxContentTokens := 2000
	request := testGigaChatResponsesToolRequest(t, functionName)
	request.Params.Tools = append(request.Params.Tools,
		schemas.ResponsesTool{
			Type: schemas.ResponsesToolTypeWebSearchPreview,
			ResponsesToolWebSearchPreview: &schemas.ResponsesToolWebSearchPreview{
				SearchContextSize: &searchContextSize,
				UserLocation: &schemas.ResponsesToolWebSearchUserLocation{
					City:    &city,
					Country: &country,
				},
			},
		},
		schemas.ResponsesTool{
			Type: schemas.ResponsesToolTypeCodeInterpreter,
			ResponsesToolCodeInterpreter: &schemas.ResponsesToolCodeInterpreter{
				Container: map[string]interface{}{"type": "auto"},
			},
		},
		schemas.ResponsesTool{
			Type: schemas.ResponsesToolTypeImageGeneration,
			ResponsesToolImageGeneration: &schemas.ResponsesToolImageGeneration{
				Model: &imageModel,
				Size:  &imageSize,
			},
		},
		schemas.ResponsesTool{
			Type: schemas.ResponsesToolTypeWebFetch,
			ResponsesToolWebFetch: &schemas.ResponsesToolWebFetch{
				MaxContentTokens: &maxContentTokens,
			},
		},
		schemas.ResponsesTool{
			Type:        schemas.ResponsesToolType("model_3d_generate"),
			Name:        schemas.Ptr("make_model"),
			Description: schemas.Ptr("Generate a 3D model."),
		},
	)

	gigaChatReq, err := ToGigaChatResponsesRequest(request)
	if err != nil {
		t.Fatalf("ToGigaChatResponsesRequest returned error: %v", err)
	}
	if len(gigaChatReq.Tools) != 6 {
		t.Fatalf("tool count mismatch: got %d, tools=%#v", len(gigaChatReq.Tools), gigaChatReq.Tools)
	}
	functionsTool := gigaChatReq.Tools[0]
	if functionsTool.Functions == nil || len(functionsTool.Functions.Specifications) != 1 {
		t.Fatalf("functions tool mismatch: %#v", functionsTool)
	}
	if got := functionsTool.Functions.Specifications[0].Name; got != "__bifrost_gigachat_user_web_search" {
		t.Fatalf("function remap mismatch: got %q", got)
	}

	webSearchTool := gigaChatReq.Tools[1]
	if webSearchTool.WebSearch == nil || webSearchTool.WebSearch.Type == nil || *webSearchTool.WebSearch.Type != "web_search_preview" {
		t.Fatalf("web_search tool mismatch: %#v", webSearchTool)
	}
	if len(webSearchTool.WebSearch.Flags) != 1 || webSearchTool.WebSearch.Flags[0] != "search_context_size:high" {
		t.Fatalf("web_search flags mismatch: %#v", webSearchTool.WebSearch.Flags)
	}
	userLocation, ok := gigaChatReq.UserInfo["user_location"].(map[string]interface{})
	if !ok || userLocation["city"] != city || userLocation["country"] != country {
		t.Fatalf("user_info mismatch: %#v", gigaChatReq.UserInfo)
	}

	codeTool := gigaChatReq.Tools[2]
	if codeTool.CodeInterpreter["type"] != "code_interpreter" {
		t.Fatalf("code_interpreter config mismatch: %#v", codeTool.CodeInterpreter)
	}
	container, ok := codeTool.CodeInterpreter["container"].(map[string]interface{})
	if !ok || container["type"] != "auto" {
		t.Fatalf("code_interpreter container mismatch: %#v", codeTool.CodeInterpreter)
	}

	imageTool := gigaChatReq.Tools[3]
	if imageTool.ImageGenerate["type"] != "image_generation" ||
		imageTool.ImageGenerate["model"] != imageModel ||
		imageTool.ImageGenerate["size"] != imageSize {
		t.Fatalf("image_generate config mismatch: %#v", imageTool.ImageGenerate)
	}

	urlTool := gigaChatReq.Tools[4]
	if urlTool.URLContentExtraction["type"] != "web_fetch" ||
		urlTool.URLContentExtraction["max_content_tokens"] != float64(maxContentTokens) {
		t.Fatalf("url_content_extraction config mismatch: %#v", urlTool.URLContentExtraction)
	}

	model3DTool := gigaChatReq.Tools[5]
	if model3DTool.Model3DGenerate["type"] != "model_3d_generate" ||
		model3DTool.Model3DGenerate["name"] != "make_model" ||
		model3DTool.Model3DGenerate["description"] != "Generate a 3D model." {
		t.Fatalf("model_3d_generate config mismatch: %#v", model3DTool.Model3DGenerate)
	}
}

func testGigaChatToolsResponsesToolChoiceVariants(t *testing.T) {
	t.Parallel()

	toolName := "get_weather"
	tests := []struct {
		name         string
		choice       *schemas.ResponsesToolChoice
		mutate       func(*schemas.BifrostResponsesRequest)
		wantMode     string
		wantFunction string
		wantTool     string
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
			wantFunction: toolName,
		},
		{
			name: "StructReservedFunction",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceTypeFunction,
				Name: schemas.Ptr("web_search"),
			}},
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools[0].Name = schemas.Ptr("web_search")
			},
			wantFunction: "__bifrost_gigachat_user_web_search",
		},
		{
			name: "StringAutoWithBuiltInOnly",
			choice: &schemas.ResponsesToolChoice{
				ResponsesToolChoiceStr: schemas.Ptr("auto"),
			},
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = []schemas.ResponsesTool{{
					Type:                         schemas.ResponsesToolTypeCodeInterpreter,
					ResponsesToolCodeInterpreter: &schemas.ResponsesToolCodeInterpreter{Container: map[string]interface{}{"type": "auto"}},
				}}
			},
			wantMode: "auto",
		},
		{
			name: "StructCodeInterpreter",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceTypeCodeInterpreter,
			}},
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = []schemas.ResponsesTool{{
					Type:                         schemas.ResponsesToolTypeCodeInterpreter,
					ResponsesToolCodeInterpreter: &schemas.ResponsesToolCodeInterpreter{Container: map[string]interface{}{"type": "auto"}},
				}}
			},
			wantTool: "code_interpreter",
		},
		{
			name: "StructImageGeneration",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceTypeImageGeneration,
			}},
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = []schemas.ResponsesTool{{
					Type: schemas.ResponsesToolTypeImageGeneration,
					ResponsesToolImageGeneration: &schemas.ResponsesToolImageGeneration{
						Size: schemas.Ptr("1024x1024"),
					},
				}}
			},
			wantTool: "image_generate",
		},
		{
			name: "StructWebSearchPreview",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceTypeWebSearchPreview,
			}},
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = []schemas.ResponsesTool{{
					Type: schemas.ResponsesToolTypeWebSearchPreview,
					ResponsesToolWebSearchPreview: &schemas.ResponsesToolWebSearchPreview{
						SearchContextSize: schemas.Ptr("low"),
					},
				}}
			},
			wantTool: "web_search",
		},
		{
			name: "StructURLContentExtraction",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceType("url_content_extraction"),
			}},
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = []schemas.ResponsesTool{{
					Type: schemas.ResponsesToolType("url_content_extraction"),
				}}
			},
			wantTool: "url_content_extraction",
		},
		{
			name: "StructModel3DGenerate",
			choice: &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
				Type: schemas.ResponsesToolChoiceType("model_3d_generate"),
			}},
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = []schemas.ResponsesTool{{
					Type: schemas.ResponsesToolType("model_3d_generate"),
				}}
			},
			wantTool: "model_3d_generate",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := testGigaChatResponsesToolRequest(t, toolName)
			if test.mutate != nil {
				test.mutate(request)
			}
			request.Params.ToolChoice = test.choice
			gigaChatReq, err := ToGigaChatResponsesRequest(request)
			if err != nil {
				t.Fatalf("ToGigaChatResponsesRequest returned error: %v", err)
			}
			if test.wantFunction != "" {
				if gigaChatReq.ToolConfig == nil || gigaChatReq.ToolConfig.FunctionName == nil || *gigaChatReq.ToolConfig.FunctionName != test.wantFunction || gigaChatReq.ToolConfig.Mode != "forced" {
					t.Fatalf("forced tool_config mismatch: %#v", gigaChatReq.ToolConfig)
				}
				return
			}
			if test.wantTool != "" {
				if gigaChatReq.ToolConfig == nil || gigaChatReq.ToolConfig.ToolName == nil || *gigaChatReq.ToolConfig.ToolName != test.wantTool || gigaChatReq.ToolConfig.Mode != "forced" {
					t.Fatalf("forced built-in tool_config mismatch: %#v", gigaChatReq.ToolConfig)
				}
				return
			}
			if gigaChatReq.ToolConfig == nil || gigaChatReq.ToolConfig.Mode != test.wantMode {
				t.Fatalf("tool_config mode mismatch: got %#v, want %q", gigaChatReq.ToolConfig, test.wantMode)
			}
		})
	}
}

func testGigaChatToolsResponsesRemapsReservedFunctionNames(t *testing.T) {
	t.Parallel()

	functionName := "web_search"
	arguments := `{"query":"GigaChat"}`
	request := testGigaChatResponsesToolRequest(t, functionName)
	request.Input = append(request.Input, schemas.ResponsesMessage{
		Type: schemas.Ptr(schemas.ResponsesMessageTypeFunctionCall),
		ResponsesToolMessage: &schemas.ResponsesToolMessage{
			Name:      &functionName,
			CallID:    schemas.Ptr("state-web-search"),
			Arguments: &arguments,
		},
	})

	gigaChatReq, err := ToGigaChatResponsesRequest(request)
	if err != nil {
		t.Fatalf("ToGigaChatResponsesRequest returned error: %v", err)
	}
	if got := gigaChatReq.Tools[0].Functions.Specifications[0].Name; got != "__bifrost_gigachat_user_web_search" {
		t.Fatalf("function specification name mismatch: got %q", got)
	}
	if gigaChatReq.Messages[1].FunctionCall == nil || gigaChatReq.Messages[1].FunctionCall.Name != "__bifrost_gigachat_user_web_search" {
		t.Fatalf("function call remap mismatch: %#v", gigaChatReq.Messages[1].FunctionCall)
	}

	response := ToBifrostResponsesResponse(schemas.GigaChat, &GigaChatResponsesResponse{
		ID:    "resp-1",
		Model: "GigaChat-2",
		Messages: []GigaChatResponsesMessage{{
			Role:         "assistant",
			ToolsStateID: schemas.Ptr("state-web-search"),
			Content: []GigaChatResponsesContentPart{{
				FunctionCall: &GigaChatResponsesFunctionCall{
					Name:      "__bifrost_gigachat_user_web_search",
					Arguments: map[string]interface{}{"query": "GigaChat"},
				},
			}},
		}},
	})
	if response == nil || len(response.Output) != 1 || response.Output[0].ResponsesToolMessage == nil || response.Output[0].ResponsesToolMessage.Name == nil {
		t.Fatalf("response output mismatch: %#v", response)
	}
	if got := *response.Output[0].ResponsesToolMessage.Name; got != functionName {
		t.Fatalf("function response name mismatch: got %q", got)
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
			name: "UnsupportedHostedTool",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.Tools = []schemas.ResponsesTool{{
					Type: schemas.ResponsesToolTypeFileSearch,
					ResponsesToolFileSearch: &schemas.ResponsesToolFileSearch{
						VectorStoreIDs: []string{"vs_123"},
					},
				}}
			},
			wantErr: "does not support tool type",
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
			name: "AnyToolChoice",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.ToolChoice = &schemas.ResponsesToolChoice{ResponsesToolChoiceStr: schemas.Ptr("any")}
			},
			wantErr: "cannot require an arbitrary tool",
		},
		{
			name: "StructRequiredToolChoice",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.ToolChoice = &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
					Type: schemas.ResponsesToolChoiceTypeRequired,
				}}
			},
			wantErr: "cannot require an arbitrary tool",
		},
		{
			name: "AllowedToolsChoice",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.ToolChoice = &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
					Type: schemas.ResponsesToolChoiceTypeAllowedTools,
					Tools: []schemas.ResponsesToolChoiceAllowedToolDef{{
						Type: string(schemas.ResponsesToolTypeFunction),
						Name: schemas.Ptr("get_weather"),
					}},
				}}
			},
			wantErr: "allowed tools set",
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
			name: "UnknownBuiltInToolChoice",
			mutate: func(request *schemas.BifrostResponsesRequest) {
				request.Params.ToolChoice = &schemas.ResponsesToolChoice{ResponsesToolChoiceStruct: &schemas.ResponsesToolChoiceStruct{
					Type: schemas.ResponsesToolChoiceTypeCodeInterpreter,
				}}
			},
			wantErr: "must match a declared",
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
