package gigachat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

const (
	gigaChatIntegrationChatModel      = "GigaChat-2"
	gigaChatIntegrationEmbeddingModel = "Embeddings"
	gigaChatIntegrationTimeout        = 90 * time.Second
)

type gigaChatIntegrationConfig struct {
	baseURL        string
	authURL        string
	scope          string
	hasOAuth       bool
	hasPassword    bool
	caBundleFile   string
	certFile       string
	keyFile        string
	keyFileHasPass bool
}

func TestGigaChatIntegration(t *testing.T) {
	config := loadGigaChatIntegrationConfig(t)
	provider := newGigaChatIntegrationProvider(t, config)
	key := config.inferenceKey()

	t.Run("OAuthTokenFetch", func(t *testing.T) {
		if !config.hasOAuth {
			t.Skip("set GIGACHAT_CREDENTIALS to run OAuth token integration test")
		}
		ctx := newGigaChatIntegrationContext(t)
		token, bifrostErr := provider.getOAuthAccessToken(ctx, config.oauthKey())
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "OAuth token fetch", bifrostErr)
		}
		if strings.TrimSpace(token) == "" {
			t.Fatal("OAuth token fetch returned an empty access token")
		}
	})

	t.Run("PasswordTokenFetch", func(t *testing.T) {
		if !config.hasPassword {
			t.Skip("set GIGACHAT_USER and GIGACHAT_PASSWORD to run password token integration test")
		}
		ctx := newGigaChatIntegrationContext(t)
		token, bifrostErr := provider.getPasswordAccessToken(ctx, config.passwordKey())
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "password token fetch", bifrostErr)
		}
		if strings.TrimSpace(token) == "" {
			t.Fatal("password token fetch returned an empty access token")
		}
	})

	t.Run("ChatCompletion", func(t *testing.T) {
		ctx := newGigaChatIntegrationContext(t)
		response, bifrostErr := provider.ChatCompletion(ctx, key, gigaChatIntegrationChatRequest())
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "chat completion", bifrostErr)
		}
		if response == nil || len(response.Choices) == 0 {
			t.Fatal("chat completion returned no choices")
		}
		if response.ExtraFields.Provider != schemas.GigaChat {
			t.Fatalf("provider mismatch: got %q, want %q", response.ExtraFields.Provider, schemas.GigaChat)
		}
	})

	t.Run("ChatCompletionStream", func(t *testing.T) {
		ctx := newGigaChatIntegrationContext(t)
		stream, bifrostErr := provider.ChatCompletionStream(ctx, testGigaChatPostHookRunner, nil, key, gigaChatIntegrationChatRequest())
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "chat completion stream", bifrostErr)
		}
		assertGigaChatIntegrationChatStream(t, stream)
	})

	t.Run("ListModels", func(t *testing.T) {
		ctx := newGigaChatIntegrationContext(t)
		response, bifrostErr := provider.ListModels(ctx, []schemas.Key{key}, &schemas.BifrostListModelsRequest{Provider: schemas.GigaChat})
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "list models", bifrostErr)
		}
		if response == nil || len(response.Data) == 0 {
			t.Fatal("list models returned no models")
		}
	})

	t.Run("Embedding", func(t *testing.T) {
		ctx := newGigaChatIntegrationContext(t)
		response, bifrostErr := provider.Embedding(ctx, key, gigaChatIntegrationEmbeddingRequest())
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "embedding", bifrostErr)
		}
		if response == nil || len(response.Data) == 0 {
			t.Fatal("embedding returned no vectors")
		}
		if response.ExtraFields.Provider != schemas.GigaChat {
			t.Fatalf("provider mismatch: got %q, want %q", response.ExtraFields.Provider, schemas.GigaChat)
		}
	})

	t.Run("Responses", func(t *testing.T) {
		ctx := newGigaChatIntegrationContext(t)
		response, bifrostErr := provider.Responses(ctx, key, gigaChatIntegrationResponsesRequest())
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "responses", bifrostErr)
		}
		if response == nil || len(response.Output) == 0 {
			t.Fatal("responses returned no output")
		}
		if response.ExtraFields.Provider != schemas.GigaChat {
			t.Fatalf("provider mismatch: got %q, want %q", response.ExtraFields.Provider, schemas.GigaChat)
		}
	})

	t.Run("ResponsesStream", func(t *testing.T) {
		ctx := newGigaChatIntegrationContext(t)
		stream, bifrostErr := provider.ResponsesStream(ctx, testGigaChatPostHookRunner, nil, key, gigaChatIntegrationResponsesRequest())
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "responses stream", bifrostErr)
		}
		assertGigaChatIntegrationResponsesStream(t, stream)
	})

	t.Run("FunctionTool", func(t *testing.T) {
		ctx := newGigaChatIntegrationContext(t)
		response, bifrostErr := provider.ChatCompletion(ctx, key, gigaChatIntegrationToolRequest(t))
		if bifrostErr != nil {
			failGigaChatIntegrationBifrostError(t, "function tool", bifrostErr)
		}
		if !gigaChatIntegrationHasToolCall(response) {
			t.Skip("GigaChat did not return a tool call for the forced tool request")
		}
	})
}

