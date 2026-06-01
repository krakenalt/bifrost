package gigachat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

const (
	gigaChatFilePurposeAssistant = "assistant"
	gigaChatFilePurposeGeneral   = "general"
)

func toGigaChatFilePurpose(purpose schemas.FilePurpose) string {
	if purpose == schemas.FilePurposeAssistants {
		return gigaChatFilePurposeAssistant
	}
	return gigaChatFilePurposeGeneral
}

func toBifrostFilePurpose(gigaChatPurpose string, requestedPurpose schemas.FilePurpose) schemas.FilePurpose {
	switch strings.TrimSpace(gigaChatPurpose) {
	case gigaChatFilePurposeAssistant:
		return schemas.FilePurposeAssistants
	case gigaChatFilePurposeGeneral, "":
		if requestedPurpose != "" {
			return requestedPurpose
		}
		return schemas.FilePurposeUserData
	default:
		return schemas.FilePurpose(gigaChatPurpose)
	}
}

func (provider *GigaChatProvider) fileUploadWithRefresh(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostFileUploadRequest, forceRefresh bool) (*schemas.BifrostFileUploadResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)

	body, contentType, bifrostErr := buildGigaChatFileUploadBody(request)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	responseBody, providerResponseHeaders, _, latency, bifrostErr := provider.executeGigaChatFileRequest(ctx, key, schemas.FileUploadRequest, http.MethodPost, "/files", contentType, "application/json", body, nil, forceRefresh)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	gigaChatResponse := &GigaChatUploadedFile{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, gigaChatResponse, nil, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, enrichGigaChatError(ctx, bifrostErr, nil, responseBody, sendBackRawRequest, sendBackRawResponse)
	}

	response := toBifrostFileUploadResponse(*gigaChatResponse, request.Purpose, latency)
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}
	return response, nil
}

func (provider *GigaChatProvider) fileListWithRefresh(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostFileListRequest, forceRefresh bool) (*schemas.BifrostFileListResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)

	responseBody, providerResponseHeaders, _, latency, bifrostErr := provider.executeGigaChatFileRequest(ctx, key, schemas.FileListRequest, http.MethodGet, "/files", "", "application/json", nil, nil, forceRefresh)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	gigaChatResponse := &GigaChatUploadedFiles{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, gigaChatResponse, nil, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, enrichGigaChatError(ctx, bifrostErr, nil, responseBody, sendBackRawRequest, sendBackRawResponse)
	}

	files := make([]schemas.FileObject, 0, len(gigaChatResponse.Data))
	for _, file := range gigaChatResponse.Data {
		var requestedPurpose schemas.FilePurpose
		if request != nil {
			requestedPurpose = request.Purpose
		}
		converted := toBifrostFileObject(file, requestedPurpose)
		if request != nil && request.Purpose != "" && converted.Purpose != request.Purpose {
			continue
		}
		files = append(files, converted)
	}

	response := &schemas.BifrostFileListResponse{
		Object: "list",
		Data:   files,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Latency:                 latency.Milliseconds(),
			ProviderResponseHeaders: providerResponseHeaders,
		},
	}
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}
	return response, nil
}

func (provider *GigaChatProvider) fileRetrieveWithRefresh(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostFileRetrieveRequest, forceRefresh bool) (*schemas.BifrostFileRetrieveResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)

	path := fmt.Sprintf("/files/%s", url.PathEscape(request.FileID))
	responseBody, providerResponseHeaders, _, latency, bifrostErr := provider.executeGigaChatFileRequest(ctx, key, schemas.FileRetrieveRequest, http.MethodGet, path, "", "application/json", nil, nil, forceRefresh)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	gigaChatResponse := &GigaChatUploadedFile{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, gigaChatResponse, nil, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, enrichGigaChatError(ctx, bifrostErr, nil, responseBody, sendBackRawRequest, sendBackRawResponse)
	}

	response := toBifrostFileRetrieveResponse(*gigaChatResponse, "", latency)
	response.ExtraFields.ProviderResponseHeaders = providerResponseHeaders
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}
	return response, nil
}

func (provider *GigaChatProvider) fileDeleteWithRefresh(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostFileDeleteRequest, forceRefresh bool) (*schemas.BifrostFileDeleteResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)

	path := fmt.Sprintf("/files/%s/delete", url.PathEscape(request.FileID))
	responseBody, providerResponseHeaders, _, latency, bifrostErr := provider.executeGigaChatFileRequest(ctx, key, schemas.FileDeleteRequest, http.MethodPost, path, "", "application/json", nil, nil, forceRefresh)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	gigaChatResponse := &GigaChatDeletedFile{}
	rawRequest, rawResponse, bifrostErr := providerUtils.HandleProviderResponse(responseBody, gigaChatResponse, nil, sendBackRawRequest, sendBackRawResponse)
	if bifrostErr != nil {
		return nil, enrichGigaChatError(ctx, bifrostErr, nil, responseBody, sendBackRawRequest, sendBackRawResponse)
	}

	response := &schemas.BifrostFileDeleteResponse{
		ID:      gigaChatResponse.ID,
		Object:  "file",
		Deleted: gigaChatResponse.Deleted,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Latency:                 latency.Milliseconds(),
			ProviderResponseHeaders: providerResponseHeaders,
		},
	}
	if sendBackRawRequest {
		response.ExtraFields.RawRequest = rawRequest
	}
	if sendBackRawResponse {
		response.ExtraFields.RawResponse = rawResponse
	}
	return response, nil
}

