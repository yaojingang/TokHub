package api

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const openCLIBrowserDeploymentAcknowledgement = "I_ACCEPT_OPENCLI_PERSONAL_BROWSER_EXPERIMENTAL_RISK"

type Config struct {
	Env                              string
	Role                             string
	Port                             string
	PublicURL                        string
	DatabaseURL                      string
	RedisURL                         string
	NATSURL                          string
	SMTPURL                          string
	SecretKey                        string
	AdminEmail                       string
	AdminUsername                    string
	AdminPassword                    string
	SeedMode                         string
	UpstreamMode                     string
	StaticDir                        string
	DocsDir                          string
	MigrationsDir                    string
	SessionSecure                    bool
	RegistrationOpen                 bool
	ExposeDevTokens                  bool
	AdminAgentEnabled                bool
	CredentialActiveKeyID            string
	CredentialEncryptionKeys         map[string]string
	CredentialActiveFingerprintKeyID string
	CredentialFingerprintKeys        map[string]string
	AIWebAuthEnabled                 bool
	AIGeminiOAuthEnabled             bool
	AIChatGPTCodexExperimental       bool
	AIDeepSeekGuidedEnabled          bool
	AIDeepSeekWebExperimental        bool
	AIDeepSeekWebBridgeURL           string
	AIDeepSeekWebBridgeAck           string
	AIOpenCLIBrowserEnabled          bool
	AIOpenCLIBrowserAck              string
	AIOpenCLIBrowserTaskTimeout      time.Duration
	AIOpenCLIChatGPTEnabled          bool
	AIOpenCLIGeminiEnabled           bool
	AIOpenCLIDeepSeekEnabled         bool
	AIOpenCLIChatGPTMinInterval      time.Duration
	AIOpenCLIGeminiMinInterval       time.Duration
	AIOpenCLIDeepSeekMinInterval     time.Duration
	AIOpenCLIChatGPTHourlyLimit      int
	AIOpenCLIGeminiHourlyLimit       int
	AIOpenCLIDeepSeekHourlyLimit     int
	AIOpenCLIChatGPTDailyLimit       int
	AIOpenCLIGeminiDailyLimit        int
	AIOpenCLIDeepSeekDailyLimit      int
	AIOAuthTTL                       time.Duration
	AIOAuthRefreshSkew               time.Duration
	AIOAuthRefreshWorkers            int
	AIOAuthProviderConcurrency       int
	AIOAuthProviderQPS               int
	AIOAuthRefreshAttemptTimeout     time.Duration
	GoogleOAuthClientID              string
	GoogleOAuthClientSecret          string
	GoogleOAuthProjectID             string
	AIExperimentalBridgeAck          string
}

