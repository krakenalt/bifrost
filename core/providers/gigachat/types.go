// Package gigachat implements the GigaChat LLM provider.
package gigachat

import (
	"encoding/json"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

// # AUTH TYPES

// GigaChatTokenResponse is returned by the GigaChat OAuth endpoint.
type GigaChatTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"`
}

// GigaChatPasswordTokenResponse is returned by the SDK-backed password auth endpoint.
type GigaChatPasswordTokenResponse struct {
	Token     string `json:"tok"`
	ExpiresAt int64  `json:"exp"`
}

// # CHAT TYPES

// GigaChatChatRequest is the v1 chat completions request body.
type GigaChatChatRequest struct {
	Model       string                 `json:"model"`
	Messages    []GigaChatChatMessage  `json:"messages"`
	Temperature *float64               `json:"temperature,omitempty"`
	TopP        *float64               `json:"top_p,omitempty"`
	MaxTokens   *int                   `json:"max_tokens,omitempty"`
	N           *int                   `json:"n,omitempty"`
	Stop        []string               `json:"stop,omitempty"`
	Stream      *bool                  `json:"stream,omitempty"`
	ExtraParams map[string]interface{} `json:"-"`
}

// GetExtraParams returns provider-specific passthrough fields.
func (request *GigaChatChatRequest) GetExtraParams() map[string]interface{} {
	if request == nil || request.ExtraParams == nil {
		return make(map[string]interface{}, 0)
	}
	return request.ExtraParams
}

// GigaChatChatMessage is a GigaChat v1 chat message.
type GigaChatChatMessage struct {
	Role             string                      `json:"role,omitempty"`
	Content          *schemas.ChatMessageContent `json:"content,omitempty"`
	Name             *string                     `json:"name,omitempty"`
	FunctionCall     *GigaChatFunctionCall       `json:"function_call,omitempty"`
	FunctionsStateID *string                     `json:"functions_state_id,omitempty"`
}

// GigaChatFunctionCall is the legacy GigaChat function-call shape.
type GigaChatFunctionCall struct {
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// GigaChatChatResponse is the v1 chat completions response body.
type GigaChatChatResponse struct {
	ID                string                 `json:"id,omitempty"`
	Choices           []GigaChatChatChoice   `json:"choices,omitempty"`
	Created           int                    `json:"created,omitempty"`
	Model             string                 `json:"model,omitempty"`
	Object            string                 `json:"object,omitempty"`
	SystemFingerprint string                 `json:"system_fingerprint,omitempty"`
	Usage             *GigaChatChatUsage     `json:"usage,omitempty"`
	ExtraParams       map[string]interface{} `json:"-"`
}

// GigaChatChatChoice is a single v1 chat completion choice.
type GigaChatChatChoice struct {
	Index        int                      `json:"index"`
	Message      *GigaChatChatMessage     `json:"message,omitempty"`
	FinishReason *string                  `json:"finish_reason,omitempty"`
	LogProbs     *schemas.BifrostLogProbs `json:"logprobs,omitempty"`
}

// GigaChatChatStreamResponse is a v1 chat completions SSE chunk.
type GigaChatChatStreamResponse struct {
	ID                string                     `json:"id,omitempty"`
	Choices           []GigaChatChatStreamChoice `json:"choices,omitempty"`
	Created           int                        `json:"created,omitempty"`
	Model             string                     `json:"model,omitempty"`
	Object            string                     `json:"object,omitempty"`
	SystemFingerprint string                     `json:"system_fingerprint,omitempty"`
	Usage             *GigaChatChatUsage         `json:"usage,omitempty"`
	ExtraParams       map[string]interface{}     `json:"-"`
}

// GigaChatChatStreamChoice is a single streaming choice.
type GigaChatChatStreamChoice struct {
	Index        int                      `json:"index"`
	Delta        *GigaChatChatStreamDelta `json:"delta,omitempty"`
	FinishReason *string                  `json:"finish_reason,omitempty"`
	LogProbs     *schemas.BifrostLogProbs `json:"logprobs,omitempty"`
}

// GigaChatChatStreamDelta is the partial assistant message in an SSE chunk.
type GigaChatChatStreamDelta struct {
	Role             *string               `json:"role,omitempty"`
	Content          *string               `json:"content,omitempty"`
	FunctionCall     *GigaChatFunctionCall `json:"function_call,omitempty"`
	FunctionsStateID *string               `json:"functions_state_id,omitempty"`
}

// GigaChatChatUsage is token usage returned by GigaChat chat completions.
type GigaChatChatUsage struct {
	PromptTokens          int `json:"prompt_tokens,omitempty"`
	CompletionTokens      int `json:"completion_tokens,omitempty"`
	TotalTokens           int `json:"total_tokens,omitempty"`
	PrecachedPromptTokens int `json:"precached_prompt_tokens,omitempty"`
}

// # ERROR TYPES

// GigaChatErrorResponse is the common REST API error shape used by GigaChat.
type GigaChatErrorResponse struct {
	Status  *int   `json:"status,omitempty"`
	Code    *int   `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
