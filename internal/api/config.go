package api

import (
	"net/url"
	"os"
	"strings"
)

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
