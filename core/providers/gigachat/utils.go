package gigachat

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/bytedance/sonic"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
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

func buildGigaChatTLSClient(baseClient *fasthttp.Client, keyConfig *schemas.GigaChatKeyConfig) (*fasthttp.Client, error) {
	if keyConfig == nil || !gigaChatKeyConfigHasTLSMaterial(keyConfig) {
		return baseClient, nil
	}

	client := providerUtils.CloneFastHTTPClientConfig(baseClient)
	tlsConfig := client.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		tlsConfig = tlsConfig.Clone()
	}

	if caBundleFile := strings.TrimSpace(keyConfig.CABundleFile); caBundleFile != "" {
		caBundlePEM, err := os.ReadFile(caBundleFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read gigachat_key_config.ca_bundle_file: %w", err)
		}
		if tlsConfig.RootCAs == nil {
			rootCAs, err := x509.SystemCertPool()
			if err != nil || rootCAs == nil {
				rootCAs = x509.NewCertPool()
			}
			tlsConfig.RootCAs = rootCAs
		} else {
			tlsConfig.RootCAs = tlsConfig.RootCAs.Clone()
		}
		if !tlsConfig.RootCAs.AppendCertsFromPEM(caBundlePEM) {
			return nil, fmt.Errorf("failed to parse gigachat_key_config.ca_bundle_file")
		}
	}

	hasCertFile := strings.TrimSpace(keyConfig.CertFile) != ""
	hasKeyFile := strings.TrimSpace(keyConfig.KeyFile) != ""
	if hasCertFile != hasKeyFile {
		return nil, fmt.Errorf("gigachat_key_config.cert_file and gigachat_key_config.key_file must be set together")
	}
	if keyConfig.KeyFilePassword.IsSet() {
		return nil, fmt.Errorf("encrypted gigachat_key_config.key_file is not supported")
	}
	if hasCertFile {
		certificate, err := tls.LoadX509KeyPair(keyConfig.CertFile, keyConfig.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load gigachat_key_config.cert_file/key_file: %w", err)
		}
		tlsConfig.Certificates = append(tlsConfig.Certificates, certificate)
	}

	client.TLSConfig = tlsConfig
	return client, nil
}

func gigaChatKeyConfigHasTLSMaterial(keyConfig *schemas.GigaChatKeyConfig) bool {
	return strings.TrimSpace(keyConfig.CABundleFile) != "" ||
		strings.TrimSpace(keyConfig.CertFile) != "" ||
		strings.TrimSpace(keyConfig.KeyFile) != "" ||
		keyConfig.KeyFilePassword.IsSet()
}

func enrichGigaChatError(ctx *schemas.BifrostContext, bifrostErr *schemas.BifrostError, requestBody []byte, responseBody []byte, sendBackRawRequest bool, sendBackRawResponse bool) *schemas.BifrostError {
	return providerUtils.EnrichError(ctx, bifrostErr, redactGigaChatRawPayload(requestBody), redactGigaChatRawPayload(responseBody), sendBackRawRequest, sendBackRawResponse)
}

func redactGigaChatRawPayload(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	var value interface{}
	if err := sonic.Unmarshal(payload, &value); err != nil {
		return []byte(redactGigaChatSensitiveText(string(payload)))
	}
	redactGigaChatJSONValue(value)
	redacted, err := sonic.Marshal(value)
	if err != nil {
		return []byte(redactGigaChatSensitiveText(string(payload)))
	}
	return redacted
}

func redactGigaChatJSONValue(value interface{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			if isGigaChatSensitiveField(key) {
				typed[key] = "<redacted>"
				continue
			}
			redactGigaChatJSONValue(child)
		}
	case []interface{}:
		for _, child := range typed {
			redactGigaChatJSONValue(child)
		}
	}
}

func isGigaChatSensitiveField(fieldName string) bool {
	switch strings.ToLower(strings.TrimSpace(fieldName)) {
	case "authorization", "access_token", "credentials", "password", "key_file_password", "client_secret", "refresh_token":
		return true
	default:
		return false
	}
}

func redactGigaChatSensitiveText(text string) string {
	redacted := text
	for _, prefix := range []string{"Bearer ", "Basic "} {
		searchFrom := 0
		for {
			index := strings.Index(redacted[searchFrom:], prefix)
			if index < 0 {
				break
			}
			start := searchFrom + index + len(prefix)
			end := start
			for end < len(redacted) && !strings.ContainsRune(" \t\r\n\"',}", rune(redacted[end])) {
				end++
			}
			redacted = redacted[:start] + "<redacted>" + redacted[end:]
			searchFrom = start + len("<redacted>")
		}
	}
	return redacted
}
