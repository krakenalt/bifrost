package gigachat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

const gigaChatOAuthRefreshLeeway = time.Minute

type gigaChatCachedToken struct {
	accessToken string
	expiresAt   time.Time
}

type gigaChatTokenCacheEntry struct {
	mu    sync.Mutex
	token gigaChatCachedToken
}

type gigaChatTokenCache struct {
	mu      sync.Mutex
	entries map[string]*gigaChatTokenCacheEntry
	now     func() time.Time
}

func newGigaChatTokenCache(now func() time.Time) *gigaChatTokenCache {
	if now == nil {
		now = time.Now
	}
	return &gigaChatTokenCache{
		entries: make(map[string]*gigaChatTokenCacheEntry),
		now:     now,
	}
}

func (provider *GigaChatProvider) getOAuthAccessToken(ctx *schemas.BifrostContext, key schemas.Key) (string, *schemas.BifrostError) {
	authConfig, bifrostErr := resolveGigaChatOAuthConfig(key)
	if bifrostErr != nil {
		return "", bifrostErr
	}

	cacheKey := buildGigaChatOAuthCacheKey(authConfig)
	entry := provider.tokenCache.getEntry(cacheKey)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.token.isValid(provider.tokenCache.now().Add(gigaChatOAuthRefreshLeeway)) {
		return entry.token.accessToken, nil
	}

	token, bifrostErr := provider.requestGigaChatOAuthToken(ctx, authConfig)
	if bifrostErr != nil {
		return "", bifrostErr
	}
	entry.token = token
	return token.accessToken, nil
}

func (cache *gigaChatTokenCache) getEntry(cacheKey string) *gigaChatTokenCacheEntry {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	entry := cache.entries[cacheKey]
	if entry == nil {
		entry = &gigaChatTokenCacheEntry{}
		cache.entries[cacheKey] = entry
	}
	return entry
}

func (token gigaChatCachedToken) isValid(validAfter time.Time) bool {
	return token.accessToken != "" && token.expiresAt.After(validAfter)
}

type gigaChatOAuthConfig struct {
	authURL     string
	credentials string
	scope       string
}

func resolveGigaChatOAuthConfig(key schemas.Key) (gigaChatOAuthConfig, *schemas.BifrostError) {
	keyConfig := key.GigaChatKeyConfig
	if keyConfig == nil || !keyConfig.Credentials.IsSet() {
		return gigaChatOAuthConfig{}, newGigaChatConfigurationError("gigachat_key_config.credentials is required for OAuth token exchange")
	}

	credentials := strings.TrimSpace(keyConfig.Credentials.GetValue())
	if credentials == "" {
		return gigaChatOAuthConfig{}, newGigaChatConfigurationError("gigachat_key_config.credentials resolved to an empty value")
	}

	scope := strings.TrimSpace(keyConfig.Scope)
	if scope == "" {
		scope = schemas.DefaultGigaChatScope
	}

	return gigaChatOAuthConfig{
		authURL:     resolveAuthURL(key),
		credentials: credentials,
		scope:       scope,
	}, nil
}

func buildGigaChatOAuthCacheKey(authConfig gigaChatOAuthConfig) string {
	hash := sha256.New()
	hash.Write([]byte(authConfig.authURL))
	hash.Write([]byte{0})
	hash.Write([]byte(authConfig.scope))
	hash.Write([]byte{0})
	hash.Write([]byte(authConfig.credentials))
	return hex.EncodeToString(hash.Sum(nil))
}

func (provider *GigaChatProvider) requestGigaChatOAuthToken(ctx *schemas.BifrostContext, authConfig gigaChatOAuthConfig) (gigaChatCachedToken, *schemas.BifrostError) {
	if ctx == nil {
		ctx = schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	form := url.Values{}
	form.Set("scope", authConfig.scope)

	req.SetRequestURI(authConfig.authURL)
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("RqUID", uuid.NewString())
	req.Header.Set("Authorization", "Basic "+authConfig.credentials)
	req.SetBodyString(form.Encode())

	_, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, provider.client, req, resp)
	defer wait()
	if bifrostErr != nil {
		bifrostErr.ExtraFields.Provider = provider.GetProviderKey()
		return gigaChatCachedToken{}, bifrostErr
	}

	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return gigaChatCachedToken{}, ParseGigaChatError(resp, provider.GetProviderKey())
	}

	body, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		return gigaChatCachedToken{}, newGigaChatProviderResponseError("failed to decode GigaChat token response", err)
	}

	var tokenResponse GigaChatTokenResponse
	if err := sonic.Unmarshal(body, &tokenResponse); err != nil {
		return gigaChatCachedToken{}, newGigaChatProviderResponseError("failed to parse GigaChat token response", err)
	}
	if strings.TrimSpace(tokenResponse.AccessToken) == "" {
		return gigaChatCachedToken{}, newGigaChatProviderResponseError("GigaChat token response missing access_token", nil)
	}
	if tokenResponse.ExpiresAt <= 0 {
		return gigaChatCachedToken{}, newGigaChatProviderResponseError("GigaChat token response missing expires_at", nil)
	}

	expiresAt := time.Unix(tokenResponse.ExpiresAt, 0)
	if !expiresAt.After(provider.tokenCache.now()) {
		return gigaChatCachedToken{}, newGigaChatProviderResponseError("GigaChat token response is already expired", nil)
	}

	return gigaChatCachedToken{
		accessToken: tokenResponse.AccessToken,
		expiresAt:   expiresAt,
	}, nil
}

func newGigaChatConfigurationError(message string) *schemas.BifrostError {
	bifrostErr := providerUtils.NewConfigurationError(message)
	bifrostErr.ExtraFields.Provider = schemas.GigaChat
	return bifrostErr
}

func newGigaChatProviderResponseError(message string, err error) *schemas.BifrostError {
	statusCode := http.StatusBadGateway
	bifrostErr := &schemas.BifrostError{
		IsBifrostError: false,
		StatusCode:     &statusCode,
		Error: &schemas.ErrorField{
			Message: message,
			Error:   err,
		},
		ExtraFields: schemas.BifrostErrorExtraFields{
			Provider: schemas.GigaChat,
		},
	}
	if err != nil {
		bifrostErr.Error.Message = fmt.Sprintf("%s: %v", message, err)
	}
	return bifrostErr
}
