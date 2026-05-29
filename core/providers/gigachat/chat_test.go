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

func testGigaChatChatCompletion(t *testing.T) {
	t.Parallel()

	t.Run("ConverterMapsRequest", testGigaChatChatConverterMapsRequest)
	t.Run("ExecutesWithOAuthTokenAndExtraParams", testGigaChatChatCompletionExecutesWithOAuthTokenAndExtraParams)
	t.Run("RejectsUnsupportedTools", testGigaChatChatCompletionRejectsUnsupportedTools)
	t.Run("MapsProviderErrors", testGigaChatChatCompletionMapsProviderErrors)
	t.Run("RefreshesTokenAfterUnauthorized", testGigaChatChatCompletionRefreshesTokenAfterUnauthorized)
}

func testGigaChatChatConverterMapsRequest(t *testing.T) {
	t.Parallel()

	maxTokens := 512
	temperature := 0.2
	topP := 0.8
	n := 1
	text := "hello"
	request := &schemas.BifrostChatRequest{
		Model: "GigaChat",
		Input: []schemas.ChatMessage{
			{
				Role:    schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{ContentStr: &text},
			},
		},
		Params: &schemas.ChatParameters{
			MaxCompletionTokens: &maxTokens,
			Temperature:         &temperature,
			TopP:                &topP,
			N:                   &n,
			Stop:                []string{"stop"},
			ExtraParams: map[string]interface{}{
				"profanity_check": false,
			},
		},
	}

	gigaChatReq, err := ToGigaChatChatRequest(testBifrostContext(), request)
	if err != nil {
		t.Fatalf("ToGigaChatChatRequest returned error: %v", err)
	}
	if gigaChatReq.MaxTokens == nil || *gigaChatReq.MaxTokens != maxTokens {
		t.Fatalf("max_tokens mismatch: got %#v, want %d", gigaChatReq.MaxTokens, maxTokens)
	}
	if gigaChatReq.Stream == nil || *gigaChatReq.Stream {
		t.Fatalf("stream mismatch: got %#v, want false", gigaChatReq.Stream)
	}
	if got := gigaChatReq.GetExtraParams()["profanity_check"]; got != false {
		t.Fatalf("extra param mismatch: got %#v", got)
	}

	body, err := json.Marshal(gigaChatReq)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	if strings.Contains(string(body), "max_completion_tokens") {
		t.Fatalf("request body should use max_tokens, got %s", body)
	}
	if !strings.Contains(string(body), `"max_tokens":512`) {
		t.Fatalf("request body missing max_tokens: %s", body)
	}
}