func loadGigaChatIntegrationConfig(t *testing.T) gigaChatIntegrationConfig {
	t.Helper()

	config := gigaChatIntegrationConfig{
		baseURL:        strings.TrimSpace(os.Getenv("GIGACHAT_BASE_URL")),
		authURL:        strings.TrimSpace(os.Getenv("GIGACHAT_AUTH_URL")),
		scope:          strings.TrimSpace(os.Getenv("GIGACHAT_SCOPE")),
		hasOAuth:       strings.TrimSpace(os.Getenv("GIGACHAT_CREDENTIALS")) != "",
		hasPassword:    strings.TrimSpace(os.Getenv("GIGACHAT_USER")) != "" && strings.TrimSpace(os.Getenv("GIGACHAT_PASSWORD")) != "",
		caBundleFile:   strings.TrimSpace(os.Getenv("GIGACHAT_CA_BUNDLE_FILE")),
		certFile:       strings.TrimSpace(os.Getenv("GIGACHAT_CERT_FILE")),
		keyFile:        strings.TrimSpace(os.Getenv("GIGACHAT_KEY_FILE")),
		keyFileHasPass: strings.TrimSpace(os.Getenv("GIGACHAT_KEY_FILE_PASSWORD")) != "",
	}

	if !config.hasOAuth && !config.hasPassword {
		t.Skip("set GIGACHAT_CREDENTIALS or GIGACHAT_USER/GIGACHAT_PASSWORD to run GigaChat integration tests")
	}
	if strings.TrimSpace(os.Getenv("GIGACHAT_USER")) != "" && strings.TrimSpace(os.Getenv("GIGACHAT_PASSWORD")) == "" {
		t.Fatal("GIGACHAT_PASSWORD must be set when GIGACHAT_USER is set")
	}
	if strings.TrimSpace(os.Getenv("GIGACHAT_PASSWORD")) != "" && strings.TrimSpace(os.Getenv("GIGACHAT_USER")) == "" {
		t.Fatal("GIGACHAT_USER must be set when GIGACHAT_PASSWORD is set")
	}
	if (config.certFile == "") != (config.keyFile == "") {
		t.Fatal("GIGACHAT_CERT_FILE and GIGACHAT_KEY_FILE must be set together")
	}
	if config.keyFileHasPass {
		t.Skip("GIGACHAT_KEY_FILE_PASSWORD is set, but encrypted GigaChat client private keys are not supported")
	}

	return config
}

