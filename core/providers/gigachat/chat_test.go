package gigachat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

func testGigaChatChatCompletion(t *testing.T) {
	t.Parallel()

	t.Run("ConverterMapsRequest", testGigaChatChatConverterMapsRequest)
	t.Run("ExecutesWithOAuthTokenAndExtraParams", testGigaChatChatCompletionExecutesWithOAuthTokenAndExtraParams)
	t.Run("RejectsUnsupportedTools", testGigaChatChatCompletionRejectsUnsupportedTools)
	t.Run("MapsProviderErrors", testGigaChatChatCompletionMapsProviderErrors)
	t.Run("RefreshesTokenAfterUnauthorized", testGigaChatChatCompletionRefreshesTokenAfterUnauthorized)
	t.Run("StreamsSSEChunks", testGigaChatChatCompletionStreamsSSEChunks)
	t.Run("MapsStreamingProviderErrors", testGigaChatChatCompletionMapsStreamingProviderErrors)
	t.Run("MapsStreamingErrorEvents", testGigaChatChatCompletionMapsStreamingErrorEvents)
	t.Run("RefreshesStreamingTokenAfterUnauthorized", testGigaChatChatCompletionStreamRefreshesTokenAfterUnauthorized)
	t.Run("HandlesStreamingContextCancellation", testGigaChatChatCompletionStreamHandlesContextCancellation)
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

func testGigaChatChatCompletionStreamsSSEChunks(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	var streamRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth":
			tokenRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"stream-access-token","expires_at":1893456000}`))
		case "/v1/chat/completions":
			streamRequests.Add(1)
			if got := request.Header.Get("Authorization"); got != "Bearer stream-access-token" {
				t.Fatalf("stream authorization header mismatch: got %q", got)
			}
			if strings.Contains(request.Header.Get("Authorization"), "super-secret-credentials") {
				t.Fatal("stream request leaked OAuth credentials")
			}
			assertGigaChatChatStreamRequestBody(t, request)
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Request-ID", "stream-request-id")
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"З\"}}],\"created\":1700000000,\"model\":\"GigaChat\",\"object\":\"chat.completion\"}\n\n"))
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"дравствуйте\"}}],\"created\":1700000000,\"model\":\"GigaChat\",\"object\":\"chat.completion\"}\n\n"))
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"created\":1700000000,\"model\":\"GigaChat\",\"object\":\"chat.completion\",\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	ctx := testBifrostContext()
	ctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)

	stream, bifrostErr := provider.ChatCompletionStream(ctx, testGigaChatPostHookRunner, nil, testGigaChatOAuthKey(server.URL+"/oauth", "", "super-secret-credentials"), testGigaChatChatRequest())
	if bifrostErr != nil {
		t.Fatalf("ChatCompletionStream returned error: %v", bifrostErr)
	}

	chunks := collectGigaChatStreamChunks(t, stream)
	if tokenRequests.Load() != 1 {
		t.Fatalf("token request count mismatch: got %d, want 1", tokenRequests.Load())
	}
	if streamRequests.Load() != 1 {
		t.Fatalf("stream request count mismatch: got %d, want 1", streamRequests.Load())
	}
	if len(chunks) != 3 {
		t.Fatalf("chunk count mismatch: got %d, want 3: %#v", len(chunks), chunks)
	}

	assertGigaChatStreamContentChunk(t, chunks[0], "З")
	assertGigaChatStreamContentChunk(t, chunks[1], "дравствуйте")
	finalChunk := chunks[2].BifrostChatResponse
	if finalChunk == nil {
		t.Fatalf("final chunk missing chat response: %#v", chunks[2])
	}
	if finalChunk.ExtraFields.Provider != schemas.GigaChat {
		t.Fatalf("provider mismatch: got %q, want %q", finalChunk.ExtraFields.Provider, schemas.GigaChat)
	}
	if finalChunk.Usage == nil || finalChunk.Usage.TotalTokens != 10 {
		t.Fatalf("usage mismatch: %#v", finalChunk.Usage)
	}
	if len(finalChunk.Choices) != 1 || finalChunk.Choices[0].FinishReason == nil || *finalChunk.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish reason mismatch: %#v", finalChunk.Choices)
	}
	if got := ctx.Value(schemas.BifrostContextKeyProviderResponseHeaders); got == nil {
		t.Fatal("provider response headers were not stored in context")
	}
}

func testGigaChatChatCompletionMapsStreamingProviderErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":400,"code":123,"message":"bad stream request"}`))
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	stream, bifrostErr := provider.ChatCompletionStream(testBifrostContext(), testGigaChatPostHookRunner, nil, testGigaChatAccessTokenKey("provider-error-token"), testGigaChatChatRequest())
	if stream != nil {
		t.Fatalf("expected nil stream, got %#v", stream)
	}
	if bifrostErr == nil {
		t.Fatal("expected provider error, got nil")
	}
	if bifrostErr.StatusCode == nil || *bifrostErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status mismatch: %#v", bifrostErr.StatusCode)
	}
	if bifrostErr.Error == nil || bifrostErr.Error.Message != "bad stream request" {
		t.Fatalf("message mismatch: %#v", bifrostErr.Error)
	}
	assertNoGigaChatSecretLeak(t, bifrostErr.String())
}