func testGigaChatChatCompletionExecutesWithOAuthTokenAndExtraParams(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	var chatRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth":
			tokenRequests.Add(1)
			if got := request.Header.Get("Authorization"); got != "Basic super-secret-credentials" {
				t.Fatalf("token authorization header mismatch: got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"chat-access-token","expires_at":1893456000}`))
		case "/v1/chat/completions":
			chatRequests.Add(1)
			if got := request.Header.Get("Authorization"); got != "Bearer chat-access-token" {
				t.Fatalf("chat authorization header mismatch: got %q", got)
			}
			if strings.Contains(request.Header.Get("Authorization"), "super-secret-credentials") {
				t.Fatal("chat request leaked OAuth credentials")
			}
			assertGigaChatChatRequestBody(t, request)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-ID", "chat-request-id")
			_, _ = w.Write([]byte(`{
				"id":"chatcmpl-test",
				"choices":[{"index":0,"message":{"role":"assistant","content":"Здравствуйте"},"finish_reason":"stop"}],
				"created":1700000000,
				"model":"GigaChat",
				"object":"chat.completion",
				"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"precached_prompt_tokens":2}
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	ctx := testBifrostContext()
	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)

	response, bifrostErr := provider.ChatCompletion(ctx, testGigaChatOAuthKey(server.URL+"/oauth", "", "super-secret-credentials"), testGigaChatChatRequest())
	if bifrostErr != nil {
		t.Fatalf("ChatCompletion returned error: %v", bifrostErr)
	}
	if tokenRequests.Load() != 1 {
		t.Fatalf("token request count mismatch: got %d, want 1", tokenRequests.Load())
	}
	if chatRequests.Load() != 1 {
		t.Fatalf("chat request count mismatch: got %d, want 1", chatRequests.Load())
	}
	if response.ID != "chatcmpl-test" {
		t.Fatalf("response id mismatch: got %q", response.ID)
	}
	if response.ExtraFields.Provider != schemas.GigaChat {
		t.Fatalf("provider mismatch: got %q, want %q", response.ExtraFields.Provider, schemas.GigaChat)
	}
	if response.Usage == nil || response.Usage.TotalTokens != 10 {
		t.Fatalf("usage mismatch: %#v", response.Usage)
	}
	if response.Usage.PromptTokensDetails == nil || response.Usage.PromptTokensDetails.CachedReadTokens != 2 {
		t.Fatalf("precached prompt tokens were not mapped: %#v", response.Usage.PromptTokensDetails)
	}
	if len(response.Choices) != 1 || response.Choices[0].ChatNonStreamResponseChoice == nil {
		t.Fatalf("unexpected choices: %#v", response.Choices)
	}
	content := response.Choices[0].ChatNonStreamResponseChoice.Message.Content
	if content == nil || content.ContentStr == nil || *content.ContentStr != "Здравствуйте" {
		t.Fatalf("content mismatch: %#v", content)
	}
	if got := ctx.Value(schemas.BifrostContextKeyProviderResponseHeaders); got == nil {
		t.Fatal("provider response headers were not stored in context")
	}
}

func testGigaChatChatCompletionRejectsUnsupportedTools(t *testing.T) {
	t.Parallel()

	text := "hello"
	request := &schemas.BifrostChatRequest{
		Model: "GigaChat",
		Input: []schemas.ChatMessage{
			{Role: schemas.ChatMessageRoleUser, Content: &schemas.ChatMessageContent{ContentStr: &text}},
		},
		Params: &schemas.ChatParameters{
			Tools: []schemas.ChatTool{
				{
					Type: schemas.ChatToolTypeFunction,
					Function: &schemas.ChatToolFunction{
						Name: "get_weather",
					},
				},
			},
		},
	}

	_, err := ToGigaChatChatRequest(testBifrostContext(), request)
	if err == nil {
		t.Fatal("expected unsupported tools error, got nil")
	}
	if !strings.Contains(err.Error(), "tools") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testGigaChatChatCompletionMapsProviderErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":400,"code":123,"message":"bad request"}`))
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	response, bifrostErr := provider.ChatCompletion(testBifrostContext(), testGigaChatAccessTokenKey("provider-error-token"), testGigaChatChatRequest())
	if response != nil {
		t.Fatalf("expected nil response, got %#v", response)
	}
	if bifrostErr == nil {
		t.Fatal("expected provider error, got nil")
	}
	if bifrostErr.StatusCode == nil || *bifrostErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status mismatch: %#v", bifrostErr.StatusCode)
	}
	if bifrostErr.Error == nil || bifrostErr.Error.Message != "bad request" {
		t.Fatalf("message mismatch: %#v", bifrostErr.Error)
	}
	if bifrostErr.Error.Code == nil || *bifrostErr.Error.Code != "123" {
		t.Fatalf("code mismatch: %#v", bifrostErr.Error)
	}
	assertNoGigaChatSecretLeak(t, bifrostErr.String())
}

func testGigaChatChatCompletionRefreshesTokenAfterUnauthorized(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	var chatRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth":
			tokenIndex := tokenRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"access_token":"token-%d","expires_at":1893456000}`, tokenIndex)))
		case "/v1/chat/completions":
			chatIndex := chatRequests.Add(1)
			wantAuthorization := fmt.Sprintf("Bearer token-%d", chatIndex)
			if got := request.Header.Get("Authorization"); got != wantAuthorization {
				t.Fatalf("authorization header mismatch on request %d: got %q, want %q", chatIndex, got, wantAuthorization)
			}
			w.Header().Set("Content-Type", "application/json")
			if chatIndex == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"status":401,"message":"expired token"}`))
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"model":"GigaChat","object":"chat.completion"}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	response, bifrostErr := provider.ChatCompletion(testBifrostContext(), testGigaChatOAuthKey(server.URL+"/oauth", "", "test-credentials"), testGigaChatChatRequest())
	if bifrostErr != nil {
		t.Fatalf("ChatCompletion returned error: %v", bifrostErr)
	}
	if response == nil {
		t.Fatal("expected response, got nil")
	}
	if tokenRequests.Load() != 2 {
		t.Fatalf("token request count mismatch: got %d, want 2", tokenRequests.Load())
	}
	if chatRequests.Load() != 2 {
		t.Fatalf("chat request count mismatch: got %d, want 2", chatRequests.Load())
	}
}

func newTestGigaChatChatProvider(t *testing.T, baseURL string) *GigaChatProvider {
	t.Helper()

	provider, err := NewGigaChatProvider(&schemas.ProviderConfig{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL: baseURL,
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewGigaChatProvider returned error: %v", err)
	}
	return provider
}

func testGigaChatAccessTokenKey(accessToken string) schemas.Key {
	return schemas.Key{
		GigaChatKeyConfig: &schemas.GigaChatKeyConfig{
			AccessToken: schemas.NewEnvVar(accessToken),
		},
	}
}

func testGigaChatChatRequest() *schemas.BifrostChatRequest {
	maxTokens := 128
	text := "Привет"
	return &schemas.BifrostChatRequest{
		Model: "GigaChat",
		Input: []schemas.ChatMessage{
			{
				Role:    schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{ContentStr: &text},
			},
		},
		Params: &schemas.ChatParameters{
			MaxCompletionTokens: &maxTokens,
			ExtraParams: map[string]interface{}{
				"profanity_check": false,
			},
		},
	}
}

func assertGigaChatChatRequestBody(t *testing.T, request *http.Request) {
	t.Helper()

	if request.Method != http.MethodPost {
		t.Fatalf("method mismatch: got %s, want POST", request.Method)
	}
	if got := request.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type mismatch: got %q", got)
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
	if got := payload["model"]; got != "GigaChat" {
		t.Fatalf("model mismatch: got %#v", got)
	}
	if got := payload["max_tokens"]; got != float64(128) {
		t.Fatalf("max_tokens mismatch: got %#v", got)
	}
	if _, ok := payload["max_completion_tokens"]; ok {
		t.Fatalf("max_completion_tokens should not be sent: %s", body)
	}
	if got := payload["stream"]; got != false {
		t.Fatalf("stream mismatch: got %#v", got)
	}
	if got := payload["profanity_check"]; got != false {
		t.Fatalf("profanity_check mismatch: got %#v", got)
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
	if got := message["content"]; got != "Привет" {
		t.Fatalf("message content mismatch: got %#v", got)
	}
}
