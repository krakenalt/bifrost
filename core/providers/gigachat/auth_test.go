package gigachat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestGigaChatOAuthTokenClient(t *testing.T) {
	t.Parallel()

	t.Run("RequestShapeAndDefaultScope", testGigaChatOAuthRequestShapeAndDefaultScope)
	t.Run("CachesTokenBeforeLeeway", testGigaChatOAuthCachesTokenBeforeLeeway)
	t.Run("RefreshesTokenInsideLeeway", testGigaChatOAuthRefreshesTokenInsideLeeway)
	t.Run("HandlesProviderErrors", testGigaChatOAuthHandlesProviderErrors)
	t.Run("HandlesMalformedResponses", testGigaChatOAuthHandlesMalformedResponses)
	t.Run("MissingCredentials", testGigaChatOAuthMissingCredentials)
	t.Run("ContextCancellation", testGigaChatOAuthContextCancellation)
}

func testGigaChatOAuthRequestShapeAndDefaultScope(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method mismatch: got %s", r.Method)
		}
		if r.URL.Path != "/api/v2/oauth" {
			t.Errorf("path mismatch: got %s", r.URL.Path)
		}
		if contentType := r.Header.Get("Content-Type"); !strings.Contains(contentType, "application/x-www-form-urlencoded") {
			t.Errorf("content type mismatch: got %q", contentType)
		}
		if accept := r.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("accept mismatch: got %q", accept)
		}
		if auth := r.Header.Get("Authorization"); auth != "Basic test-credentials" {
			t.Errorf("authorization mismatch: got %q", auth)
		}
		requestID := r.Header.Get("RqUID")
		parsedRequestID, err := uuid.Parse(requestID)
		if err != nil {
			t.Errorf("RqUID is not a UUID: %q", requestID)
		} else if parsedRequestID.Version() != 4 {
			t.Errorf("RqUID version mismatch: got %d", parsedRequestID.Version())
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}
		if scope := values.Get("scope"); scope != schemas.DefaultGigaChatScope {
			t.Errorf("scope mismatch: got %q, want %q", scope, schemas.DefaultGigaChatScope)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token-1","expires_at":` + formatUnix(now.Add(30*time.Minute)) + `}`))
	}))
	defer server.Close()

	provider := newTestGigaChatProvider(t, func() time.Time { return now })
	token, bifrostErr := provider.getOAuthAccessToken(testBifrostContext(), testGigaChatOAuthKey(server.URL+"/api/v2/oauth", "", "test-credentials"))
	if bifrostErr != nil {
		t.Fatalf("getOAuthAccessToken returned error: %v", bifrostErr)
	}
	if token != "token-1" {
		t.Fatalf("token mismatch: got %q", token)
	}
}

func testGigaChatOAuthCachesTokenBeforeLeeway(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token-` + formatInt32(count) + `","expires_at":` + formatUnix(now.Add(30*time.Minute)) + `}`))
	}))
	defer server.Close()

	provider := newTestGigaChatProvider(t, func() time.Time { return now })
	key := testGigaChatOAuthKey(server.URL, "", "test-credentials")

	firstToken, bifrostErr := provider.getOAuthAccessToken(testBifrostContext(), key)
	if bifrostErr != nil {
		t.Fatalf("first getOAuthAccessToken returned error: %v", bifrostErr)
	}
	secondToken, bifrostErr := provider.getOAuthAccessToken(testBifrostContext(), key)
	if bifrostErr != nil {
		t.Fatalf("second getOAuthAccessToken returned error: %v", bifrostErr)
	}

	if firstToken != "token-1" || secondToken != "token-1" {
		t.Fatalf("cached token mismatch: first=%q second=%q", firstToken, secondToken)
	}
	if requestCount.Load() != 1 {
		t.Fatalf("request count mismatch: got %d, want 1", requestCount.Load())
	}
}

func testGigaChatOAuthRefreshesTokenInsideLeeway(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token-` + formatInt32(count) + `","expires_at":` + formatUnix(now.Add(30*time.Minute)) + `}`))
	}))
	defer server.Close()

	provider := newTestGigaChatProvider(t, func() time.Time { return now })
	key := testGigaChatOAuthKey(server.URL, "", "test-credentials")

	firstToken, bifrostErr := provider.getOAuthAccessToken(testBifrostContext(), key)
	if bifrostErr != nil {
		t.Fatalf("first getOAuthAccessToken returned error: %v", bifrostErr)
	}
	now = now.Add(29*time.Minute + time.Second)
	secondToken, bifrostErr := provider.getOAuthAccessToken(testBifrostContext(), key)
	if bifrostErr != nil {
		t.Fatalf("second getOAuthAccessToken returned error: %v", bifrostErr)
	}

	if firstToken != "token-1" || secondToken != "token-2" {
		t.Fatalf("token refresh mismatch: first=%q second=%q", firstToken, secondToken)
	}
	if requestCount.Load() != 2 {
		t.Fatalf("request count mismatch: got %d, want 2", requestCount.Load())
	}
}

func testGigaChatOAuthHandlesProviderErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		statusCode  int
		body        string
		wantMessage string
		wantCode    string
	}{
		{
			name:        "bad request code shape",
			statusCode:  http.StatusBadRequest,
			body:        `{"code":5,"message":"scope is empty"}`,
			wantMessage: "scope is empty",
			wantCode:    "5",
		},
		{
			name:        "unauthorized code shape",
			statusCode:  http.StatusUnauthorized,
			body:        `{"code":4,"message":"Can't decode 'Authorization' header"}`,
			wantMessage: "Can't decode 'Authorization' header",
			wantCode:    "4",
		},
		{
			name:        "server status shape",
			statusCode:  http.StatusInternalServerError,
			body:        `{"status":500,"message":"Internal Server Error"}`,
			wantMessage: "Internal Server Error",
			wantCode:    "500",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(testCase.statusCode)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()

			provider := newTestGigaChatProvider(t, time.Now)
			_, bifrostErr := provider.getOAuthAccessToken(testBifrostContext(), testGigaChatOAuthKey(server.URL, "", "super-secret-credentials"))
			if bifrostErr == nil {
				t.Fatal("expected error, got nil")
			}
			if bifrostErr.Error == nil {
				t.Fatal("expected error field, got nil")
			}
			if bifrostErr.Error.Message != testCase.wantMessage {
				t.Fatalf("message mismatch: got %q, want %q", bifrostErr.Error.Message, testCase.wantMessage)
			}
			if bifrostErr.Error.Code == nil || *bifrostErr.Error.Code != testCase.wantCode {
				t.Fatalf("code mismatch: got %#v, want %q", bifrostErr.Error.Code, testCase.wantCode)
			}
			assertNoGigaChatSecretLeak(t, bifrostErr.String())
		})
	}
}

func testGigaChatOAuthHandlesMalformedResponses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `not-json`},
		{name: "missing token", body: `{"expires_at":1893456000}`},
		{name: "missing expiry", body: `{"access_token":"token-1"}`},
		{name: "expired token", body: `{"access_token":"token-1","expires_at":1}`},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()

			provider := newTestGigaChatProvider(t, func() time.Time { return time.Unix(100, 0) })
			_, bifrostErr := provider.getOAuthAccessToken(testBifrostContext(), testGigaChatOAuthKey(server.URL, "", "super-secret-credentials"))
			if bifrostErr == nil {
				t.Fatal("expected error, got nil")
			}
			assertNoGigaChatSecretLeak(t, bifrostErr.String())
		})
	}
}

func testGigaChatOAuthMissingCredentials(t *testing.T) {
	t.Parallel()

	provider := newTestGigaChatProvider(t, time.Now)
	_, bifrostErr := provider.getOAuthAccessToken(testBifrostContext(), schemas.Key{
		GigaChatKeyConfig: &schemas.GigaChatKeyConfig{},
	})
	if bifrostErr == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(bifrostErr.GetErrorString(), "credentials") {
		t.Fatalf("unexpected error: %v", bifrostErr)
	}

	_, bifrostErr = provider.getOAuthAccessToken(testBifrostContext(), schemas.Key{
		GigaChatKeyConfig: &schemas.GigaChatKeyConfig{
			Credentials: schemas.NewEnvVar("env.MISSING_GIGACHAT_CREDENTIALS_FOR_TEST"),
		},
	})
	if bifrostErr == nil {
		t.Fatal("expected unresolved env error, got nil")
	}
	if !strings.Contains(bifrostErr.GetErrorString(), "empty value") {
		t.Fatalf("unexpected unresolved env error: %v", bifrostErr)
	}
}

func testGigaChatOAuthContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token-1","expires_at":1893456000}`))
	}))
	defer server.Close()

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()

	provider := newTestGigaChatProvider(t, time.Now)
	_, bifrostErr := provider.getOAuthAccessToken(schemas.NewBifrostContext(cancelledContext, schemas.NoDeadline), testGigaChatOAuthKey(server.URL, "", "test-credentials"))
	if bifrostErr == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if bifrostErr.Error == nil || bifrostErr.Error.Type == nil || *bifrostErr.Error.Type != schemas.RequestCancelled {
		t.Fatalf("unexpected cancellation error: %v", bifrostErr)
	}
}

func newTestGigaChatProvider(t *testing.T, now func() time.Time) *GigaChatProvider {
	t.Helper()

	provider, err := NewGigaChatProvider(&schemas.ProviderConfig{}, nil)
	if err != nil {
		t.Fatalf("NewGigaChatProvider returned error: %v", err)
	}
	provider.tokenCache = newGigaChatTokenCache(now)
	return provider
}

func testGigaChatOAuthKey(authURL string, scope string, credentials string) schemas.Key {
	return schemas.Key{
		GigaChatKeyConfig: &schemas.GigaChatKeyConfig{
			Credentials: schemas.NewEnvVar(credentials),
			Scope:       scope,
			AuthURL:     authURL,
		},
	}
}

func testBifrostContext() *schemas.BifrostContext {
	return schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
}

func assertNoGigaChatSecretLeak(t *testing.T, output string) {
	t.Helper()
	for _, secret := range []string{"super-secret-credentials", "test-credentials"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret %q leaked in %s", secret, output)
		}
	}
}

func formatUnix(value time.Time) string {
	return formatInt64(value.Unix())
}

func formatInt32(value int32) string {
	return formatInt64(int64(value))
}

func formatInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}