func LoadConfig() Config {
	env := getEnv("TOKHUB_ENV", "development")
	publicURL := getEnv("TOKHUB_PUBLIC_URL", "http://localhost:8080")
	secretKey := getEnv("TOKHUB_SECRET_KEY", "dev-only-change-this-secret-key-32b")
	credentialActiveKeyID := getEnv("TOKHUB_CREDENTIAL_ACTIVE_KEY_ID", "legacy-v1")
	credentialEncryptionKeys := parseCredentialKeys(os.Getenv("TOKHUB_CREDENTIAL_ENCRYPTION_KEYS"))
	if len(credentialEncryptionKeys) == 0 && !strings.EqualFold(env, "production") {
		credentialEncryptionKeys[credentialActiveKeyID] = secretKey + ":credential-encryption"
	}
	credentialActiveFingerprintKeyID := getEnv("TOKHUB_CREDENTIAL_ACTIVE_FINGERPRINT_KEY_ID", "legacy-v1")
	credentialFingerprintKeys := parseCredentialKeys(os.Getenv("TOKHUB_CREDENTIAL_FINGERPRINT_KEYS"))
	if len(credentialFingerprintKeys) == 0 && !strings.EqualFold(env, "production") {
		credentialFingerprintKeys[credentialActiveFingerprintKeyID] = secretKey + ":credential-fingerprint"
	}
	opencliBrowserAck := getEnv("TOKHUB_AI_OPENCLI_BROWSER_ACK", "")
	opencliBrowserEnabled := envBool("TOKHUB_AI_OPENCLI_BROWSER_EXPERIMENTAL", false) &&
		opencliBrowserAck == openCLIBrowserDeploymentAcknowledgement
	return Config{
		Env:                              env,
		Role:                             getEnv("TOKHUB_ROLE", "all"),
		Port:                             getEnv("PORT", "8080"),
		PublicURL:                        publicURL,
		DatabaseURL:                      getEnv("DATABASE_URL", "postgres://tokhub:tokhub@localhost:5432/tokhub?sslmode=disable"),
		RedisURL:                         getEnv("REDIS_URL", "redis://localhost:6379/0"),
		NATSURL:                          getEnv("NATS_URL", "nats://localhost:4222"),
		SMTPURL:                          getEnv("SMTP_URL", ""),
		SecretKey:                        secretKey,
		AdminEmail:                       getEnv("TOKHUB_ADMIN_EMAIL", "admin@tokhub.local"),
		AdminUsername:                    normalizeLoginUsername(getEnv("TOKHUB_ADMIN_USERNAME", "admin")),
		AdminPassword:                    getEnv("TOKHUB_ADMIN_PASSWORD", "admin@tokhub.local"),
		SeedMode:                         seedMode(env),
		UpstreamMode:                     upstreamMode(env),
		StaticDir:                        getEnv("TOKHUB_STATIC_DIR", "web/dist"),
		DocsDir:                          getEnv("TOKHUB_DOCS_DIR", "docs"),
		MigrationsDir:                    getEnv("TOKHUB_MIGRATIONS_DIR", "db/migrations"),
		SessionSecure:                    strings.EqualFold(getEnv("TOKHUB_SESSION_SECURE", "false"), "true"),
		RegistrationOpen:                 strings.EqualFold(getEnv("TOKHUB_REGISTRATION_OPEN", "true"), "true"),
		ExposeDevTokens:                  exposeDevTokens(env, publicURL),
		AdminAgentEnabled:                adminAgentEnabled(env),
		CredentialActiveKeyID:            credentialActiveKeyID,
		CredentialEncryptionKeys:         credentialEncryptionKeys,
		CredentialActiveFingerprintKeyID: credentialActiveFingerprintKeyID,
		CredentialFingerprintKeys:        credentialFingerprintKeys,
		AIWebAuthEnabled:                 envBool("TOKHUB_AI_WEB_AUTH_ENABLED", false),
		AIGeminiOAuthEnabled:             envBool("TOKHUB_AI_GEMINI_OAUTH_ENABLED", false),
		AIChatGPTCodexExperimental:       envBool("TOKHUB_AI_CHATGPT_CODEX_EXPERIMENTAL", false),
		AIDeepSeekGuidedEnabled:          envBool("TOKHUB_AI_DEEPSEEK_GUIDED_ENABLED", true),
		AIDeepSeekWebExperimental:        envBool("TOKHUB_AI_DEEPSEEK_WEB_EXPERIMENTAL", false),
		AIDeepSeekWebBridgeURL:           getEnv("TOKHUB_AI_DEEPSEEK_WEB_BRIDGE_URL", "http://deepseek-web-bridge:5001"),
		AIDeepSeekWebBridgeAck:           getEnv("TOKHUB_AI_DEEPSEEK_WEB_ACK", ""),
		AIOpenCLIBrowserEnabled:          opencliBrowserEnabled,
		AIOpenCLIBrowserAck:              opencliBrowserAck,
		AIOpenCLIBrowserTaskTimeout:      envDuration("TOKHUB_AI_OPENCLI_BROWSER_TASK_TIMEOUT", 2*time.Minute, 2*time.Minute, 5*time.Minute),
		AIOpenCLIChatGPTEnabled:          envBool("TOKHUB_AI_OPENCLI_CHATGPT_ENABLED", true),
		AIOpenCLIGeminiEnabled:           envBool("TOKHUB_AI_OPENCLI_GEMINI_ENABLED", true),
		AIOpenCLIDeepSeekEnabled:         envBool("TOKHUB_AI_OPENCLI_DEEPSEEK_ENABLED", true),
		AIOpenCLIChatGPTMinInterval:      envDuration("TOKHUB_AI_OPENCLI_CHATGPT_MIN_INTERVAL", 10*time.Second, 5*time.Second, 5*time.Minute),
		AIOpenCLIGeminiMinInterval:       envDuration("TOKHUB_AI_OPENCLI_GEMINI_MIN_INTERVAL", 10*time.Second, 5*time.Second, 5*time.Minute),
		AIOpenCLIDeepSeekMinInterval:     envDuration("TOKHUB_AI_OPENCLI_DEEPSEEK_MIN_INTERVAL", 15*time.Second, 5*time.Second, 5*time.Minute),
		AIOpenCLIChatGPTHourlyLimit:      envInt("TOKHUB_AI_OPENCLI_CHATGPT_HOURLY_LIMIT", 30, 1, 1000),
		AIOpenCLIGeminiHourlyLimit:       envInt("TOKHUB_AI_OPENCLI_GEMINI_HOURLY_LIMIT", 30, 1, 1000),
		AIOpenCLIDeepSeekHourlyLimit:     envInt("TOKHUB_AI_OPENCLI_DEEPSEEK_HOURLY_LIMIT", 20, 1, 1000),
		AIOpenCLIChatGPTDailyLimit:       envInt("TOKHUB_AI_OPENCLI_CHATGPT_DAILY_LIMIT", 120, 1, 10000),
		AIOpenCLIGeminiDailyLimit:        envInt("TOKHUB_AI_OPENCLI_GEMINI_DAILY_LIMIT", 120, 1, 10000),
		AIOpenCLIDeepSeekDailyLimit:      envInt("TOKHUB_AI_OPENCLI_DEEPSEEK_DAILY_LIMIT", 80, 1, 10000),
		AIOAuthTTL:                       envDuration("TOKHUB_AI_OAUTH_TTL", 10*time.Minute, time.Minute, 30*time.Minute),
		AIOAuthRefreshSkew:               envDuration("TOKHUB_AI_OAUTH_REFRESH_SKEW", 5*time.Minute, time.Minute, 30*time.Minute),
		AIOAuthRefreshWorkers:            envInt("TOKHUB_AI_OAUTH_REFRESH_WORKERS", 8, 1, 64),
		AIOAuthProviderConcurrency:       envInt("TOKHUB_AI_OAUTH_PROVIDER_CONCURRENCY", 4, 1, 32),
		AIOAuthProviderQPS:               envInt("TOKHUB_AI_OAUTH_PROVIDER_QPS", 2, 1, 100),
		AIOAuthRefreshAttemptTimeout:     envDuration("TOKHUB_AI_OAUTH_REFRESH_ATTEMPT_TIMEOUT", 20*time.Second, 5*time.Second, 2*time.Minute),
		GoogleOAuthClientID:              getEnv("TOKHUB_GOOGLE_OAUTH_CLIENT_ID", ""),
		GoogleOAuthClientSecret:          getEnv("TOKHUB_GOOGLE_OAUTH_CLIENT_SECRET", ""),
		GoogleOAuthProjectID:             getEnv("TOKHUB_GOOGLE_OAUTH_PROJECT_ID", ""),
		AIExperimentalBridgeAck:          getEnv("TOKHUB_AI_EXPERIMENTAL_BRIDGE_ACK", ""),
	}
}

