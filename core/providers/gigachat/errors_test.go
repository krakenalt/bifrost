package gigachat

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

func testGigaChatErrors(t *testing.T) {
	t.Parallel()

	t.Run("ParsesCommonPayloads", testGigaChatErrorParsesCommonPayloads)
	t.Run("ParsesOAuthPayloads", testGigaChatErrorParsesOAuthPayloads)
	t.Run("UsesFallbackForNonJSON", testGigaChatErrorUsesFallbackForNonJSON)
	t.Run("RedactsRawPayloads", testGigaChatErrorRedactsRawPayloads)
	t.Run("RedactsTextPayloads", testGigaChatErrorRedactsTextPayloads)
}

func testGigaChatErrorParsesCommonPayloads(t *testing.T) {
	t.Parallel()

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)
	resp.SetStatusCode(http.StatusTooManyRequests)
	resp.SetBodyString(`{"status":429,"code":7,"message":"quota exceeded"}`)

	bifrostErr := ParseGigaChatError(resp, schemas.GigaChat)
	if bifrostErr == nil {
		t.Fatal("expected error, got nil")
	}
	if bifrostErr.StatusCode == nil || *bifrostErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status mismatch: %#v", bifrostErr.StatusCode)
	}
	if bifrostErr.Error == nil || bifrostErr.Error.Message != "quota exceeded" {
		t.Fatalf("message mismatch: %#v", bifrostErr.Error)
	}
	if bifrostErr.Error.Code == nil || *bifrostErr.Error.Code != "7" {
		t.Fatalf("code mismatch: %#v", bifrostErr.Error)
	}
	if bifrostErr.ExtraFields.Provider != schemas.GigaChat {
		t.Fatalf("provider mismatch: got %q", bifrostErr.ExtraFields.Provider)
	}
}

func testGigaChatErrorParsesOAuthPayloads(t *testing.T) {
	t.Parallel()

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)
	resp.SetStatusCode(http.StatusUnauthorized)
	resp.SetBodyString(`{"error":"invalid_client","error_description":"bad credentials","code":"AUTH_FAILED"}`)

	bifrostErr := ParseGigaChatError(resp, schemas.GigaChat)
	if bifrostErr == nil {
		t.Fatal("expected error, got nil")
	}
	if bifrostErr.Error == nil || bifrostErr.Error.Message != "bad credentials" {
		t.Fatalf("message mismatch: %#v", bifrostErr.Error)
	}
	if bifrostErr.Error.Code == nil || *bifrostErr.Error.Code != "AUTH_FAILED" {
		t.Fatalf("code mismatch: %#v", bifrostErr.Error)
	}
}

func testGigaChatErrorUsesFallbackForNonJSON(t *testing.T) {
	t.Parallel()

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)
	resp.SetStatusCode(http.StatusBadGateway)
	resp.SetBodyString("upstream unavailable")

	bifrostErr := ParseGigaChatError(resp, schemas.GigaChat)
	if bifrostErr == nil {
		t.Fatal("expected error, got nil")
	}
	if bifrostErr.Error == nil || !strings.Contains(bifrostErr.Error.Message, "provider API error") {
		t.Fatalf("message mismatch: %#v", bifrostErr.Error)
	}
}

func testGigaChatErrorRedactsRawPayloads(t *testing.T) {
	t.Parallel()

	ctx := testBifrostContext()
	requestBody := []byte(`{"model":"GigaChat","credentials":"super-secret-credentials","nested":{"password":"super-secret-password"}}`)
	responseBody := []byte(`{"message":"bad","access_token":"super-secret-token","authorization":"Bearer super-secret-token"}`)
	bifrostErr := newGigaChatProviderResponseError("failed", nil)

	enriched := enrichGigaChatError(ctx, bifrostErr, requestBody, responseBody, true, true)
	output := stringifyGigaChatRaw(enriched.ExtraFields.RawRequest) + stringifyGigaChatRaw(enriched.ExtraFields.RawResponse)
	for _, secret := range []string{"super-secret-credentials", "super-secret-password", "super-secret-token"} {
		if strings.Contains(output, secret) {
			t.Fatalf("raw payload leaked %q in %s", secret, output)
		}
	}
	if !strings.Contains(output, "redacted") {
		t.Fatalf("expected redacted marker in raw payloads, got %s", output)
	}
}

func testGigaChatErrorRedactsTextPayloads(t *testing.T) {
	t.Parallel()

	payload := []byte(`error: Authorization Bearer super-secret-token failed; Basic super-secret-basic rejected`)
	redacted := string(redactGigaChatRawPayload(payload))
	for _, secret := range []string{"super-secret-token", "super-secret-basic"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("text payload leaked %q in %s", secret, redacted)
		}
	}
}

func stringifyGigaChatRaw(raw interface{}) string {
	if raw == nil {
		return ""
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(data)
}
