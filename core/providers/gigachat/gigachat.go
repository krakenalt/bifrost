package gigachat

import (
	"context"
	"net/http"
	"strings"
	"time"

	openaiProvider "github.com/maximhq/bifrost/core/providers/openai"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

var _ schemas.Provider = (*GigaChatProvider)(nil)

// GigaChatProvider implements the Provider interface for GigaChat's API.
type GigaChatProvider struct {
	logger               schemas.Logger
	client               *fasthttp.Client
	streamingClient      *fasthttp.Client
	networkConfig        schemas.NetworkConfig
	sendBackRawRequest   bool
	sendBackRawResponse  bool
	customProviderConfig *schemas.CustomProviderConfig
	tokenCache           *gigaChatTokenCache
}

// NewGigaChatProvider creates a new GigaChat provider instance.
func NewGigaChatProvider(config *schemas.ProviderConfig, logger schemas.Logger) (*GigaChatProvider, error) {
	config.CheckAndSetDefaults()

	requestTimeout := time.Second * time.Duration(config.NetworkConfig.DefaultRequestTimeoutInSeconds)
	client := &fasthttp.Client{
		ReadTimeout:         requestTimeout,
		WriteTimeout:        requestTimeout,
		MaxConnsPerHost:     config.NetworkConfig.MaxConnsPerHost,
		MaxIdleConnDuration: 30 * time.Second,
		MaxConnWaitTimeout:  requestTimeout,
		MaxConnDuration:     time.Second * time.Duration(schemas.DefaultMaxConnDurationInSeconds),
		ConnPoolStrategy:    fasthttp.FIFO,
	}

	client = providerUtils.ConfigureProxy(client, config.ProxyConfig, logger)
	client = providerUtils.ConfigureDialer(client)
	client = providerUtils.ConfigureTLS(client, config.NetworkConfig, logger)
	streamingClient := providerUtils.BuildStreamingClient(client)

	if config.NetworkConfig.BaseURL == "" {
		config.NetworkConfig.BaseURL = gigaChatDefaultBaseURL
	}
	config.NetworkConfig.BaseURL = strings.TrimRight(config.NetworkConfig.BaseURL, "/")

	return &GigaChatProvider{
		logger:               logger,
		client:               client,
		streamingClient:      streamingClient,
		networkConfig:        config.NetworkConfig,
		sendBackRawRequest:   config.SendBackRawRequest,
		sendBackRawResponse:  config.SendBackRawResponse,
		customProviderConfig: config.CustomProviderConfig,
		tokenCache:           newGigaChatTokenCache(time.Now),
	}, nil
}

// GetProviderKey returns the provider identifier for GigaChat.
func (provider *GigaChatProvider) GetProviderKey() schemas.ModelProvider {
	return providerUtils.GetProviderName(schemas.GigaChat, provider.customProviderConfig)
}

func (provider *GigaChatProvider) chatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest, forceRefresh bool) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)
	if request == nil {
		return nil, providerUtils.NewBifrostOperationError("chat completion request is nil", nil)
	}

	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToGigaChatChatRequest(ctx, request)
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	headers, bifrostErr := provider.buildAuthHeaders(ctx, key)
	if forceRefresh {
		headers, bifrostErr = provider.refreshAuthHeaders(ctx, key)
	}
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	client, clientErr := buildGigaChatTLSClient(provider.client, key.GigaChatKeyConfig)
	if clientErr != nil {
		return nil, newGigaChatConfigurationError(clientErr.Error())
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	for headerName, headerValue := range headers {
		req.Header.Set(headerName, headerValue)
	}
	req.SetRequestURI(buildGigaChatRequestURL(ctx, resolveBaseURL(key, provider.networkConfig), gigaChatAPIVersionV1, "/chat/completions", provider.customProviderConfig, schemas.ChatCompletionRequest))
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBody(jsonData)

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, client, req, resp)
	defer wait()
	if bifrostErr != nil {
		bifrostErr.ExtraFields.Provider = provider.GetProviderKey()
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, sendBackRawRequest, sendBackRawResponse)
	}

	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)

	if resp.StatusCode() != fasthttp.StatusOK {
		bifrostErr := ParseGigaChatError(resp, provider.GetProviderKey())
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, sendBackRawRequest, sendBackRawResponse)
	}

	responseBody, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		bifrostErr := newGigaChatProviderResponseError("failed to decode GigaChat chat completion response", err)
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	gigaChatResponse := &GigaChatChatResponse{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, gigaChatResponse, jsonData, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, responseBody, sendBackRawRequest, sendBackRawResponse)
	}

	response := ToBifrostChatResponse(provider.GetProviderKey(), gigaChatResponse)
	if response == nil {
		return nil, newGigaChatProviderResponseError("GigaChat chat completion response is empty", nil)
	}
	response.BackfillParams(request)
	response.ExtraFields.Latency = latency.Milliseconds()
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}

	return response, nil
}

