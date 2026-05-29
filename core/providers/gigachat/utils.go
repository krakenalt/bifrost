package gigachat

import (
	"strings"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
)

const (
	gigaChatDefaultBaseURL = "https://gigachat.devices.sberbank.ru/api"
	gigaChatDefaultAuthURL = "https://ngw.devices.sberbank.ru:9443/api/v2/oauth"

	gigaChatAPIVersionV1 = "v1"
	gigaChatAPIVersionV2 = "v2"
)

func resolveAuthURL(key schemas.Key) string {
	if key.GigaChatKeyConfig != nil {
		if authURL := strings.TrimSpace(key.GigaChatKeyConfig.AuthURL); authURL != "" {
			return strings.TrimRight(authURL, "/")
		}
	}
	return gigaChatDefaultAuthURL
}

func resolveBaseURL(key schemas.Key, networkConfig schemas.NetworkConfig) string {
	if key.GigaChatKeyConfig != nil {
		if baseURL := strings.TrimSpace(key.GigaChatKeyConfig.BaseURL); baseURL != "" {
			return strings.TrimRight(baseURL, "/")
		}
	}
	if baseURL := strings.TrimSpace(networkConfig.BaseURL); baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return gigaChatDefaultBaseURL
}

func buildGigaChatURL(baseURL string, apiVersion string, path string) string {
	resolvedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if resolvedBaseURL == "" {
		resolvedBaseURL = gigaChatDefaultBaseURL
	}

	version := normalizeGigaChatAPIVersion(apiVersion)
	versionedBaseURL := buildGigaChatVersionedBaseURL(resolvedBaseURL, version)
	normalizedPath := normalizeGigaChatPath(path, version)
	if normalizedPath == "" {
		return versionedBaseURL
	}
	return versionedBaseURL + normalizedPath
}

func buildGigaChatRequestURL(ctx *schemas.BifrostContext, baseURL string, apiVersion string, defaultPath string, customProviderConfig *schemas.CustomProviderConfig, requestType schemas.RequestType) string {
	path, isCompleteURL := providerUtils.GetRequestPath(ctx, defaultPath, customProviderConfig, requestType)
	if isCompleteURL {
		return path
	}
	return buildGigaChatURL(baseURL, apiVersion, path)
}

func normalizeGigaChatAPIVersion(apiVersion string) string {
	return strings.Trim(strings.TrimSpace(apiVersion), "/")
}

func buildGigaChatVersionedBaseURL(baseURL string, apiVersion string) string {
	if apiVersion == "" {
		return baseURL
	}
	for _, version := range []string{gigaChatAPIVersionV1, gigaChatAPIVersionV2} {
		suffix := "/" + version
		if strings.HasSuffix(baseURL, suffix) {
			return strings.TrimSuffix(baseURL, suffix) + "/" + apiVersion
		}
	}
	return baseURL + "/" + apiVersion
}

func normalizeGigaChatPath(path string, apiVersion string) string {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return ""
	}
	normalizedPath = "/" + strings.TrimLeft(normalizedPath, "/")

	for _, version := range []string{apiVersion, gigaChatAPIVersionV1, gigaChatAPIVersionV2} {
		if version == "" {
			continue
		}
		versionPrefix := "/" + version
		if normalizedPath == versionPrefix {
			return ""
		}
		if strings.HasPrefix(normalizedPath, versionPrefix+"/") {
			return strings.TrimPrefix(normalizedPath, versionPrefix)
		}
	}

	return normalizedPath
}
