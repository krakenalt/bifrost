package gigachat

import (
	"encoding/json"
	"testing"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

func TestToGigaChatFilePurpose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		purpose schemas.FilePurpose
		want    string
	}{
		{name: "assistants maps to assistant", purpose: schemas.FilePurposeAssistants, want: gigaChatFilePurposeAssistant},
		{name: "batch maps to general", purpose: schemas.FilePurposeBatch, want: gigaChatFilePurposeGeneral},
		{name: "fine tune maps to general", purpose: schemas.FilePurposeFineTune, want: gigaChatFilePurposeGeneral},
		{name: "vision maps to general", purpose: schemas.FilePurposeVision, want: gigaChatFilePurposeGeneral},
		{name: "responses maps to general", purpose: schemas.FilePurposeResponses, want: gigaChatFilePurposeGeneral},
		{name: "evals maps to general", purpose: schemas.FilePurposeEvals, want: gigaChatFilePurposeGeneral},
		{name: "user data maps to general", purpose: schemas.FilePurposeUserData, want: gigaChatFilePurposeGeneral},
		{name: "batch output maps to general", purpose: schemas.FilePurposeBatchOutput, want: gigaChatFilePurposeGeneral},
		{name: "empty maps to general", purpose: "", want: gigaChatFilePurposeGeneral},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := toGigaChatFilePurpose(tt.purpose); got != tt.want {
				t.Fatalf("toGigaChatFilePurpose(%q) = %q, want %q", tt.purpose, got, tt.want)
			}
		})
	}
}

func TestToBifrostFilePurpose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		gigaChatPurpose  string
		requestedPurpose schemas.FilePurpose
		want             schemas.FilePurpose
	}{
		{
			name:            "assistant maps to assistants",
			gigaChatPurpose: gigaChatFilePurposeAssistant,
			want:            schemas.FilePurposeAssistants,
		},
		{
			name:            "general defaults to user data",
			gigaChatPurpose: gigaChatFilePurposeGeneral,
			want:            schemas.FilePurposeUserData,
		},
		{
			name:             "general keeps requested purpose when available",
			gigaChatPurpose:  gigaChatFilePurposeGeneral,
			requestedPurpose: schemas.FilePurposeBatch,
			want:             schemas.FilePurposeBatch,
		},
		{
			name: "empty defaults to user data",
			want: schemas.FilePurposeUserData,
		},
		{
			name:            "unknown purpose is preserved",
			gigaChatPurpose: "custom_purpose",
			want:            schemas.FilePurpose("custom_purpose"),
		},
		{
			name:            "general trims whitespace",
			gigaChatPurpose: " general ",
			want:            schemas.FilePurposeUserData,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := toBifrostFilePurpose(tt.gigaChatPurpose, tt.requestedPurpose)
			if got != tt.want {
				t.Fatalf("toBifrostFilePurpose(%q, %q) = %q, want %q", tt.gigaChatPurpose, tt.requestedPurpose, got, tt.want)
			}
		})
	}
}

func TestGigaChatFileTypesJSON(t *testing.T) {
	t.Parallel()

	accessPolicy := "public"
	uploaded := GigaChatUploadedFile{
		ID:           "file-1",
		Object:       "file",
		Bytes:        123,
		CreatedAt:    1780306293,
		Filename:     "document.txt",
		Purpose:      gigaChatFilePurposeGeneral,
		AccessPolicy: &accessPolicy,
	}

	raw, err := json.Marshal(GigaChatUploadedFiles{Data: []GigaChatUploadedFile{uploaded}})
	if err != nil {
		t.Fatalf("marshal uploaded files: %v", err)
	}

	var decoded GigaChatUploadedFiles
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal uploaded files: %v", err)
	}
	if len(decoded.Data) != 1 {
		t.Fatalf("decoded %d files, want 1", len(decoded.Data))
	}
	if decoded.Data[0].ID != uploaded.ID || decoded.Data[0].Purpose != uploaded.Purpose {
		t.Fatalf("decoded file = %+v, want %+v", decoded.Data[0], uploaded)
	}

	contentRaw, err := json.Marshal(GigaChatFileContent{Content: "SGVsbG8="})
	if err != nil {
		t.Fatalf("marshal file content: %v", err)
	}
	if string(contentRaw) != `{"content":"SGVsbG8="}` {
		t.Fatalf("content JSON = %s", contentRaw)
	}
}