func (provider *GigaChatProvider) chatCompletionStream(
	ctx *schemas.BifrostContext,
	postHookRunner schemas.PostHookRunner,
	postHookSpanFinalizer func(context.Context),
	key schemas.Key,
	request *schemas.BifrostChatRequest,
	forceRefresh bool,
) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)

	headers, bifrostErr := provider.buildAuthHeaders(ctx, key)
	if forceRefresh {
		headers, bifrostErr = provider.refreshAuthHeaders(ctx, key)
	}
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	client, clientErr := buildGigaChatTLSClient(provider.streamingClient, key.GigaChatKeyConfig)
	if clientErr != nil {
		return nil, newGigaChatConfigurationError(clientErr.Error())
	}

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)
	providerName := provider.GetProviderKey()

	return openaiProvider.HandleOpenAIChatCompletionStreaming(
		ctx,
		client,
		buildGigaChatRequestURL(ctx, resolveBaseURL(key, provider.networkConfig), gigaChatAPIVersionV1, "/chat/completions", provider.customProviderConfig, schemas.ChatCompletionStreamRequest),
		request,
		headers,
		nil,
		provider.networkConfig.StreamIdleTimeoutInSeconds,
		sendBackRawRequest,
		sendBackRawResponse,
		providerName,
		postHookRunner,
		func(request *schemas.BifrostChatRequest) (providerUtils.RequestBodyWithExtraParams, error) {
			return ToGigaChatChatStreamRequest(ctx, request)
		},
		handleGigaChatChatStreamResponse(providerName),
		func(resp *fasthttp.Response) *schemas.BifrostError {
			return ParseGigaChatError(resp, providerName)
		},
		nil,
		withGigaChatChatResponseProvider(providerName),
		provider.logger,
		postHookSpanFinalizer,
	)
}

func ensureGigaChatContext(ctx *schemas.BifrostContext) *schemas.BifrostContext {
	if ctx != nil {
		return ctx
	}
	return schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
}

func isGigaChatUnauthorizedError(bifrostErr *schemas.BifrostError) bool {
	return bifrostErr != nil && bifrostErr.StatusCode != nil && *bifrostErr.StatusCode == http.StatusUnauthorized
}

func (provider *GigaChatProvider) unsupported(requestType schemas.RequestType) *schemas.BifrostError {
	return providerUtils.NewUnsupportedOperationError(requestType, provider.GetProviderKey())
}

func (provider *GigaChatProvider) listModelsByKey(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	response, bifrostErr := provider.listModelsByKeyWithRefresh(ctx, key, request, false)
	if isGigaChatUnauthorizedError(bifrostErr) {
		return provider.listModelsByKeyWithRefresh(ctx, key, request, true)
	}
	return response, bifrostErr
}