func newGigaChatIntegrationProvider(t *testing.T, config gigaChatIntegrationConfig) *GigaChatProvider {
	t.Helper()

	providerConfig := &schemas.ProviderConfig{}
	if config.baseURL != "" {
		providerConfig.NetworkConfig.BaseURL = config.baseURL
	}
	provider, err := NewGigaChatProvider(providerConfig, nil)
	if err != nil {
		failGigaChatIntegrationError(t, "new provider", err)
	}
	return provider
}

func (config gigaChatIntegrationConfig) inferenceKey() schemas.Key {
	if config.hasOAuth {
		return config.oauthKey()
	}
	return config.passwordKey()
}

func (config gigaChatIntegrationConfig) oauthKey() schemas.Key {
	keyConfig := config.keyConfig()
	keyConfig.Credentials = schemas.NewEnvVar("env.GIGACHAT_CREDENTIALS")
	keyConfig.Scope = config.scope
	return schemas.Key{
		Name:              "gigachat-integration-oauth",
		Models:            schemas.WhiteList{"*"},
		GigaChatKeyConfig: keyConfig,
	}
}

func (config gigaChatIntegrationConfig) passwordKey() schemas.Key {
	keyConfig := config.keyConfig()
	keyConfig.User = schemas.NewEnvVar("env.GIGACHAT_USER")
	keyConfig.Password = schemas.NewEnvVar("env.GIGACHAT_PASSWORD")
	return schemas.Key{
		Name:              "gigachat-integration-password",
		Models:            schemas.WhiteList{"*"},
		GigaChatKeyConfig: keyConfig,
	}
}

func (config gigaChatIntegrationConfig) keyConfig() *schemas.GigaChatKeyConfig {
	return &schemas.GigaChatKeyConfig{
		AuthURL:      config.authURL,
		BaseURL:      config.baseURL,
		CertFile:     config.certFile,
		KeyFile:      config.keyFile,
		CABundleFile: config.caBundleFile,
	}
}

func newGigaChatIntegrationContext(t *testing.T) *schemas.BifrostContext {
	t.Helper()

	ctx, cancel := schemas.NewBifrostContextWithTimeout(context.Background(), gigaChatIntegrationTimeout)
	t.Cleanup(cancel)
	return ctx
}

func gigaChatIntegrationChatRequest() *schemas.BifrostChatRequest {
	maxTokens := 64
	text := "Reply with one short sentence."
	return &schemas.BifrostChatRequest{
		Model: gigaChatIntegrationChatModel,
		Input: []schemas.ChatMessage{{
			Role:    schemas.ChatMessageRoleUser,
			Content: &schemas.ChatMessageContent{ContentStr: &text},
		}},
		Params: &schemas.ChatParameters{
			MaxCompletionTokens: &maxTokens,
		},
	}
}

func gigaChatIntegrationEmbeddingRequest() *schemas.BifrostEmbeddingRequest {
	return &schemas.BifrostEmbeddingRequest{
		Model: gigaChatIntegrationEmbeddingModel,
		Input: &schemas.EmbeddingInput{Text: schemas.Ptr("integration test")},
	}
}

func gigaChatIntegrationResponsesRequest() *schemas.BifrostResponsesRequest {
	maxTokens := 64
	return &schemas.BifrostResponsesRequest{
		Model: gigaChatIntegrationChatModel,
		Input: []schemas.ResponsesMessage{{
			Role:    schemas.Ptr(schemas.ResponsesInputMessageRoleUser),
			Content: &schemas.ResponsesMessageContent{ContentStr: schemas.Ptr("Reply with one short sentence.")},
		}},
		Params: &schemas.ResponsesParameters{
			MaxOutputTokens: &maxTokens,
		},
	}
}

