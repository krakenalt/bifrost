// Package gigachat implements the GigaChat LLM provider.
package gigachat

// # AUTH TYPES

// GigaChatTokenResponse is returned by the GigaChat OAuth endpoint.
type GigaChatTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresAt   int64  `json:"expires_at"`
}

// # ERROR TYPES

// GigaChatErrorResponse is the common REST API error shape used by GigaChat.
type GigaChatErrorResponse struct {
	Status  *int   `json:"status,omitempty"`
	Code    *int   `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