func (provider *GigaChatProvider) listModelsByKeyWithRefresh(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostListModelsRequest, forceRefresh bool) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)

	headers, bifrostErr := provider.buildAuthHeaders(ctx, key)
	if forceRefresh {
		headers, bifrostErr = provider.refreshAuthHeaders(ctx, key)
	}
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	client, clientErr := buildGigaChatTLSClient(provider.client, key.GigaChatKeyConfig)
	if clientErr != nil {
		return nil, newGigaChatConfigurationError(clientErr.Error())
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	for headerName, headerValue := range headers {
		req.Header.Set(headerName, headerValue)
	}
	req.SetRequestURI(buildGigaChatRequestURL(ctx, resolveBaseURL(key, provider.networkConfig), gigaChatAPIVersionV1, "/models", provider.customProviderConfig, schemas.ListModelsRequest))
	req.Header.SetMethod(http.MethodGet)
	req.Header.SetContentType("application/json")
	req.Header.Set("Accept", "application/json")

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, client, req, resp)
	defer wait()
	if bifrostErr != nil {
		bifrostErr.ExtraFields.Provider = provider.GetProviderKey()
		return nil, providerUtils.EnrichError(ctx, bifrostErr, nil, nil, sendBackRawRequest, sendBackRawResponse)
	}

	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)

	if resp.StatusCode() != fasthttp.StatusOK {
		bifrostErr := ParseGigaChatError(resp, provider.GetProviderKey())
		return nil, providerUtils.EnrichError(ctx, bifrostErr, nil, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	responseBody, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		bifrostErr := newGigaChatProviderResponseError("failed to decode GigaChat models response", err)
		return nil, providerUtils.EnrichError(ctx, bifrostErr, nil, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	gigaChatResponse := &GigaChatListModelsResponse{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, gigaChatResponse, nil, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, nil, responseBody, sendBackRawRequest, sendBackRawResponse)
	}

	response := gigaChatResponse.ToBifrostListModelsResponse(provider.GetProviderKey(), key.Models, key.BlacklistedModels, key.Aliases, request.Unfiltered)
	if response == nil {
		return nil, newGigaChatProviderResponseError("GigaChat models response is empty", nil)
	}
	response.ExtraFields.Latency = latency.Milliseconds()
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}

	return response, nil
}

func (provider *GigaChatProvider) embeddingWithRefresh(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostEmbeddingRequest, forceRefresh bool) (*schemas.BifrostEmbeddingResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)
	if request == nil {
		return nil, providerUtils.NewBifrostOperationError("embedding request is nil", nil)
	}

	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToGigaChatEmbeddingRequest(request)
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	headers, bifrostErr := provider.buildAuthHeaders(ctx, key)
	if forceRefresh {
		headers, bifrostErr = provider.refreshAuthHeaders(ctx, key)
	}
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	client, clientErr := buildGigaChatTLSClient(provider.client, key.GigaChatKeyConfig)
	if clientErr != nil {
		return nil, newGigaChatConfigurationError(clientErr.Error())
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	for headerName, headerValue := range headers {
		req.Header.Set(headerName, headerValue)
	}
	req.SetRequestURI(buildGigaChatRequestURL(ctx, resolveBaseURL(key, provider.networkConfig), gigaChatAPIVersionV1, "/embeddings", provider.customProviderConfig, schemas.EmbeddingRequest))
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBody(jsonData)

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, client, req, resp)
	defer wait()
	if bifrostErr != nil {
		bifrostErr.ExtraFields.Provider = provider.GetProviderKey()
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, sendBackRawRequest, sendBackRawResponse)
	}

	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)

	if resp.StatusCode() != fasthttp.StatusOK {
		bifrostErr := ParseGigaChatError(resp, provider.GetProviderKey())
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	responseBody, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		bifrostErr := newGigaChatProviderResponseError("failed to decode GigaChat embeddings response", err)
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	gigaChatResponse := &GigaChatEmbeddingResponse{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, gigaChatResponse, jsonData, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, responseBody, sendBackRawRequest, sendBackRawResponse)
	}

	response := ToBifrostEmbeddingResponse(provider.GetProviderKey(), gigaChatResponse)
	if response == nil {
		return nil, newGigaChatProviderResponseError("GigaChat embeddings response is empty", nil)
	}
	response.BackfillParams(request)
	response.ExtraFields.Latency = latency.Milliseconds()
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}

	return response, nil
}

