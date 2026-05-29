package gigachat

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

var gigaChatFunctionNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var gigaChatBuiltInFunctionNames = map[string]struct{}{
	"text2image":       {},
	"get_file_content": {},
	"text2model3d":     {},
}

// GigaChat built-ins are service-side functions with provider-specific side effects.
// They are intentionally rejected through neutral Bifrost tool fields until the provider has a scoped API for them.
func validateGigaChatFunctionName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("function tool name is required")
	}
	if _, ok := gigaChatBuiltInFunctionNames[trimmed]; ok {
		return fmt.Errorf("GigaChat built-in function %q is not supported through neutral function tools", trimmed)
	}
	if !gigaChatFunctionNamePattern.MatchString(trimmed) {
		return fmt.Errorf("function tool name %q must start with a latin letter or underscore and contain only latin letters, digits, or underscores", trimmed)
	}
	return nil
}

func validateGigaChatFunctionParameters(parameters *schemas.ToolFunctionParameters) error {
	if parameters == nil {
		return fmt.Errorf("function parameters JSON schema is required")
	}
	if _, err := schemas.MarshalSorted(parameters); err != nil {
		return fmt.Errorf("function parameters JSON schema is invalid: %w", err)
	}
	return nil
}

func validateGigaChatFunctionStrict(strict *bool) error {
	if strict != nil && *strict {
		return fmt.Errorf("function strict mode is not supported by GigaChat")
	}
	return nil
}

