package gigachat

import (
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestGigachat(t *testing.T) {
	t.Parallel()

	t.Run("NewProvider", testNewGigaChatProvider)
	t.Run("TrimBaseURL", testNewGigaChatProviderTrimsBaseURL)
	t.Run("UnsupportedOperation", testGigaChatProviderUnsupportedOperation)
}

func testNewGigaChatProvider(t *testing.T) {
	t.Parallel()

	provider, err := NewGigaChatProvider(&schemas.ProviderConfig{}, nil)
	if err != nil {
		t.Fatalf("NewGigaChatProvider returned error: %v", err)
	}
	if provider.GetProviderKey() != schemas.GigaChat {
		t.Fatalf("provider key mismatch: got %q, want %q", provider.GetProviderKey(), schemas.GigaChat)
	}
	if provider.networkConfig.BaseURL != gigaChatDefaultBaseURL {
		t.Fatalf("base URL mismatch: got %q, want %q", provider.networkConfig.BaseURL, gigaChatDefaultBaseURL)
	}
	if provider.client == nil {
		t.Fatal("client is nil")
	}
	if provider.streamingClient == nil {
		t.Fatal("streaming client is nil")
	}
}

func testNewGigaChatProviderTrimsBaseURL(t *testing.T) {
	t.Parallel()

	provider, err := NewGigaChatProvider(&schemas.ProviderConfig{
		NetworkConfig: schemas.NetworkConfig{
			BaseURL: "https://api.giga.chat/v1/",
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewGigaChatProvider returned error: %v", err)
	}

	if provider.networkConfig.BaseURL != "https://api.giga.chat/v1" {
		t.Fatalf("base URL mismatch: got %q", provider.networkConfig.BaseURL)
	}
}

func testGigaChatProviderUnsupportedOperation(t *testing.T) {
	t.Parallel()

	provider, err := NewGigaChatProvider(&schemas.ProviderConfig{}, nil)
	if err != nil {
		t.Fatalf("NewGigaChatProvider returned error: %v", err)
	}

	response, bifrostErr := provider.ChatCompletion(nil, schemas.Key{}, &schemas.BifrostChatRequest{})
	if response != nil {
		t.Fatalf("expected nil response, got %#v", response)
	}
	if bifrostErr == nil {
		t.Fatal("expected unsupported operation error, got nil")
	}
	if bifrostErr.Error == nil || bifrostErr.Error.Code == nil || *bifrostErr.Error.Code != "unsupported_operation" {
		t.Fatalf("unexpected error code: %#v", bifrostErr.Error)
	}
	if bifrostErr.ExtraFields.Provider != schemas.GigaChat {
		t.Fatalf("provider mismatch: got %q, want %q", bifrostErr.ExtraFields.Provider, schemas.GigaChat)
	}
	if bifrostErr.ExtraFields.RequestType != schemas.ChatCompletionRequest {
		t.Fatalf("request type mismatch: got %q, want %q", bifrostErr.ExtraFields.RequestType, schemas.ChatCompletionRequest)
	}
	if !strings.Contains(bifrostErr.Error.Message, "gigachat provider") {
		t.Fatalf("unexpected error message: %q", bifrostErr.Error.Message)
	}
}