func (provider *GigaChatProvider) responsesWithRefresh(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest, forceRefresh bool) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)
	if request == nil {
		return nil, providerUtils.NewBifrostOperationError("responses request is nil", nil)
	}

	jsonData, bifrostErr := providerUtils.CheckContextAndGetRequestBody(
		ctx,
		request,
		func() (providerUtils.RequestBodyWithExtraParams, error) {
			return ToGigaChatResponsesRequest(request)
		})
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	headers, bifrostErr := provider.buildAuthHeaders(ctx, key)
	if forceRefresh {
		headers, bifrostErr = provider.refreshAuthHeaders(ctx, key)
	}
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	client, clientErr := buildGigaChatTLSClient(provider.client, key.GigaChatKeyConfig)
	if clientErr != nil {
		return nil, newGigaChatConfigurationError(clientErr.Error())
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	for headerName, headerValue := range headers {
		req.Header.Set(headerName, headerValue)
	}
	req.SetRequestURI(buildGigaChatRequestURL(ctx, resolveBaseURL(key, provider.networkConfig), gigaChatAPIVersionV2, "/chat/completions", provider.customProviderConfig, schemas.ResponsesRequest))
	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBody(jsonData)

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, client, req, resp)
	defer wait()
	if bifrostErr != nil {
		bifrostErr.ExtraFields.Provider = provider.GetProviderKey()
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, nil, sendBackRawRequest, sendBackRawResponse)
	}

	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)

	if resp.StatusCode() != fasthttp.StatusOK {
		bifrostErr := ParseGigaChatError(resp, provider.GetProviderKey())
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	responseBody, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		bifrostErr := newGigaChatProviderResponseError("failed to decode GigaChat Responses response", err)
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	gigaChatResponse := &GigaChatResponsesResponse{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, gigaChatResponse, jsonData, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, providerUtils.EnrichError(ctx, bifrostErr, jsonData, responseBody, sendBackRawRequest, sendBackRawResponse)
	}

	response := ToBifrostResponsesResponse(provider.GetProviderKey(), gigaChatResponse)
	if response == nil {
		return nil, newGigaChatProviderResponseError("GigaChat Responses response is empty", nil)
	}
	response.BackfillParams(request)
	response.ExtraFields.Latency = latency.Milliseconds()
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}

	return response, nil
}

// ListModels performs a v1 models request to GigaChat.
func (provider *GigaChatProvider) ListModels(ctx *schemas.BifrostContext, keys []schemas.Key, request *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.GigaChat, provider.customProviderConfig, schemas.ListModelsRequest); err != nil {
		return nil, err
	}
	if request == nil {
		request = &schemas.BifrostListModelsRequest{Provider: provider.GetProviderKey()}
	} else if request.Provider == "" {
		requestCopy := *request
		requestCopy.Provider = provider.GetProviderKey()
		request = &requestCopy
	}
	if len(keys) == 0 {
		return providerUtils.HandleKeylessListModelsRequest(provider.GetProviderKey(), func() (*schemas.BifrostListModelsResponse, *schemas.BifrostError) {
			return provider.listModelsByKey(ctx, schemas.Key{}, request)
		})
	}
	return providerUtils.HandleMultipleListModelsRequests(ctx, keys, request, provider.listModelsByKey)
}

// TextCompletion is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) TextCompletion(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostTextCompletionRequest) (*schemas.BifrostTextCompletionResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.TextCompletionRequest)
}

// TextCompletionStream is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) TextCompletionStream(_ *schemas.BifrostContext, _ schemas.PostHookRunner, _ func(context.Context), _ schemas.Key, _ *schemas.BifrostTextCompletionRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.TextCompletionStreamRequest)
}

// ChatCompletion sends a non-streaming v1 chat completions request to GigaChat.
func (provider *GigaChatProvider) ChatCompletion(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.GigaChat, provider.customProviderConfig, schemas.ChatCompletionRequest); err != nil {
		return nil, err
	}

	response, bifrostErr := provider.chatCompletion(ctx, key, request, false)
	if isGigaChatUnauthorizedError(bifrostErr) {
		return provider.chatCompletion(ctx, key, request, true)
	}
	return response, bifrostErr
}

// ChatCompletionStream sends a streaming v1 chat completions request to GigaChat.
func (provider *GigaChatProvider) ChatCompletionStream(ctx *schemas.BifrostContext, postHookRunner schemas.PostHookRunner, postHookSpanFinalizer func(context.Context), key schemas.Key, request *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.GigaChat, provider.customProviderConfig, schemas.ChatCompletionStreamRequest); err != nil {
		return nil, err
	}

	responseChan, bifrostErr := provider.chatCompletionStream(ctx, postHookRunner, postHookSpanFinalizer, key, request, false)
	if isGigaChatUnauthorizedError(bifrostErr) {
		return provider.chatCompletionStream(ctx, postHookRunner, postHookSpanFinalizer, key, request, true)
	}
	return responseChan, bifrostErr
}

// Responses sends a non-streaming v2 chat completions request to GigaChat.
func (provider *GigaChatProvider) Responses(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostResponsesRequest) (*schemas.BifrostResponsesResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.GigaChat, provider.customProviderConfig, schemas.ResponsesRequest); err != nil {
		return nil, err
	}

	response, bifrostErr := provider.responsesWithRefresh(ctx, key, request, false)
	if isGigaChatUnauthorizedError(bifrostErr) {
		return provider.responsesWithRefresh(ctx, key, request, true)
	}
	return response, bifrostErr
}

