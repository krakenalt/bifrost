package bifrost

import (
	"strings"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestValidateKeyGigaChat(t *testing.T) {
	t.Parallel()

	t.Run("CredentialsConfigDefaultsScope", func(t *testing.T) {
		t.Parallel()

		key := schemas.Key{
			Name:   "gigachat",
			Weight: 1,
			GigaChatKeyConfig: &schemas.GigaChatKeyConfig{
				Credentials: schemas.NewEnvVar("env.GIGACHAT_CREDENTIALS"),
			},
		}

		if err := validateKey(schemas.GigaChat, &key); err != nil {
			t.Fatalf("validateKey returned error: %v", err)
		}
		if key.GigaChatKeyConfig.Scope != schemas.DefaultGigaChatScope {
			t.Fatalf("scope mismatch: got %q, want %q", key.GigaChatKeyConfig.Scope, schemas.DefaultGigaChatScope)
		}
	})

	t.Run("ValueOnlyAllowed", func(t *testing.T) {
		t.Parallel()

		key := schemas.Key{
			Name:   "gigachat",
			Value:  *schemas.NewEnvVar("access-token"),
			Weight: 1,
		}

		if err := validateKey(schemas.GigaChat, &key); err != nil {
			t.Fatalf("validateKey returned error: %v", err)
		}
	})

	t.Run("MissingAuthMaterialRejected", func(t *testing.T) {
		t.Parallel()

		key := schemas.Key{
			Name:   "gigachat",
			Weight: 1,
			GigaChatKeyConfig: &schemas.GigaChatKeyConfig{
				BaseURL: "https://api.giga.chat",
			},
		}

		err := validateKey(schemas.GigaChat, &key)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "gigachat_key_config auth material") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("PartialUserPasswordRejected", func(t *testing.T) {
		t.Parallel()

		key := schemas.Key{
			Name:   "gigachat",
			Weight: 1,
			GigaChatKeyConfig: &schemas.GigaChatKeyConfig{
				User: schemas.NewEnvVar("env.GIGACHAT_USER"),
			},
		}

		err := validateKey(schemas.GigaChat, &key)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "user and gigachat_key_config.password") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("PartialCertificatePairRejected", func(t *testing.T) {
		t.Parallel()

		key := schemas.Key{
			Name:   "gigachat",
			Weight: 1,
			GigaChatKeyConfig: &schemas.GigaChatKeyConfig{
				CertFile: "/secure/client.pem",
			},
		}

		err := validateKey(schemas.GigaChat, &key)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "cert_file and gigachat_key_config.key_file") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