func gigaChatIntegrationToolRequest(t *testing.T) *schemas.BifrostChatRequest {
	t.Helper()

	request := testGigaChatChatToolRequest(t, "get_weather")
	request.Model = gigaChatIntegrationChatModel
	request.Input[0].Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr("Use the get_weather function for Moscow.")}
	request.Params.ToolChoice = &schemas.ChatToolChoice{
		ChatToolChoiceStruct: &schemas.ChatToolChoiceStruct{
			Type: schemas.ChatToolChoiceTypeFunction,
			Function: &schemas.ChatToolChoiceFunction{
				Name: "get_weather",
			},
		},
	}
	return request
}

func assertGigaChatIntegrationChatStream(t *testing.T, stream chan *schemas.BifrostStreamChunk) {
	t.Helper()

	if stream == nil {
		t.Fatal("chat completion stream is nil")
	}

	receivedResponse := false
	for chunk := range stream {
		if chunk == nil {
			continue
		}
		if chunk.BifrostError != nil {
			failGigaChatIntegrationBifrostError(t, "chat completion stream chunk", chunk.BifrostError)
		}
		if chunk.BifrostChatResponse != nil {
			receivedResponse = true
			if chunk.BifrostChatResponse.ExtraFields.Provider != schemas.GigaChat {
				t.Fatalf("provider mismatch: got %q, want %q", chunk.BifrostChatResponse.ExtraFields.Provider, schemas.GigaChat)
			}
		}
	}
	if !receivedResponse {
		t.Fatal("chat completion stream returned no response chunks")
	}
}

func assertGigaChatIntegrationResponsesStream(t *testing.T, stream chan *schemas.BifrostStreamChunk) {
	t.Helper()

	if stream == nil {
		t.Fatal("responses stream is nil")
	}

	receivedCompleted := false
	for chunk := range stream {
		if chunk == nil {
			continue
		}
		if chunk.BifrostError != nil {
			failGigaChatIntegrationBifrostError(t, "responses stream chunk", chunk.BifrostError)
		}
		if chunk.BifrostResponsesStreamResponse == nil {
			continue
		}
		if chunk.BifrostResponsesStreamResponse.Response != nil &&
			chunk.BifrostResponsesStreamResponse.Response.ExtraFields.Provider != schemas.GigaChat {
			t.Fatalf("provider mismatch: got %q, want %q", chunk.BifrostResponsesStreamResponse.Response.ExtraFields.Provider, schemas.GigaChat)
		}
		if chunk.BifrostResponsesStreamResponse.Type == schemas.ResponsesStreamResponseTypeCompleted {
			receivedCompleted = true
		}
	}
	if !receivedCompleted {
		t.Fatal("responses stream did not emit response.completed")
	}
}

func gigaChatIntegrationHasToolCall(response *schemas.BifrostChatResponse) bool {
	if response == nil {
		return false
	}
	for _, choice := range response.Choices {
		if choice.ChatNonStreamResponseChoice == nil || choice.ChatNonStreamResponseChoice.Message == nil {
			continue
		}
		message := choice.ChatNonStreamResponseChoice.Message
		if message.ChatAssistantMessage != nil && len(message.ChatAssistantMessage.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func failGigaChatIntegrationBifrostError(t *testing.T, operation string, bifrostErr *schemas.BifrostError) {
	t.Helper()

	if bifrostErr == nil {
		t.Fatalf("%s failed", operation)
	}
	failGigaChatIntegrationError(t, operation, errors.New(bifrostErr.String()))
}

func failGigaChatIntegrationError(t *testing.T, operation string, err error) {
	t.Helper()

	message := fmt.Sprintf("%s failed: %v", operation, err)
	t.Fatal(redactGigaChatIntegrationSecrets(message))
}

func redactGigaChatIntegrationSecrets(message string) string {
	for _, envName := range []string{
		"GIGACHAT_CREDENTIALS",
		"GIGACHAT_USER",
		"GIGACHAT_PASSWORD",
		"GIGACHAT_KEY_FILE_PASSWORD",
	} {
		if value := os.Getenv(envName); strings.TrimSpace(value) != "" {
			message = strings.ReplaceAll(message, value, "<redacted>")
		}
	}
	return message
}
