package api

import (
	"testing"
	"time"
)

func TestExposeDevTokensDefaultsToLocalDevelopmentOnly(t *testing.T) {
	t.Setenv("TOKHUB_EXPOSE_DEV_TOKENS", "")

	if !exposeDevTokens("development", "http://localhost:8080") {
		t.Fatal("expected local development URL to expose dev tokens")
	}
	if exposeDevTokens("development", "https://tokhub.example.com") {
		t.Fatal("expected non-local development URL to hide dev tokens by default")
	}
	if exposeDevTokens("production", "http://localhost:8080") {
		t.Fatal("expected production env to hide dev tokens by default")
	}
}

func TestExposeDevTokensEnvOverride(t *testing.T) {
	t.Setenv("TOKHUB_EXPOSE_DEV_TOKENS", "true")
	if !exposeDevTokens("production", "https://tokhub.example.com") {
		t.Fatal("expected explicit true override to expose dev tokens")
	}

	t.Setenv("TOKHUB_EXPOSE_DEV_TOKENS", "false")
	if exposeDevTokens("development", "http://localhost:8080") {
		t.Fatal("expected explicit false override to hide dev tokens")
	}
}

func TestLoadConfigReadsDocsDir(t *testing.T) {
	t.Setenv("TOKHUB_DOCS_DIR", "/srv/tokhub/docs")

	cfg := LoadConfig()

	if cfg.DocsDir != "/srv/tokhub/docs" {
		t.Fatalf("expected docs dir from env, got %q", cfg.DocsDir)
	}
}

func TestLoadConfigReadsSMTPURL(t *testing.T) {
	t.Setenv("SMTP_URL", "smtp://smtp.example.com:587?from=noreply@example.com")

	cfg := LoadConfig()

	if cfg.SMTPURL != "smtp://smtp.example.com:587?from=noreply@example.com" {
		t.Fatalf("SMTPURL = %q", cfg.SMTPURL)
	}
}

func TestOpenCLIBrowserConnectorIsExplicitlyEnabled(t *testing.T) {
	t.Setenv("TOKHUB_AI_OPENCLI_BROWSER_EXPERIMENTAL", "")
	t.Setenv("TOKHUB_AI_OPENCLI_BROWSER_ACK", "")
	cfg := LoadConfig()
	if cfg.AIOpenCLIBrowserEnabled {
		t.Fatal("personal browser connector was enabled without explicit configuration")
	}

	t.Setenv("TOKHUB_AI_OPENCLI_BROWSER_EXPERIMENTAL", "true")
	t.Setenv("TOKHUB_AI_OPENCLI_BROWSER_ACK", "I_ACCEPT_OPENCLI_PERSONAL_BROWSER_EXPERIMENTAL_RISK")
	cfg = LoadConfig()
	if !cfg.AIOpenCLIBrowserEnabled || cfg.AIOpenCLIBrowserTaskTimeout != 2*time.Minute {
		t.Fatalf("personal browser connector config was not loaded: %#v", cfg)
	}
	if !cfg.AIOpenCLIChatGPTEnabled || !cfg.AIOpenCLIGeminiEnabled || !cfg.AIOpenCLIDeepSeekEnabled {
		t.Fatalf("OpenCLI provider switches should default to enabled behind the global gate: %#v", cfg)
	}
	if cfg.AIOpenCLIChatGPTMinInterval != 10*time.Second ||
		cfg.AIOpenCLIGeminiMinInterval != 10*time.Second ||
		cfg.AIOpenCLIDeepSeekMinInterval != 15*time.Second {
		t.Fatalf("OpenCLI safety intervals = (%v,%v,%v)",
			cfg.AIOpenCLIChatGPTMinInterval, cfg.AIOpenCLIGeminiMinInterval, cfg.AIOpenCLIDeepSeekMinInterval)
	}
	if cfg.AIOpenCLIChatGPTHourlyLimit != 30 || cfg.AIOpenCLIGeminiHourlyLimit != 30 ||
		cfg.AIOpenCLIDeepSeekHourlyLimit != 20 || cfg.AIOpenCLIChatGPTDailyLimit != 120 ||
		cfg.AIOpenCLIGeminiDailyLimit != 120 || cfg.AIOpenCLIDeepSeekDailyLimit != 80 {
		t.Fatalf("OpenCLI safety quotas were not loaded: %#v", cfg)
	}

	t.Setenv("TOKHUB_AI_OPENCLI_BROWSER_TASK_TIMEOUT", "30s")
	cfg = LoadConfig()
	if cfg.AIOpenCLIBrowserTaskTimeout != 2*time.Minute {
		t.Fatalf("OpenCLI browser timeout below the command contract should fall back to 2m: %v", cfg.AIOpenCLIBrowserTaskTimeout)
	}
}