func (provider *GigaChatProvider) fileContentWithRefresh(ctx *schemas.BifrostContext, key schemas.Key, request *schemas.BifrostFileContentRequest, forceRefresh bool) (*schemas.BifrostFileContentResponse, *schemas.BifrostError) {
	ctx = ensureGigaChatContext(ctx)

	path := fmt.Sprintf("/files/%s/content", url.PathEscape(request.FileID))
	responseBody, providerResponseHeaders, responseContentType, latency, bifrostErr := provider.executeGigaChatFileRequest(ctx, key, schemas.FileContentRequest, http.MethodGet, path, "", "*/*", nil, nil, forceRefresh)
	if bifrostErr != nil {
		return nil, bifrostErr
	}

	contentType := responseContentType
	content, decodedContentType, decodeErr := decodeGigaChatFileContent(responseBody, contentType)
	if decodeErr != nil {
		return nil, newGigaChatProviderResponseError("failed to decode GigaChat file content response", decodeErr)
	}
	if decodedContentType != "" {
		contentType = decodedContentType
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}

	return &schemas.BifrostFileContentResponse{
		FileID:      request.FileID,
		Content:     content,
		ContentType: contentType,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Latency:                 latency.Milliseconds(),
			ProviderResponseHeaders: providerResponseHeaders,
		},
	}, nil
}

