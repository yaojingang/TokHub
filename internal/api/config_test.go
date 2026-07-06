package api

import "testing"

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