func TestOpenCLIBrowserProviderSwitchesAndLimitsAreConfigurable(t *testing.T) {
	t.Setenv("TOKHUB_AI_OPENCLI_DEEPSEEK_ENABLED", "false")
	t.Setenv("TOKHUB_AI_OPENCLI_DEEPSEEK_MIN_INTERVAL", "25s")
	t.Setenv("TOKHUB_AI_OPENCLI_DEEPSEEK_HOURLY_LIMIT", "8")
	t.Setenv("TOKHUB_AI_OPENCLI_DEEPSEEK_DAILY_LIMIT", "24")
	cfg := LoadConfig()
	if cfg.AIOpenCLIDeepSeekEnabled || cfg.AIOpenCLIDeepSeekMinInterval != 25*time.Second ||
		cfg.AIOpenCLIDeepSeekHourlyLimit != 8 || cfg.AIOpenCLIDeepSeekDailyLimit != 24 {
		t.Fatalf("DeepSeek OpenCLI safety config = %#v", cfg)
	}
}

func TestAdminAgentEnabledDefaultsByEnvironment(t *testing.T) {
	t.Setenv("TOKHUB_ADMIN_AGENT_ENABLED", "")
	if !adminAgentEnabled("development") {
		t.Fatal("expected admin agent support enabled in development by default")
	}
	if adminAgentEnabled("production") {
		t.Fatal("expected admin agent support disabled in production by default")
	}

	t.Setenv("TOKHUB_ADMIN_AGENT_ENABLED", "true")
	if !adminAgentEnabled("production") {
		t.Fatal("expected explicit true override to enable admin agent support")
	}

	t.Setenv("TOKHUB_ADMIN_AGENT_ENABLED", "false")
	if adminAgentEnabled("development") {
		t.Fatal("expected explicit false override to disable admin agent support")
	}
}

func TestLoadConfigNormalizesAdminUsername(t *testing.T) {
	t.Setenv("TOKHUB_ADMIN_USERNAME", " Admin.User_01! ")

	cfg := LoadConfig()

	if cfg.AdminUsername != "admin.user_01" {
		t.Fatalf("AdminUsername = %q, want admin.user_01", cfg.AdminUsername)
	}
}

func TestLoadConfigFallsBackWhenAdminUsernameIsInvalid(t *testing.T) {
	t.Setenv("TOKHUB_ADMIN_USERNAME", "管理员")

	cfg := LoadConfig()

	if cfg.AdminUsername != "admin" {
		t.Fatalf("AdminUsername = %q, want admin", cfg.AdminUsername)
	}
}

func TestLoadConfigSeedModeDefaultsByEnvironment(t *testing.T) {
	t.Setenv("TOKHUB_ENV", "development")
	t.Setenv("TOKHUB_SEED_MODE", "")
	if cfg := LoadConfig(); cfg.SeedMode != "prod" {
		t.Fatalf("development SeedMode = %q, want prod", cfg.SeedMode)
	}

	t.Setenv("TOKHUB_ENV", "production")
	t.Setenv("TOKHUB_SEED_MODE", "")
	if cfg := LoadConfig(); cfg.SeedMode != "prod" {
		t.Fatalf("production SeedMode = %q, want prod", cfg.SeedMode)
	}

	t.Setenv("TOKHUB_SEED_MODE", "test")
	if cfg := LoadConfig(); cfg.SeedMode != "test" {
		t.Fatalf("explicit SeedMode = %q, want test", cfg.SeedMode)
	}

	t.Setenv("TOKHUB_SEED_MODE", "invalid")
	if cfg := LoadConfig(); cfg.SeedMode != "prod" {
		t.Fatalf("invalid SeedMode = %q, want prod", cfg.SeedMode)
	}
}