func testGigaChatChatCompletionMapsStreamingErrorEvents(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"status\":429,\"code\":42901,\"message\":\"rate limit\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	stream, bifrostErr := provider.ChatCompletionStream(testBifrostContext(), testGigaChatPostHookRunner, nil, testGigaChatAccessTokenKey("provider-error-token"), testGigaChatChatRequest())
	if bifrostErr != nil {
		t.Fatalf("ChatCompletionStream returned error before stream: %v", bifrostErr)
	}
	chunks := collectGigaChatStreamChunks(t, stream)
	if len(chunks) != 1 || chunks[0].BifrostError == nil {
		t.Fatalf("expected one error chunk, got %#v", chunks)
	}
	streamErr := chunks[0].BifrostError
	if streamErr.StatusCode == nil || *streamErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status mismatch: %#v", streamErr.StatusCode)
	}
	if streamErr.Error == nil || streamErr.Error.Message != "rate limit" {
		t.Fatalf("message mismatch: %#v", streamErr.Error)
	}
	if streamErr.Error.Code == nil || *streamErr.Error.Code != "42901" {
		t.Fatalf("code mismatch: %#v", streamErr.Error)
	}
}

func testGigaChatChatCompletionStreamRefreshesTokenAfterUnauthorized(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	var streamRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth":
			tokenIndex := tokenRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"access_token":"stream-token-%d","expires_at":1893456000}`, tokenIndex)))
		case "/v1/chat/completions":
			streamIndex := streamRequests.Add(1)
			wantAuthorization := fmt.Sprintf("Bearer stream-token-%d", streamIndex)
			if got := request.Header.Get("Authorization"); got != wantAuthorization {
				t.Fatalf("authorization header mismatch on stream request %d: got %q, want %q", streamIndex, got, wantAuthorization)
			}
			if streamIndex == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"status":401,"message":"expired token"}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}],\"model\":\"GigaChat\",\"object\":\"chat.completion\"}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	stream, bifrostErr := provider.ChatCompletionStream(testBifrostContext(), testGigaChatPostHookRunner, nil, testGigaChatOAuthKey(server.URL+"/oauth", "", "test-credentials"), testGigaChatChatRequest())
	if bifrostErr != nil {
		t.Fatalf("ChatCompletionStream returned error: %v", bifrostErr)
	}
	chunks := collectGigaChatStreamChunks(t, stream)
	if tokenRequests.Load() != 2 {
		t.Fatalf("token request count mismatch: got %d, want 2", tokenRequests.Load())
	}
	if streamRequests.Load() != 2 {
		t.Fatalf("stream request count mismatch: got %d, want 2", streamRequests.Load())
	}
	if len(chunks) != 2 {
		t.Fatalf("chunk count mismatch: got %d, want 2: %#v", len(chunks), chunks)
	}
	assertGigaChatStreamContentChunk(t, chunks[0], "ok")
}

func testGigaChatChatCompletionStreamHandlesContextCancellation(t *testing.T) {
	t.Parallel()

	firstChunkWritten := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}],\"model\":\"GigaChat\",\"object\":\"chat.completion\"}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(firstChunkWritten)
		<-request.Context().Done()
	}))
	defer server.Close()

	provider := newTestGigaChatChatProvider(t, server.URL)
	ctx, cancel := schemas.NewBifrostContextWithCancel(context.Background())
	stream, bifrostErr := provider.ChatCompletionStream(ctx, testGigaChatPostHookRunner, nil, testGigaChatAccessTokenKey("stream-token"), testGigaChatChatRequest())
	if bifrostErr != nil {
		t.Fatalf("ChatCompletionStream returned error: %v", bifrostErr)
	}

	select {
	case <-firstChunkWritten:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first stream chunk")
	}

	firstChunk := <-stream
	assertGigaChatStreamContentChunk(t, firstChunk, "partial")
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for context cancellation")
	}

	streamClosed := make(chan struct{})
	go func() {
		for range stream {
		}
		close(streamClosed)
	}()

	select {
	case <-streamClosed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream to close after context cancellation")
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

func testGigaChatPostHookRunner(_ *schemas.BifrostContext, response *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError) {
	return response, bifrostErr
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

	assertGigaChatChatRequestBodyWithStream(t, request, false)
}

func assertGigaChatChatStreamRequestBody(t *testing.T, request *http.Request) {
	t.Helper()

	assertGigaChatChatRequestBodyWithStream(t, request, true)
}

func assertGigaChatChatRequestBodyWithStream(t *testing.T, request *http.Request, wantStream bool) {
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
	if got := payload["stream"]; got != wantStream {
		t.Fatalf("stream mismatch: got %#v, want %v", got, wantStream)
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

func collectGigaChatStreamChunks(t *testing.T, stream chan *schemas.BifrostStreamChunk) []*schemas.BifrostStreamChunk {
	t.Helper()

	chunks := make([]*schemas.BifrostStreamChunk, 0)
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func assertGigaChatStreamContentChunk(t *testing.T, chunk *schemas.BifrostStreamChunk, wantContent string) {
	t.Helper()

	if chunk == nil || chunk.BifrostChatResponse == nil {
		t.Fatalf("missing chat stream response: %#v", chunk)
	}
	response := chunk.BifrostChatResponse
	if response.ExtraFields.Provider != schemas.GigaChat {
		t.Fatalf("provider mismatch: got %q, want %q", response.ExtraFields.Provider, schemas.GigaChat)
	}
	if len(response.Choices) != 1 || response.Choices[0].ChatStreamResponseChoice == nil || response.Choices[0].ChatStreamResponseChoice.Delta == nil {
		t.Fatalf("unexpected choices: %#v", response.Choices)
	}
	content := response.Choices[0].ChatStreamResponseChoice.Delta.Content
	if content == nil || *content != wantContent {
		t.Fatalf("content mismatch: got %#v, want %q", content, wantContent)
	}
}