// ResponsesStream is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ResponsesStream(_ *schemas.BifrostContext, _ schemas.PostHookRunner, _ func(context.Context), _ schemas.Key, _ *schemas.BifrostResponsesRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ResponsesStreamRequest)
}

// CountTokens is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) CountTokens(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostResponsesRequest) (*schemas.BifrostCountTokensResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.CountTokensRequest)
}

// Embedding sends a non-streaming v1 embeddings request to GigaChat.
func (provider *GigaChatProvider) Embedding(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostEmbeddingRequest) (*schemas.BifrostEmbeddingResponse, *schemas.BifrostError) {
	if err := providerUtils.CheckOperationAllowed(schemas.GigaChat, provider.customProviderConfig, schemas.EmbeddingRequest); err != nil {
		return nil, err
	}

	response, bifrostErr := provider.embeddingWithRefresh(ctx, key, request, false)
	if isGigaChatUnauthorizedError(bifrostErr) {
		return provider.embeddingWithRefresh(ctx, key, request, true)
	}
	return response, bifrostErr
}

// Rerank is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) Rerank(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostRerankRequest) (*schemas.BifrostRerankResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.RerankRequest)
}

// OCR is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) OCR(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostOCRRequest) (*schemas.BifrostOCRResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.OCRRequest)
}

// Speech is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) Speech(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostSpeechRequest) (*schemas.BifrostSpeechResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.SpeechRequest)
}

// SpeechStream is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) SpeechStream(_ *schemas.BifrostContext, _ schemas.PostHookRunner, _ func(context.Context), _ schemas.Key, _ *schemas.BifrostSpeechRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.SpeechStreamRequest)
}

// Transcription is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) Transcription(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostTranscriptionRequest) (*schemas.BifrostTranscriptionResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.TranscriptionRequest)
}

// TranscriptionStream is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) TranscriptionStream(_ *schemas.BifrostContext, _ schemas.PostHookRunner, _ func(context.Context), _ schemas.Key, _ *schemas.BifrostTranscriptionRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.TranscriptionStreamRequest)
}

// ImageGeneration is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ImageGeneration(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostImageGenerationRequest) (*schemas.BifrostImageGenerationResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ImageGenerationRequest)
}

// ImageGenerationStream is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ImageGenerationStream(_ *schemas.BifrostContext, _ schemas.PostHookRunner, _ func(context.Context), _ schemas.Key, _ *schemas.BifrostImageGenerationRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ImageGenerationStreamRequest)
}

// ImageEdit is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ImageEdit(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostImageEditRequest) (*schemas.BifrostImageGenerationResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ImageEditRequest)
}

// ImageEditStream is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ImageEditStream(_ *schemas.BifrostContext, _ schemas.PostHookRunner, _ func(context.Context), _ schemas.Key, _ *schemas.BifrostImageEditRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ImageEditStreamRequest)
}

// ImageVariation is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ImageVariation(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostImageVariationRequest) (*schemas.BifrostImageGenerationResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ImageVariationRequest)
}

// VideoGeneration is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) VideoGeneration(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostVideoGenerationRequest) (*schemas.BifrostVideoGenerationResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.VideoGenerationRequest)
}

// VideoRetrieve is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) VideoRetrieve(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostVideoRetrieveRequest) (*schemas.BifrostVideoGenerationResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.VideoRetrieveRequest)
}

// VideoDownload is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) VideoDownload(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostVideoDownloadRequest) (*schemas.BifrostVideoDownloadResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.VideoDownloadRequest)
}

// VideoDelete is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) VideoDelete(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostVideoDeleteRequest) (*schemas.BifrostVideoDeleteResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.VideoDeleteRequest)
}

// VideoList is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) VideoList(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostVideoListRequest) (*schemas.BifrostVideoListResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.VideoListRequest)
}

// VideoRemix is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) VideoRemix(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostVideoRemixRequest) (*schemas.BifrostVideoGenerationResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.VideoRemixRequest)
}

// BatchCreate is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) BatchCreate(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostBatchCreateRequest) (*schemas.BifrostBatchCreateResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.BatchCreateRequest)
}

// BatchList is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) BatchList(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostBatchListRequest) (*schemas.BifrostBatchListResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.BatchListRequest)
}