func TestLoadConfigUpstreamModeDefaultsByEnvironment(t *testing.T) {
	t.Setenv("TOKHUB_ENV", "development")
	t.Setenv("TOKHUB_UPSTREAM_MODE", "")
	if cfg := LoadConfig(); cfg.UpstreamMode != "mock" {
		t.Fatalf("development UpstreamMode = %q, want mock", cfg.UpstreamMode)
	}

	t.Setenv("TOKHUB_ENV", "production")
	t.Setenv("TOKHUB_UPSTREAM_MODE", "")
	if cfg := LoadConfig(); cfg.UpstreamMode != "real" {
		t.Fatalf("production UpstreamMode = %q, want real", cfg.UpstreamMode)
	}

	t.Setenv("TOKHUB_UPSTREAM_MODE", "mock")
	if cfg := LoadConfig(); cfg.UpstreamMode != "mock" {
		t.Fatalf("explicit UpstreamMode = %q, want mock", cfg.UpstreamMode)
	}
}

func TestLoadConfigReadsCredentialKeyringRotationSet(t *testing.T) {
	t.Setenv("TOKHUB_CREDENTIAL_ACTIVE_KEY_ID", "enc-v2")
	t.Setenv("TOKHUB_CREDENTIAL_ENCRYPTION_KEYS", "enc-v1:11111111111111111111111111111111,enc-v2:22222222222222222222222222222222")
	t.Setenv("TOKHUB_CREDENTIAL_ACTIVE_FINGERPRINT_KEY_ID", "fp-v2")
	t.Setenv("TOKHUB_CREDENTIAL_FINGERPRINT_KEYS", "fp-v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,fp-v2:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	cfg := LoadConfig()

	if cfg.CredentialActiveKeyID != "enc-v2" || cfg.CredentialEncryptionKeys["enc-v1"] == "" {
		t.Fatalf("credential encryption keyring was not loaded: %#v", cfg.CredentialEncryptionKeys)
	}
	if cfg.CredentialActiveFingerprintKeyID != "fp-v2" || cfg.CredentialFingerprintKeys["fp-v2"] == "" {
		t.Fatalf("credential fingerprint keyring was not loaded: %#v", cfg.CredentialFingerprintKeys)
	}
}

func TestLoadConfigRequiresDedicatedCredentialKeysInProduction(t *testing.T) {
	t.Setenv("TOKHUB_ENV", "production")
	t.Setenv("TOKHUB_CREDENTIAL_ENCRYPTION_KEYS", "")
	t.Setenv("TOKHUB_CREDENTIAL_FINGERPRINT_KEYS", "")

	cfg := LoadConfig()

	if len(cfg.CredentialEncryptionKeys) != 0 || len(cfg.CredentialFingerprintKeys) != 0 {
		t.Fatalf("production config fell back to the global secret: encryption=%d fingerprint=%d", len(cfg.CredentialEncryptionKeys), len(cfg.CredentialFingerprintKeys))
	}
}

func TestLoadConfigUsesSeparatedCredentialFallbacksInDevelopment(t *testing.T) {
	t.Setenv("TOKHUB_ENV", "development")
	t.Setenv("TOKHUB_SECRET_KEY", "development-secret-material-32-bytes")
	t.Setenv("TOKHUB_CREDENTIAL_ENCRYPTION_KEYS", "")
	t.Setenv("TOKHUB_CREDENTIAL_FINGERPRINT_KEYS", "")

	cfg := LoadConfig()
	encryptionSecret := cfg.CredentialEncryptionKeys[cfg.CredentialActiveKeyID]
	fingerprintSecret := cfg.CredentialFingerprintKeys[cfg.CredentialActiveFingerprintKeyID]
	if encryptionSecret == "" || fingerprintSecret == "" || encryptionSecret == fingerprintSecret {
		t.Fatalf("development credential fallback keys were not separated")
	}
}

func TestLoadConfigKeepsWebAuthorizationOffByDefaultAndReadsExplicitProviderFlags(t *testing.T) {
	t.Setenv("TOKHUB_AI_WEB_AUTH_ENABLED", "")
	t.Setenv("TOKHUB_AI_GEMINI_OAUTH_ENABLED", "")
	t.Setenv("TOKHUB_AI_CHATGPT_CODEX_EXPERIMENTAL", "")
	t.Setenv("TOKHUB_AI_DEEPSEEK_WEB_EXPERIMENTAL", "")
	cfg := LoadConfig()
	if cfg.AIWebAuthEnabled || cfg.AIGeminiOAuthEnabled || cfg.AIChatGPTCodexExperimental || cfg.AIDeepSeekWebExperimental {
		t.Fatalf("web authorization was enabled by default: %#v", cfg)
	}
	if !cfg.AIDeepSeekGuidedEnabled {
		t.Fatal("DeepSeek official guided flow should be enabled by default")
	}

	t.Setenv("TOKHUB_AI_WEB_AUTH_ENABLED", "true")
	t.Setenv("TOKHUB_AI_GEMINI_OAUTH_ENABLED", "true")
	t.Setenv("TOKHUB_AI_CHATGPT_CODEX_EXPERIMENTAL", "true")
	t.Setenv("TOKHUB_AI_DEEPSEEK_WEB_EXPERIMENTAL", "true")
	t.Setenv("TOKHUB_AI_DEEPSEEK_WEB_BRIDGE_URL", "https://bridge.example.test")
	t.Setenv("TOKHUB_AI_DEEPSEEK_WEB_ACK", "ack")
	t.Setenv("TOKHUB_AI_OAUTH_TTL", "12m")
	t.Setenv("TOKHUB_AI_OAUTH_REFRESH_SKEW", "7m")
	t.Setenv("TOKHUB_AI_OAUTH_REFRESH_WORKERS", "5")
	t.Setenv("TOKHUB_AI_OAUTH_PROVIDER_CONCURRENCY", "3")
	t.Setenv("TOKHUB_AI_OAUTH_PROVIDER_QPS", "4")
	t.Setenv("TOKHUB_AI_OAUTH_REFRESH_ATTEMPT_TIMEOUT", "18s")
	cfg = LoadConfig()
	if !cfg.AIWebAuthEnabled || !cfg.AIGeminiOAuthEnabled || !cfg.AIChatGPTCodexExperimental || !cfg.AIDeepSeekWebExperimental {
		t.Fatalf("explicit provider flags were not loaded: %#v", cfg)
	}
	if cfg.AIDeepSeekWebBridgeURL != "https://bridge.example.test" || cfg.AIDeepSeekWebBridgeAck != "ack" {
		t.Fatalf("DeepSeek web bridge config was not loaded: %#v", cfg)
	}
	if cfg.AIOAuthTTL != 12*time.Minute || cfg.AIOAuthRefreshSkew != 7*time.Minute || cfg.AIOAuthRefreshWorkers != 5 ||
		cfg.AIOAuthProviderConcurrency != 3 || cfg.AIOAuthProviderQPS != 4 || cfg.AIOAuthRefreshAttemptTimeout != 18*time.Second {
		t.Fatalf("OAuth runtime config = ttl %s skew %s workers %d provider concurrency %d provider qps %d attempt timeout %s",
			cfg.AIOAuthTTL, cfg.AIOAuthRefreshSkew, cfg.AIOAuthRefreshWorkers,
			cfg.AIOAuthProviderConcurrency, cfg.AIOAuthProviderQPS, cfg.AIOAuthRefreshAttemptTimeout)
	}
}
