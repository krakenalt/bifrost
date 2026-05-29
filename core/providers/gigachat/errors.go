package gigachat

import (
	"fmt"
	"strconv"
	"strings"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"
	schemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// ParseGigaChatError parses GigaChat REST error responses.
func ParseGigaChatError(resp *fasthttp.Response, providerName schemas.ModelProvider) *schemas.BifrostError {
	var errorResp GigaChatErrorResponse
	bifrostErr := providerUtils.HandleProviderAPIError(resp, &errorResp)
	if bifrostErr.Error == nil {
		bifrostErr.Error = &schemas.ErrorField{}
	}

	if strings.TrimSpace(errorResp.Message) != "" {
		bifrostErr.Error.Message = errorResp.Message
	}
	if errorResp.Code != nil {
		code := strconv.Itoa(*errorResp.Code)
		bifrostErr.Error.Code = &code
	} else if errorResp.Status != nil {
		code := strconv.Itoa(*errorResp.Status)
		bifrostErr.Error.Code = &code
	}
	if errorResp.Status != nil && bifrostErr.StatusCode == nil {
		status := *errorResp.Status
		bifrostErr.StatusCode = &status
	}
	if strings.TrimSpace(bifrostErr.Error.Message) == "" {
		if bifrostErr.StatusCode != nil {
			bifrostErr.Error.Message = fmt.Sprintf("GigaChat API error (status %d)", *bifrostErr.StatusCode)
		} else {
			bifrostErr.Error.Message = "GigaChat API error"
		}
	}

	bifrostErr.ExtraFields.Provider = providerName
	return bifrostErr
}
