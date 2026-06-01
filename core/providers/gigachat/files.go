package gigachat

import (
	"strings"

	schemas "github.com/maximhq/bifrost/core/schemas"
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
