package gigachat

import (
	"fmt"
	"strings"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

func toGigaChatResponsesTools(tools []schemas.ResponsesTool) ([]GigaChatResponsesTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}

	specifications := make([]GigaChatResponsesFunctionSpecification, 0, len(tools))
	for index, tool := range tools {
		if tool.Type != schemas.ResponsesToolTypeFunction {
			return nil, fmt.Errorf("tools[%d]: GigaChat Responses supports function tools only, got %q", index, tool.Type)
		}
		if tool.Name == nil || strings.TrimSpace(*tool.Name) == "" {
			return nil, fmt.Errorf("tools[%d]: function tool name is required", index)
		}
		if tool.ResponsesToolFunction == nil {
			return nil, fmt.Errorf("tools[%d]: function tool definition is required", index)
		}
		if err := validateGigaChatFunctionParameters(tool.ResponsesToolFunction.Parameters); err != nil {
			return nil, fmt.Errorf("tools[%d]: %w", index, err)
		}

		specifications = append(specifications, GigaChatResponsesFunctionSpecification{
			Name:        strings.TrimSpace(*tool.Name),
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

func validateGigaChatFunctionParameters(parameters *schemas.ToolFunctionParameters) error {
	if parameters == nil {
		return fmt.Errorf("function parameters JSON schema is required")
	}
	if _, err := schemas.MarshalSorted(parameters); err != nil {
		return fmt.Errorf("function parameters JSON schema is invalid: %w", err)
	}
	return nil
}

func toGigaChatResponsesToolConfig(toolChoice *schemas.ResponsesToolChoice) (*GigaChatResponsesToolConfig, error) {
	if toolChoice == nil {
		return nil, nil
	}
	if toolChoice.ResponsesToolChoiceStr != nil {
		switch strings.TrimSpace(*toolChoice.ResponsesToolChoiceStr) {
		case "", "auto":
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
		return &GigaChatResponsesToolConfig{
			Mode:         "forced",
			FunctionName: &name,
		}, nil
	case schemas.ResponsesToolChoiceTypeAuto:
		return &GigaChatResponsesToolConfig{Mode: "auto"}, nil
	case schemas.ResponsesToolChoiceTypeNone:
		return &GigaChatResponsesToolConfig{Mode: "none"}, nil
	default:
		return nil, fmt.Errorf("tool_choice type %q is not supported by GigaChat Responses", choice.Type)
	}
}