func parseCredentialKeys(raw string) map[string]string {
	keys := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		id, secret, ok := strings.Cut(strings.TrimSpace(pair), ":")
		id = strings.TrimSpace(id)
		secret = strings.TrimSpace(secret)
		if !ok || id == "" || secret == "" {
			continue
		}
		keys[id] = secret
	}
	return keys
}

func adminAgentEnabled(env string) bool {
	if raw := strings.TrimSpace(os.Getenv("TOKHUB_ADMIN_AGENT_ENABLED")); raw != "" {
		return strings.EqualFold(raw, "true")
	}
	return !strings.EqualFold(env, "production")
}

func seedMode(env string) string {
	raw := strings.ToLower(strings.TrimSpace(getEnv("TOKHUB_SEED_MODE", "")))
	switch raw {
	case "prod", "demo", "test":
		return raw
	case "":
		return "prod"
	default:
		return "prod"
	}
}

func upstreamMode(env string) string {
	raw := strings.ToLower(strings.TrimSpace(getEnv("TOKHUB_UPSTREAM_MODE", "")))
	switch raw {
	case "real", "mock":
		return raw
	case "":
		if strings.EqualFold(env, "production") {
			return "real"
		}
		return "mock"
	default:
		return "mock"
	}
}

func exposeDevTokens(env string, publicURL string) bool {
	if raw := strings.TrimSpace(os.Getenv("TOKHUB_EXPOSE_DEV_TOKENS")); raw != "" {
		return strings.EqualFold(raw, "true")
	}
	return strings.EqualFold(env, "development") && isLocalPublicURL(publicURL)
}

func isLocalPublicURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func getEnv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration, min time.Duration, max time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < min || value > max {
		return fallback
	}
	return value
}

func envInt(key string, fallback int, min int, max int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return fallback
	}
	return value
}

func normalizeLoginUsername(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			out.WriteRune(r)
		}
	}
	if out.Len() == 0 {
		return "admin"
	}
	return out.String()
}