// BatchRetrieve is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) BatchRetrieve(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostBatchRetrieveRequest) (*schemas.BifrostBatchRetrieveResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.BatchRetrieveRequest)
}

// BatchCancel is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) BatchCancel(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostBatchCancelRequest) (*schemas.BifrostBatchCancelResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.BatchCancelRequest)
}

// BatchDelete is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) BatchDelete(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostBatchDeleteRequest) (*schemas.BifrostBatchDeleteResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.BatchDeleteRequest)
}

// BatchResults is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) BatchResults(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostBatchResultsRequest) (*schemas.BifrostBatchResultsResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.BatchResultsRequest)
}

// FileUpload is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) FileUpload(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostFileUploadRequest) (*schemas.BifrostFileUploadResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.FileUploadRequest)
}

// FileList is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) FileList(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostFileListRequest) (*schemas.BifrostFileListResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.FileListRequest)
}

// FileRetrieve is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) FileRetrieve(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostFileRetrieveRequest) (*schemas.BifrostFileRetrieveResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.FileRetrieveRequest)
}

// FileDelete is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) FileDelete(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostFileDeleteRequest) (*schemas.BifrostFileDeleteResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.FileDeleteRequest)
}

// FileContent is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) FileContent(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostFileContentRequest) (*schemas.BifrostFileContentResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.FileContentRequest)
}

// CachedContentCreate is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) CachedContentCreate(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostCachedContentCreateRequest) (*schemas.BifrostCachedContentCreateResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.CachedContentCreateRequest)
}

// CachedContentList is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) CachedContentList(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostCachedContentListRequest) (*schemas.BifrostCachedContentListResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.CachedContentListRequest)
}

// CachedContentRetrieve is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) CachedContentRetrieve(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostCachedContentRetrieveRequest) (*schemas.BifrostCachedContentRetrieveResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.CachedContentRetrieveRequest)
}

// CachedContentUpdate is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) CachedContentUpdate(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostCachedContentUpdateRequest) (*schemas.BifrostCachedContentUpdateResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.CachedContentUpdateRequest)
}

// CachedContentDelete is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) CachedContentDelete(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostCachedContentDeleteRequest) (*schemas.BifrostCachedContentDeleteResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.CachedContentDeleteRequest)
}

// ContainerCreate is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerCreate(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostContainerCreateRequest) (*schemas.BifrostContainerCreateResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerCreateRequest)
}

// ContainerList is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerList(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostContainerListRequest) (*schemas.BifrostContainerListResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerListRequest)
}

// ContainerRetrieve is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerRetrieve(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostContainerRetrieveRequest) (*schemas.BifrostContainerRetrieveResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerRetrieveRequest)
}

// ContainerDelete is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerDelete(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostContainerDeleteRequest) (*schemas.BifrostContainerDeleteResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerDeleteRequest)
}

// ContainerFileCreate is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerFileCreate(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostContainerFileCreateRequest) (*schemas.BifrostContainerFileCreateResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerFileCreateRequest)
}

// ContainerFileList is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerFileList(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostContainerFileListRequest) (*schemas.BifrostContainerFileListResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerFileListRequest)
}

// ContainerFileRetrieve is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerFileRetrieve(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostContainerFileRetrieveRequest) (*schemas.BifrostContainerFileRetrieveResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerFileRetrieveRequest)
}

// ContainerFileContent is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerFileContent(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostContainerFileContentRequest) (*schemas.BifrostContainerFileContentResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerFileContentRequest)
}

// ContainerFileDelete is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) ContainerFileDelete(_ *schemas.BifrostContext, _ []schemas.Key, _ *schemas.BifrostContainerFileDeleteRequest) (*schemas.BifrostContainerFileDeleteResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.ContainerFileDeleteRequest)
}

// Passthrough is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) Passthrough(_ *schemas.BifrostContext, _ schemas.Key, _ *schemas.BifrostPassthroughRequest) (*schemas.BifrostPassthroughResponse, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.PassthroughRequest)
}

// PassthroughStream is not supported by the GigaChat provider skeleton.
func (provider *GigaChatProvider) PassthroughStream(_ *schemas.BifrostContext, _ schemas.PostHookRunner, _ func(context.Context), _ schemas.Key, _ *schemas.BifrostPassthroughRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError) {
	return nil, provider.unsupported(schemas.PassthroughStreamRequest)
}
