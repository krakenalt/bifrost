package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGigaChatKeyConfig_Redacted(t *testing.T) {
	t.Parallel()

	config := &GigaChatKeyConfig{
		Credentials:     NewEnvVar("secret-credentials"),
		User:            NewEnvVar("secret-user"),
		Password:        NewEnvVar("secret-password"),
		AccessToken:     NewEnvVar("secret-access-token"),
		KeyFilePassword: NewEnvVar("secret-key-password"),
		CertFile:        "/secure/client.pem",
		KeyFile:         "/secure/client.key",
		CABundleFile:    "/secure/ca.pem",
		BaseURL:         "https://api.giga.chat",
		AuthURL:         "https://ngw.devices.sberbank.ru:9443/api/v2/oauth",
	}

	redacted := config.Redacted()
	data, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	output := string(data)

	for _, secret := range []string{
		"secret-credentials",
		"secret-user",
		"secret-password",
		"secret-access-token",
		"secret-key-password",
		"/secure/client.pem",
		"/secure/client.key",
		"/secure/ca.pem",
	} {
		if strings.Contains(output, secret) {
			t.Fatalf("redacted config leaked %q in %s", secret, output)
		}
	}
	if !strings.Contains(output, "https://api.giga.chat") {
		t.Fatalf("non-secret base_url should be preserved in %s", output)
	}
}

func TestGigaChatKeyConfig_EnvVarAuthMaterial(t *testing.T) {
	t.Parallel()

	config := &GigaChatKeyConfig{
		Credentials: NewEnvVar("env.MISSING_GIGACHAT_CREDENTIALS"),
	}
	config.CheckAndSetDefaults()

	if config.Scope != DefaultGigaChatScope {
		t.Fatalf("scope mismatch: got %q, want %q", config.Scope, DefaultGigaChatScope)
	}
	if !config.HasAuthMaterial() {
		t.Fatal("expected unresolved env var reference to count as configured auth material")
	}
}