func (provider *GigaChatProvider) executeGigaChatFileRequest(
	ctx *schemas.BifrostContext,
	key schemas.Key,
	requestType schemas.RequestType,
	method string,
	path string,
	contentType string,
	accept string,
	body []byte,
	rawRequestForError []byte,
	forceRefresh bool,
) ([]byte, map[string]string, string, time.Duration, *schemas.BifrostError) {
	headers, bifrostErr := provider.buildAuthHeaders(ctx, key)
	if forceRefresh {
		headers, bifrostErr = provider.refreshAuthHeaders(ctx, key)
	}
	if bifrostErr != nil {
		return nil, nil, "", 0, bifrostErr
	}

	client, clientErr := buildGigaChatTLSClient(provider.client, key.GigaChatKeyConfig)
	if clientErr != nil {
		return nil, nil, "", 0, newGigaChatConfigurationError(clientErr.Error())
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	for headerName, headerValue := range headers {
		req.Header.Set(headerName, headerValue)
	}
	req.SetRequestURI(buildGigaChatRequestURL(ctx, resolveBaseURL(key, provider.networkConfig), gigaChatAPIVersionV1, path, provider.customProviderConfig, requestType))
	req.Header.SetMethod(method)
	if strings.TrimSpace(contentType) != "" {
		req.Header.SetContentType(contentType)
	}
	if strings.TrimSpace(accept) != "" {
		req.Header.Set("Accept", accept)
	}
	if body != nil {
		req.SetBody(body)
	}

	sendBackRawRequest := providerUtils.ShouldSendBackRawRequest(ctx, provider.sendBackRawRequest)
	sendBackRawResponse := providerUtils.ShouldSendBackRawResponse(ctx, provider.sendBackRawResponse)

	latency, bifrostErr, wait := providerUtils.MakeRequestWithContext(ctx, client, req, resp)
	defer wait()
	if bifrostErr != nil {
		bifrostErr.ExtraFields.Provider = provider.GetProviderKey()
		return nil, nil, "", latency, enrichGigaChatError(ctx, bifrostErr, rawRequestForError, nil, sendBackRawRequest, sendBackRawResponse)
	}

	responseContentType := string(resp.Header.ContentType())
	providerResponseHeaders := providerUtils.ExtractProviderResponseHeaders(resp)
	ctx.SetValue(schemas.BifrostContextKeyProviderResponseHeaders, providerResponseHeaders)

	if resp.StatusCode() < fasthttp.StatusOK || resp.StatusCode() >= fasthttp.StatusMultipleChoices {
		bifrostErr := ParseGigaChatError(resp, provider.GetProviderKey())
		return nil, providerResponseHeaders, responseContentType, latency, enrichGigaChatError(ctx, bifrostErr, rawRequestForError, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	responseBody, err := providerUtils.CheckAndDecodeBody(resp)
	if err != nil {
		bifrostErr := newGigaChatProviderResponseError("failed to decode GigaChat file response", err)
		return nil, providerResponseHeaders, responseContentType, latency, enrichGigaChatError(ctx, bifrostErr, rawRequestForError, resp.Body(), sendBackRawRequest, sendBackRawResponse)
	}

	return responseBody, providerResponseHeaders, responseContentType, latency, nil
}

func buildGigaChatFileUploadBody(request *schemas.BifrostFileUploadRequest) ([]byte, string, *schemas.BifrostError) {
	if request == nil {
		return nil, "", providerUtils.NewBifrostOperationError("file upload request is nil", nil)
	}
	if len(request.File) == 0 {
		return nil, "", providerUtils.NewBifrostOperationError("file content is required", nil)
	}
	if request.Purpose == "" {
		return nil, "", providerUtils.NewBifrostOperationError("purpose is required", nil)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("purpose", toGigaChatFilePurpose(request.Purpose)); err != nil {
		return nil, "", providerUtils.NewBifrostOperationError("failed to write purpose field", err)
	}

	filename := strings.TrimSpace(request.Filename)
	if filename == "" {
		filename = "file"
	}

	partHeaders := textproto.MIMEHeader{}
	partHeaders.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeGigaChatMultipartFilename(filename)))
	if request.ContentType != nil && strings.TrimSpace(*request.ContentType) != "" {
		partHeaders.Set("Content-Type", strings.TrimSpace(*request.ContentType))
	}
	part, err := writer.CreatePart(partHeaders)
	if err != nil {
		return nil, "", providerUtils.NewBifrostOperationError("failed to create form file", err)
	}
	if _, err := part.Write(request.File); err != nil {
		return nil, "", providerUtils.NewBifrostOperationError("failed to write file content", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", providerUtils.NewBifrostOperationError("failed to close multipart writer", err)
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

func escapeGigaChatMultipartFilename(filename string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(filename)
}

func toBifrostFileObject(file GigaChatUploadedFile, requestedPurpose schemas.FilePurpose) schemas.FileObject {
	object := file.Object
	if object == "" {
		object = "file"
	}
	return schemas.FileObject{
		ID:        file.ID,
		Object:    object,
		Bytes:     file.Bytes,
		CreatedAt: file.CreatedAt,
		Filename:  file.Filename,
		Purpose:   toBifrostFilePurpose(file.Purpose, requestedPurpose),
	}
}

func toBifrostFileUploadResponse(file GigaChatUploadedFile, requestedPurpose schemas.FilePurpose, latency time.Duration) *schemas.BifrostFileUploadResponse {
	object := file.Object
	if object == "" {
		object = "file"
	}
	return &schemas.BifrostFileUploadResponse{
		ID:             file.ID,
		Object:         object,
		Bytes:          file.Bytes,
		CreatedAt:      file.CreatedAt,
		Filename:       file.Filename,
		Purpose:        toBifrostFilePurpose(file.Purpose, requestedPurpose),
		StorageBackend: schemas.FileStorageAPI,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Latency: latency.Milliseconds(),
		},
	}
}

func toBifrostFileRetrieveResponse(file GigaChatUploadedFile, requestedPurpose schemas.FilePurpose, latency time.Duration) *schemas.BifrostFileRetrieveResponse {
	object := file.Object
	if object == "" {
		object = "file"
	}
	return &schemas.BifrostFileRetrieveResponse{
		ID:             file.ID,
		Object:         object,
		Bytes:          file.Bytes,
		CreatedAt:      file.CreatedAt,
		Filename:       file.Filename,
		Purpose:        toBifrostFilePurpose(file.Purpose, requestedPurpose),
		StorageBackend: schemas.FileStorageAPI,
		ExtraFields: schemas.BifrostResponseExtraFields{
			Latency: latency.Milliseconds(),
		},
	}
}

func decodeGigaChatFileContent(body []byte, contentType string) ([]byte, string, error) {
	if isGigaChatJSONContentType(contentType) {
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrapper); err == nil {
			if rawContent, ok := wrapper["content"]; ok {
				var encoded string
				if err := json.Unmarshal(rawContent, &encoded); err != nil {
					return nil, "", err
				}
				decoded, err := decodeGigaChatBase64Content(encoded)
				if err != nil {
					return nil, "", err
				}
				return decoded, "application/octet-stream", nil
			}
		}
	}
	return append([]byte(nil), body...), contentType, nil
}

func isGigaChatJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return contentType == "application/json" || strings.HasPrefix(contentType, "application/json;")
}

func decodeGigaChatBase64Content(encoded string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err == nil {
		return decoded, nil
	}
	decoded, rawErr := base64.RawStdEncoding.DecodeString(encoded)
	if rawErr == nil {
		return decoded, nil
	}
	return nil, err
}