func toGigaChatChatFunctions(tools []schemas.ChatTool) ([]GigaChatFunction, map[string]struct{}, error) {
	if len(tools) == 0 {
		return nil, nil, nil
	}

	functions := make([]GigaChatFunction, 0, len(tools))
	functionNames := make(map[string]struct{}, len(tools))
	for index, tool := range tools {
		if tool.Type != schemas.ChatToolTypeFunction {
			return nil, nil, fmt.Errorf("tools[%d]: GigaChat chat completions support user-defined function tools only, got %q", index, tool.Type)
		}
		if tool.Function == nil {
			return nil, nil, fmt.Errorf("tools[%d]: function tool definition is required", index)
		}
		name := strings.TrimSpace(tool.Function.Name)
		if err := validateGigaChatFunctionName(name); err != nil {
			return nil, nil, fmt.Errorf("tools[%d]: %w", index, err)
		}
		if err := validateGigaChatFunctionParameters(tool.Function.Parameters); err != nil {
			return nil, nil, fmt.Errorf("tools[%d]: %w", index, err)
		}
		if err := validateGigaChatFunctionStrict(tool.Function.Strict); err != nil {
			return nil, nil, fmt.Errorf("tools[%d]: %w", index, err)
		}
		if _, exists := functionNames[name]; exists {
			return nil, nil, fmt.Errorf("tools[%d]: duplicate function tool name %q", index, name)
		}

		functionNames[name] = struct{}{}
		functions = append(functions, GigaChatFunction{
			Name:        name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}

	return functions, functionNames, nil
}

func toGigaChatChatFunctionCall(toolChoice *schemas.ChatToolChoice, functionNames map[string]struct{}) (interface{}, error) {
	if toolChoice == nil {
		return nil, nil
	}
	if toolChoice.ChatToolChoiceStr != nil {
		switch strings.TrimSpace(*toolChoice.ChatToolChoiceStr) {
		case "":
			return nil, nil
		case "auto":
			if len(functionNames) == 0 {
				return nil, fmt.Errorf("tool_choice auto requires at least one declared GigaChat function tool")
			}
			return "auto", nil
		case "none":
			return "none", nil
		default:
			return nil, fmt.Errorf("tool_choice %q is not supported by GigaChat chat completions", *toolChoice.ChatToolChoiceStr)
		}
	}
	if toolChoice.ChatToolChoiceStruct == nil {
		return nil, nil
	}

	choice := toolChoice.ChatToolChoiceStruct
	switch choice.Type {
	case schemas.ChatToolChoiceTypeFunction:
		if choice.Function == nil || strings.TrimSpace(choice.Function.Name) == "" {
			return nil, fmt.Errorf("tool_choice function name is required")
		}
		name := strings.TrimSpace(choice.Function.Name)
		if _, ok := functionNames[name]; !ok {
			return nil, fmt.Errorf("tool_choice function %q must match a declared GigaChat function tool", name)
		}
		return GigaChatFunctionCallChoice{Name: name}, nil
	case schemas.ChatToolChoiceTypeAuto:
		if len(functionNames) == 0 {
			return nil, fmt.Errorf("tool_choice auto requires at least one declared GigaChat function tool")
		}
		return "auto", nil
	case schemas.ChatToolChoiceTypeNone:
		return "none", nil
	default:
		return nil, fmt.Errorf("tool_choice type %q is not supported by GigaChat chat completions", choice.Type)
	}
}

func toGigaChatResponsesTools(tools []schemas.ResponsesTool) ([]GigaChatResponsesTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}

	specifications := make([]GigaChatResponsesFunctionSpecification, 0, len(tools))
	for index, tool := range tools {
		if tool.Type != schemas.ResponsesToolTypeFunction {
			return nil, fmt.Errorf("tools[%d]: GigaChat Responses supports user-defined function tools only, got %q", index, tool.Type)
		}
		if tool.Name == nil || strings.TrimSpace(*tool.Name) == "" {
			return nil, fmt.Errorf("tools[%d]: function tool name is required", index)
		}
		name := strings.TrimSpace(*tool.Name)
		if err := validateGigaChatFunctionName(name); err != nil {
			return nil, fmt.Errorf("tools[%d]: %w", index, err)
		}
		if tool.ResponsesToolFunction == nil {
			return nil, fmt.Errorf("tools[%d]: function tool definition is required", index)
		}
		if err := validateGigaChatFunctionParameters(tool.ResponsesToolFunction.Parameters); err != nil {
			return nil, fmt.Errorf("tools[%d]: %w", index, err)
		}
		if err := validateGigaChatFunctionStrict(tool.ResponsesToolFunction.Strict); err != nil {
			return nil, fmt.Errorf("tools[%d]: %w", index, err)
		}

		specifications = append(specifications, GigaChatResponsesFunctionSpecification{
			Name:        name,
			Description: tool.Description,
			Parameters:  tool.ResponsesToolFunction.Parameters,
		})
	}

	return []GigaChatResponsesTool{{
		Functions: &GigaChatResponsesFunctionsTool{
			Specifications: specifications,
		},
	}}, nil
}

func toGigaChatResponsesToolConfig(toolChoice *schemas.ResponsesToolChoice, tools []schemas.ResponsesTool) (*GigaChatResponsesToolConfig, error) {
	if toolChoice == nil {
		return nil, nil
	}
	if toolChoice.ResponsesToolChoiceStr != nil {
		switch strings.TrimSpace(*toolChoice.ResponsesToolChoiceStr) {
		case "":
			return nil, nil
		case "auto":
			if len(tools) == 0 {
				return nil, fmt.Errorf("tool_choice auto requires at least one declared GigaChat function tool")
			}
			return &GigaChatResponsesToolConfig{Mode: "auto"}, nil
		case "none":
			return &GigaChatResponsesToolConfig{Mode: "none"}, nil
		default:
			return nil, fmt.Errorf("tool_choice %q is not supported by GigaChat Responses", *toolChoice.ResponsesToolChoiceStr)
		}
	}
	if toolChoice.ResponsesToolChoiceStruct == nil {
		return nil, nil
	}

	choice := toolChoice.ResponsesToolChoiceStruct
	switch choice.Type {
	case schemas.ResponsesToolChoiceTypeFunction:
		if choice.Name == nil || strings.TrimSpace(*choice.Name) == "" {
			return nil, fmt.Errorf("tool_choice function name is required")
		}
		name := strings.TrimSpace(*choice.Name)
		if !gigaChatResponsesToolNameExists(tools, name) {
			return nil, fmt.Errorf("tool_choice function %q must match a declared GigaChat function tool", name)
		}
		return &GigaChatResponsesToolConfig{
			Mode:         "forced",
			FunctionName: &name,
		}, nil
	case schemas.ResponsesToolChoiceTypeAuto:
		if len(tools) == 0 {
			return nil, fmt.Errorf("tool_choice auto requires at least one declared GigaChat function tool")
		}
		return &GigaChatResponsesToolConfig{Mode: "auto"}, nil
	case schemas.ResponsesToolChoiceTypeNone:
		return &GigaChatResponsesToolConfig{Mode: "none"}, nil
	default:
		return nil, fmt.Errorf("tool_choice type %q is not supported by GigaChat Responses", choice.Type)
	}
}

func gigaChatResponsesToolNameExists(tools []schemas.ResponsesTool, name string) bool {
	for _, tool := range tools {
		if tool.Type == schemas.ResponsesToolTypeFunction && tool.Name != nil && strings.TrimSpace(*tool.Name) == name {
			return true
		}
	}
	return false
}

func unsupportedGigaChatToolControlExtraParams(extraParams map[string]interface{}, names ...string) []string {
	if len(extraParams) == 0 {
		return nil
	}

	unsupported := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := extraParams[name]; ok {
			unsupported = append(unsupported, "extra_params."+name)
		}
	}
	sort.Strings(unsupported)
	return unsupported
}
