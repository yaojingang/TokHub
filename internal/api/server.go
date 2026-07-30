package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"tokhub/internal/auth"
	"tokhub/internal/buildinfo"
	"tokhub/internal/connections"
	secretcrypto "tokhub/internal/crypto"
	gatewaycache "tokhub/internal/gateway"
	"tokhub/internal/prober"
	"tokhub/internal/store"
)

type Server struct {
	cfg            Config
	repo           *store.Repository
	auth           *auth.Service
	secretBox      *secretcrypto.SecretBox
	credentialKeys *secretcrypto.CredentialKeyring
	gatewayCache   *gatewaycache.Cache
	upstreamClient *gatewaycache.UpstreamClient
	authRegistry   *connections.AuthRegistry
	authzStore     connections.AuthorizationStore
	probeRunner    *prober.Runner
	logger         *slog.Logger
	publicLimiter  *rateLimiter
	authLimiter    *rateLimiter
	gatewayLimiter *rateLimiter
	circuitMu      sync.Mutex
	circuits       map[string]time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
}

type rateBucket struct {
	window time.Time
	count  int
}

func NewServer(cfg Config, repo *store.Repository, authSvc *auth.Service, probeRunner *prober.Runner, gatewayCache *gatewaycache.Cache, logger *slog.Logger) http.Handler {
	secretBox, err := secretcrypto.NewSecretBox(cfg.SecretKey)
	if err != nil {
		logger.Error("secret box unavailable", "error", err)
	}
	credentialKeys, err := secretcrypto.NewCredentialKeyring(secretcrypto.CredentialKeyringConfig{
		ActiveEncryptionKeyID:  cfg.CredentialActiveKeyID,
		EncryptionKeys:         cfg.CredentialEncryptionKeys,
		ActiveFingerprintKeyID: cfg.CredentialActiveFingerprintKeyID,
		FingerprintKeys:        cfg.CredentialFingerprintKeys,
	})
	if err != nil {
		logger.Error("credential keyring unavailable", "error", err)
	}
	authRegistry := connections.NewAuthRegistry(connections.AdapterConfig{
		WebAuthEnabled:           cfg.AIWebAuthEnabled,
		GeminiOAuthEnabled:       cfg.AIGeminiOAuthEnabled,
		DeepSeekGuidedEnabled:    cfg.AIDeepSeekGuidedEnabled,
		DeepSeekWebExperimental:  cfg.AIDeepSeekWebExperimental,
		DeepSeekWebBridgeURL:     cfg.AIDeepSeekWebBridgeURL,
		DeepSeekWebBridgeAck:     cfg.AIDeepSeekWebBridgeAck,
		ChatGPTCodexExperimental: cfg.AIChatGPTCodexExperimental,
		ExperimentalBridgeAck:    cfg.AIExperimentalBridgeAck,
		PublicURL:                cfg.PublicURL,
		GoogleClientID:           cfg.GoogleOAuthClientID,
		GoogleClientSecret:       cfg.GoogleOAuthClientSecret,
		GoogleProjectID:          cfg.GoogleOAuthProjectID,
	})
	var authzStore connections.AuthorizationStore
	if cfg.AIWebAuthEnabled {
		authzStore, err = connections.NewRedisAuthorizationStore(context.Background(), cfg.RedisURL)
		if err != nil {
			logger.Warn("AI authorization transaction store unavailable", "error", err)
		}
	}
	s := &Server{
		cfg:            cfg,
		repo:           repo,
		auth:           authSvc,
		secretBox:      secretBox,
		credentialKeys: credentialKeys,
		gatewayCache:   gatewayCache,
		upstreamClient: gatewaycache.NewUpstreamClient(),
		authRegistry:   authRegistry,
		authzStore:     authzStore,
		probeRunner:    probeRunner,
		logger:         logger,
		publicLimiter:  &rateLimiter{buckets: map[string]rateBucket{}},
		authLimiter:    &rateLimiter{buckets: map[string]rateBucket{}},
		gatewayLimiter: &rateLimiter{buckets: map[string]rateBucket{}},
		circuits:       map[string]time.Time{},
	}
	s.backfillNotificationChannelTargets()
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.logRequests)
	r.Use(s.cors)
	r.Use(s.securityHeaders)
	r.Use(s.csrf)

	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)
	r.Get("/metrics", s.metrics)
	r.Get("/ws/status", s.statusStream)
	r.Get("/openapi.yaml", s.openAPISpec)
	r.Get("/docs/openapi.yaml", s.openAPISpec)
	r.Get("/robots.txt", s.robotsTxt)
	r.Get("/sitemap.xml", s.sitemapXML)
	r.Get("/llms.txt", s.llmsTxt)

	r.Route("/api", func(api chi.Router) {
		api.Route("/auth", func(ar chi.Router) {
			ar.Use(s.authRateLimit)
			ar.Get("/csrf", s.csrfToken)
			ar.Post("/login", s.login)
			ar.Post("/register", s.register)
			ar.Post("/logout", s.logout)
			ar.Get("/me", s.me)
			ar.Post("/verify-email", s.verifyEmail)
			ar.Post("/forgot-password", s.forgotPassword)
			ar.Post("/reset-password", s.resetPassword)
			ar.Post("/revoke-sessions", s.revokeSessions)
		})
		api.Route("/me", func(mr chi.Router) {
			mr.Use(s.requireUser)
			mr.Patch("/profile", s.updateMeProfile)
			mr.Get("/favorites", s.meFavorites)
			mr.Post("/favorites/bulk", s.bulkFavorites)
			mr.Put("/favorites/{channelID}", s.putFavorite)
			mr.Delete("/favorites/{channelID}", s.deleteFavorite)
			mr.Get("/private-channels", s.mePrivateChannels)
			mr.Post("/private-channels/validate", s.validatePrivateChannelDraft)
			mr.Post("/private-channels", s.createPrivateChannel)
			mr.Post("/private-channels/bulk", s.bulkPrivateChannels)
			mr.Patch("/private-channels/{channelID}", s.updatePrivateChannel)
			mr.Delete("/private-channels/{channelID}", s.deletePrivateChannel)
			mr.Post("/private-channels/{channelID}/probe-now", s.probePrivateChannelNow)
			mr.Post("/private-channels/{channelID}/validate", s.validatePrivateChannel)
			mr.Get("/ai-connection-providers", s.meAIConnectionProviders)
			mr.Post("/ai-auth/step-up", s.stepUpAIConnectionAuth)
			mr.Post("/ai-authorizations", s.startAIConnectionAuthorization)
			mr.Get("/ai-authorizations/google/callback", s.googleAIAuthorizationCallback)
			mr.Get("/ai-authorizations/{authorizationID}", s.aiConnectionAuthorizationStatus)
			mr.Post("/ai-authorizations/{authorizationID}/complete", s.completeAIConnectionAuthorization)
			mr.Delete("/ai-authorizations/{authorizationID}", s.cancelAIConnectionAuthorization)
			mr.Get("/ai-connections", s.meAIConnections)
			mr.Post("/ai-connections", s.createAIConnection)
			mr.Get("/ai-browser-connectors", s.meAIBrowserConnectors)
			mr.Post("/ai-browser-connectors", s.createAIBrowserConnector)
			mr.Delete("/ai-browser-connectors/{connectorID}", s.revokeAIBrowserConnector)
			mr.Post("/ai-browser-connections", s.createAIBrowserConnection)
			mr.Get("/ai-connections/{connectionID}", s.meAIConnection)
			mr.Post("/ai-connections/{connectionID}/validate", s.validateAIConnection)
			mr.Get("/ai-connections/{connectionID}/browser-risk", s.meAIBrowserConnectionRisk)
			mr.Post("/ai-connections/{connectionID}/browser-risk/pause", s.pauseAIBrowserConnection)
			mr.Post("/ai-connections/{connectionID}/browser-risk/resume", s.resumeAIBrowserConnection)
			mr.Post("/ai-connections/{connectionID}/rotate", s.rotateAIConnectionCredential)
			mr.Post("/ai-connections/{connectionID}/quick-relay", s.quickCreateAIConnectionRelay)
			mr.Post("/ai-connections/{connectionID}/reauthorize", s.startAIConnectionAuthorization)
			mr.Post("/ai-connections/{connectionID}/disconnect", s.disconnectAIConnection)
			mr.Delete("/ai-connections/{connectionID}", s.deleteAIConnection)
		})
		api.Route("/ai-browser-connectors", func(br chi.Router) {
			br.Use(s.browserConnectorRateLimit)
			br.Post("/pair", s.pairAIBrowserConnector)
			br.Post("/heartbeat", s.heartbeatAIBrowserConnector)
			br.Post("/tasks/claim", s.claimAIBrowserConnectorTask)
			br.Post("/tasks/{taskID}/complete", s.completeAIBrowserConnectorTask)
		})
		api.Route("/public", func(pr chi.Router) {
			pr.Use(s.publicRateLimit)
			pr.Use(s.publicCacheHeaders)
			pr.Get("/overview", s.publicOverview)
			pr.Get("/channels", s.publicChannels)
			pr.Get("/channels/export", s.publicChannelsExport)
			pr.Get("/channels/{channelID}", s.publicChannel)
			pr.Get("/channels/{channelID}/series", s.publicChannelSeries)
			pr.Get("/providers/rank", s.publicProviderRank)
			pr.Get("/errors/summary", s.publicErrorsSummary)
			pr.Get("/site-config", s.siteConfig)
			pr.Get("/recommend", s.publicRecommend)
			pr.Post("/recommend/click", s.trackRecommendClick)
		})
		api.Route("/admin", func(ar chi.Router) {
			ar.Use(s.requireAdmin)
			ar.Get("/agent-tokens", s.adminAgentTokens)
			ar.Post("/agent-tokens", s.createAdminAgentToken)
			ar.Post("/agent-tokens/{tokenID}/revoke", s.revokeAdminAgentToken)
			ar.Get("/channels", s.adminChannels)
			ar.Post("/channels", s.createAdminChannel)
			ar.Post("/channels/bulk", s.bulkAdminChannels)
			ar.Post("/channels/export", s.exportAdminChannels)
			ar.Post("/channels/import", s.importAdminChannels)
			ar.Post("/channels/sync", s.syncAdminChannelsFromAPI)
			ar.Patch("/channels/{channelID}", s.updateAdminChannel)
			ar.Post("/channels/{channelID}/intro-fetch", s.fetchAdminChannelIntro)
			ar.Post("/channels/{channelID}/credentials", s.rotateAdminChannelCredential)
			ar.Post("/channels/{channelID}/validate", s.validateAdminChannel)
			ar.Post("/channels/{channelID}/disable", s.disableAdminChannel)
			ar.Post("/channels/{channelID}/recommend", s.addAdminChannelRecommendation)
			ar.Delete("/channels/{channelID}/recommend", s.removeAdminChannelRecommendation)
			ar.Delete("/channels/{channelID}", s.deleteAdminChannel)
			ar.Post("/channels/{channelID}/probe-now", s.adminProbeNow)
			ar.Get("/settings", s.adminSettings)
			ar.Patch("/settings", s.updateAdminSettings)
			ar.Get("/users", s.adminUsers)
			ar.Post("/users", s.createAdminUser)
			ar.Patch("/users/{userID}", s.patchAdminUser)
			ar.Patch("/users/{userID}/status", s.patchAdminUserStatus)
			ar.Delete("/users/{userID}", s.deleteAdminUser)
			ar.Post("/users/bulk", s.bulkAdminUsers)
			ar.Get("/orgs", s.adminOrgs)
			ar.Post("/orgs", s.createAdminOrg)
			ar.Patch("/orgs/{orgID}", s.patchAdminOrg)
			ar.Patch("/orgs/{orgID}/status", s.patchAdminOrgStatus)
			ar.Delete("/orgs/{orgID}", s.deleteAdminOrg)
			ar.Post("/orgs/bulk", s.bulkAdminOrgs)
			ar.Get("/gateways", s.adminGateways)
			ar.Post("/gateways", s.createGateway)
			ar.Post("/gateways/bulk", s.bulkAdminGateways)
			ar.Patch("/gateways/{gatewayID}", s.patchGateway)
			ar.Delete("/gateways/{gatewayID}", s.deleteGateway)
			ar.Get("/gateway-keys", s.adminGatewayKeys)
			ar.Post("/gateway-keys", s.createGatewayKey)
			ar.Post("/gateway-keys/bulk", s.bulkAdminGatewayKeys)
			ar.Patch("/gateway-keys/{keyID}", s.patchGatewayKey)
			ar.Post("/gateway-keys/{keyID}/revoke", s.revokeGatewayKey)
			ar.Delete("/gateway-keys/{keyID}", s.deleteGatewayKey)
			ar.Get("/members", s.adminMembers)
			ar.Post("/members", s.inviteAdminMember)
			ar.Post("/members/bulk", s.bulkAdminMembers)
			ar.Patch("/members/{userID}", s.patchAdminMember)
			ar.Delete("/members/{userID}", s.deleteAdminMember)
			ar.Get("/usage", s.adminUsage)
			ar.Get("/usage/export", s.exportAdminUsage)
			ar.Post("/usage/rollup/recompute", s.recomputeAdminUsageRollup)
			ar.Get("/alerts", s.adminAlertCenter)
			ar.Post("/alerts/rules", s.createAdminAlertRule)
			ar.Post("/alerts/rules/bulk", s.bulkAdminAlertRules)
			ar.Patch("/alerts/rules/{ruleID}", s.patchAdminAlertRule)
			ar.Delete("/alerts/rules/{ruleID}", s.deleteAdminAlertRule)
			ar.Post("/alerts/evaluate", s.evaluateAdminAlerts)
			ar.Post("/alerts/channels", s.createAdminNotificationChannel)
			ar.Post("/alerts/channels/bulk", s.bulkAdminNotificationChannels)
			ar.Patch("/alerts/channels/{channelID}", s.patchAdminNotificationChannel)
			ar.Post("/alerts/channels/{channelID}/test", s.testAdminNotificationChannel)
			ar.Delete("/alerts/channels/{channelID}", s.deleteAdminNotificationChannel)
			ar.Get("/audit", s.adminAuditLogs)
			ar.Get("/audit/export", s.exportAdminAudit)
			ar.Get("/audit/{auditID}", s.adminAuditDetail)
			ar.Get("/probe-logs", s.adminProbeLogs)
			ar.Get("/probe-logs/export", s.exportAdminProbeLogs)
			ar.Get("/probe-logs/{runID}", s.adminProbeLogDetail)
			ar.Get("/incidents", s.adminIncidents)
			ar.Post("/incidents", s.createAdminIncident)
			ar.Post("/incidents/bulk", s.bulkAdminIncidents)
			ar.Patch("/incidents/{incidentID}", s.patchAdminIncident)
			ar.Post("/incidents/{incidentID}/resolve", s.resolveAdminIncident)
			ar.Post("/incidents/{incidentID}/reopen", s.reopenAdminIncident)
			ar.Delete("/incidents/{incidentID}", s.deleteAdminIncident)
			ar.Get("/governance/summary", s.adminGovernanceSummary)
			ar.Get("/recommend", s.adminRecommend)
			ar.Put("/recommend", s.saveAdminRecommend)
			ar.Get("/open-api", s.adminOpenAPI)
			ar.Post("/open-api/sites", s.createOpenAPISite)
			ar.Post("/open-api/sites/bulk", s.bulkOpenAPISites)
			ar.Patch("/open-api/sites/{siteID}", s.patchOpenAPISite)
			ar.Post("/open-api/sites/{siteID}/revoke", s.revokeOpenAPISite)
			ar.Delete("/open-api/sites/{siteID}", s.deleteOpenAPISite)
			ar.Get("/channel-sites", s.adminChannelSites)
			ar.Post("/channel-sites", s.createChannelSite)
			ar.Patch("/channel-sites/{siteID}", s.patchChannelSite)
			ar.Delete("/channel-sites/{siteID}", s.deleteChannelSite)
			ar.Post("/channel-sites/{siteID}/rotate-key", s.rotateChannelSiteKey)
			ar.Post("/channel-sites/{siteID}/build", s.buildChannelSitePackage)
			ar.Get("/channel-sites/{siteID}/download", s.downloadChannelSitePackage)
			ar.Get("/web", s.adminWebConfig)
			ar.Patch("/web", s.saveAdminWebConfig)
			ar.Post("/web/reset", s.resetAdminWebConfig)
			ar.Get("/production-health", s.adminProductionHealth)
		})
		api.Route("/console", func(cr chi.Router) {
			cr.Use(s.requireUser)
			cr.Get("/settings", s.consoleSettings)
			cr.Patch("/settings", s.updateConsoleSettings)
			cr.Post("/settings/reset", s.resetConsoleSettings)
			cr.Get("/gateways", s.consoleGateways)
			cr.Post("/gateways", s.createConsoleGateway)
			cr.Post("/gateways/bulk", s.bulkConsoleGateways)
			cr.Patch("/gateways/{gatewayID}", s.patchConsoleGateway)
			cr.Delete("/gateways/{gatewayID}", s.deleteConsoleGateway)
			cr.Post("/gateways/{gatewayID}/debug", s.debugConsoleGateway)
			cr.Get("/gateway-keys", s.consoleGatewayKeys)
			cr.Post("/gateway-keys", s.createConsoleGatewayKey)
			cr.Post("/gateway-keys/bulk", s.bulkConsoleGatewayKeys)
			cr.Patch("/gateway-keys/{keyID}", s.patchConsoleGatewayKey)
			cr.Post("/gateway-keys/{keyID}/revoke", s.revokeConsoleGatewayKey)
			cr.Delete("/gateway-keys/{keyID}", s.deleteConsoleGatewayKey)
			cr.Get("/members", s.consoleMembers)
			cr.Post("/members", s.inviteConsoleMember)
			cr.Post("/members/bulk", s.bulkConsoleMembers)
			cr.Patch("/members/{userID}", s.patchConsoleMember)
			cr.Delete("/members/{userID}", s.deleteConsoleMember)
			cr.Get("/usage", s.consoleUsage)
			cr.Get("/usage/export", s.exportConsoleUsage)
			cr.Post("/usage/rollup/recompute", s.recomputeConsoleUsageRollup)
			cr.Get("/alerts", s.consoleAlertCenter)
			cr.Post("/alerts/rules", s.createConsoleAlertRule)
			cr.Post("/alerts/rules/bulk", s.bulkConsoleAlertRules)
			cr.Patch("/alerts/rules/{ruleID}", s.patchConsoleAlertRule)
			cr.Delete("/alerts/rules/{ruleID}", s.deleteConsoleAlertRule)
			cr.Post("/alerts/evaluate", s.evaluateConsoleAlerts)
			cr.Post("/alerts/channels", s.createConsoleNotificationChannel)
			cr.Post("/alerts/channels/bulk", s.bulkConsoleNotificationChannels)
			cr.Patch("/alerts/channels/{channelID}", s.patchConsoleNotificationChannel)
			cr.Post("/alerts/channels/{channelID}/test", s.testConsoleNotificationChannel)
			cr.Delete("/alerts/channels/{channelID}", s.deleteConsoleNotificationChannel)
			cr.Get("/audit", s.consoleAuditLogs)
			cr.Get("/audit/export", s.exportConsoleAudit)
			cr.Get("/audit/{auditID}", s.consoleAuditDetail)
			cr.Get("/incidents", s.consoleIncidents)
			cr.Post("/incidents", s.createConsoleIncident)
			cr.Post("/incidents/bulk", s.bulkConsoleIncidents)
			cr.Patch("/incidents/{incidentID}", s.patchConsoleIncident)
			cr.Post("/incidents/{incidentID}/resolve", s.resolveConsoleIncident)
			cr.Post("/incidents/{incidentID}/reopen", s.reopenConsoleIncident)
			cr.Delete("/incidents/{incidentID}", s.deleteConsoleIncident)
			cr.Get("/governance/summary", s.consoleGovernanceSummary)
		})
	})

	r.Route("/gateway/v1", func(gr chi.Router) {
		gr.Get("/models", s.gatewayModels)
		gr.Get("/v1/models", s.gatewayModels)
		gr.Post("/chat/completions", s.gatewayChatCompletions)
		gr.Post("/responses", s.gatewayResponses)
		gr.Post("/messages", s.gatewayAnthropicMessages)
		gr.Post("/v1/messages", s.gatewayAnthropicMessages)
	})

	r.Route("/v1/status", func(vr chi.Router) {
		vr.Get("/channels", s.openAPIChannels)
		vr.Get("/channels/{channelID}", s.openAPIChannel)
		vr.Get("/channel-sync", s.openAPIChannelSync)
		vr.Get("/uptime", s.openAPIUptime)
		vr.Get("/incidents", s.openAPIIncidents)
		vr.Get("/overview", s.openAPIOverview)
	})

	r.Route("/site/v1/{siteID}", func(sr chi.Router) {
		sr.Get("/config", s.channelSiteConfig)
		sr.Get("/overview", s.channelSiteOverview)
		sr.Get("/channels", s.channelSiteChannels)
		sr.Get("/recommend", s.channelSiteRecommend)
	})

	r.NotFound(s.frontend)
	r.Get("/*", s.frontend)
	return r
}

func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	if origin := originFromURL(s.cfg.PublicURL); origin != "" {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if strings.HasPrefix(r.URL.Path, "/site/v1/") {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Channel-Site-Key, X-Site-Key")
				w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				w.Header().Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if origin != "" {
			if !allowed[origin] && origin != requestOrigin(r) {
				writeError(w, r, http.StatusForbidden, "cors_forbidden", "Origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Site-Key, X-TokHub-Agent-Reason, X-Idempotency-Key")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	if r.Host == "" {
		return ""
	}
	return scheme + "://" + r.Host
}

func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicRecommendClick := r.Method == http.MethodPost && r.URL.Path == "/api/public/recommend/click"
		localBrowserConnector := strings.HasPrefix(r.URL.Path, "/api/ai-browser-connectors/")
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || !strings.HasPrefix(r.URL.Path, "/api/") || publicRecommendClick || localBrowserConnector {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/admin/") && isAdminAgentAuthorization(r) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(auth.CSRFCookieName)
		if err != nil || cookie.Value == "" || r.Header.Get("X-CSRF-Token") != cookie.Value {
			writeError(w, r, http.StatusForbidden, "csrf_invalid", "CSRF token is missing or invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		if !s.allowRate(s.authLimiter, clientIP(r), 30, time.Minute) {
			writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many auth requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) browserConnectorRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowRate(s.authLimiter, "browser-connector-device:"+clientIP(r), 600, time.Minute) {
			writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many local browser connector requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) publicCacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path == "/api/public/site-config" || r.URL.Path == "/api/public/recommend" || strings.HasPrefix(r.URL.Path, "/api/public/channels") {
				w.Header().Set("Cache-Control", "no-store")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=10")
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) publicRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowRate(s.publicLimiter, clientIP(r), 240, time.Minute) {
			writeError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many public API requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowRate(limiter *rateLimiter, key string, limit int, window time.Duration) bool {
	now := time.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	bucket := limiter.buckets[key]
	if now.Sub(bucket.window) > window {
		bucket = rateBucket{window: now}
	}
	bucket.count++
	limiter.buckets[key] = bucket
	return bucket.count <= limit
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return r.RemoteAddr
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) csrfToken(w http.ResponseWriter, r *http.Request) {
	token := auth.NewCSRFToken()
	http.SetCookie(w, s.auth.CSRFCookie(token))
	writeJSON(w, http.StatusOK, map[string]string{"csrfToken": token})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": buildinfo.Version})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.credentialKeys == nil {
		writeError(w, r, http.StatusServiceUnavailable, "credential_vault_unavailable", "Credential vault is not ready")
		return
	}
	if err := s.repo.Ping(r.Context()); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "Database is not ready")
		return
	}
	if s.cfg.AIWebAuthEnabled && s.authzStore == nil {
		writeError(w, r, http.StatusServiceUnavailable, "authorization_store_unavailable", "AI authorization store is not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	snapshot, err := s.repo.MetricsSnapshot(r.Context())
	if err != nil {
		s.logger.Warn("metrics snapshot unavailable", "error", err)
	}
	authSnapshot, authErr := s.repo.AIAuthorizationMetrics(r.Context())
	if authErr != nil {
		s.logger.Warn("AI authorization metrics unavailable", "error", authErr)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# HELP tokhub_build_info TokHub build info\n# TYPE tokhub_build_info gauge\ntokhub_build_info{role=%q} 1\n", s.cfg.Role)
	fmt.Fprintf(&out, "# HELP tokhub_gateway_requests_total Gateway requests recorded\n# TYPE tokhub_gateway_requests_total counter\ntokhub_gateway_requests_total %d\n", snapshot.GatewayRequests)
	fmt.Fprintf(&out, "# HELP tokhub_gateway_errors_total Gateway requests with HTTP status >= 400\n# TYPE tokhub_gateway_errors_total counter\ntokhub_gateway_errors_total %d\n", snapshot.GatewayErrors)
	fmt.Fprintf(&out, "# HELP tokhub_probe_runs_total Probe runs recorded\n# TYPE tokhub_probe_runs_total counter\ntokhub_probe_runs_total %d\n", snapshot.ProbeRuns)
	fmt.Fprintf(&out, "# HELP tokhub_incidents_open Open incidents\n# TYPE tokhub_incidents_open gauge\ntokhub_incidents_open %d\n", snapshot.OpenIncidents)
	fmt.Fprintf(&out, "# HELP tokhub_alert_deliveries_total Alert delivery events\n# TYPE tokhub_alert_deliveries_total counter\ntokhub_alert_deliveries_total %d\n", snapshot.AlertDeliveries)
	fmt.Fprintf(&out, "# HELP tokhub_audit_events_total Audit events\n# TYPE tokhub_audit_events_total counter\ntokhub_audit_events_total %d\n", snapshot.AuditEvents)
	fmt.Fprintf(&out, "# HELP tokhub_usage_rollups_total Usage rollup rows\n# TYPE tokhub_usage_rollups_total gauge\ntokhub_usage_rollups_total %d\n", snapshot.UsageRollups)
	fmt.Fprintf(&out, "# HELP tokhub_ai_connections_active Active official developer credential connections\n# TYPE tokhub_ai_connections_active gauge\ntokhub_ai_connections_active %d\n", snapshot.AIConnectionsActive)
	fmt.Fprintf(&out, "# HELP tokhub_ai_connections_attention Official developer credential connections requiring attention\n# TYPE tokhub_ai_connections_attention gauge\ntokhub_ai_connections_attention %d\n", snapshot.AIConnectionsAttention)
	fmt.Fprintf(&out, "# HELP tokhub_ai_connection_validations_total AI connection validation attempts\n# TYPE tokhub_ai_connection_validations_total counter\ntokhub_ai_connection_validations_total %d\n", snapshot.AIConnectionValidations)
	fmt.Fprintf(&out, "# HELP tokhub_ai_connection_validation_failures_total Failed AI connection validation attempts\n# TYPE tokhub_ai_connection_validation_failures_total counter\ntokhub_ai_connection_validation_failures_total %d\n", snapshot.AIConnectionValidationFailure)
	fmt.Fprintf(&out, "# HELP tokhub_ai_quick_relays_total Completed personal relays created from AI connections\n# TYPE tokhub_ai_quick_relays_total counter\ntokhub_ai_quick_relays_total %d\n", snapshot.AIQuickRelays)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_connectors_online Local browser connectors seen in the last 45 seconds\n# TYPE tokhub_ai_browser_connectors_online gauge\ntokhub_ai_browser_connectors_online %d\n", snapshot.AIBrowserConnectorsOnline)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_tasks_completed_total Completed local browser tasks\n# TYPE tokhub_ai_browser_tasks_completed_total counter\ntokhub_ai_browser_tasks_completed_total %d\n", snapshot.AIBrowserTasksCompleted)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_tasks_failed_total Failed local browser tasks\n# TYPE tokhub_ai_browser_tasks_failed_total counter\ntokhub_ai_browser_tasks_failed_total %d\n", snapshot.AIBrowserTasksFailed)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_tasks_expired_total Expired local browser tasks\n# TYPE tokhub_ai_browser_tasks_expired_total counter\ntokhub_ai_browser_tasks_expired_total %d\n", snapshot.AIBrowserTasksExpired)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_security_challenges_total Local browser tasks stopped by a security challenge\n# TYPE tokhub_ai_browser_security_challenges_total counter\ntokhub_ai_browser_security_challenges_total %d\n", snapshot.AIBrowserSecurityChallenges)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_accounts_cooling Local browser accounts in cooldown\n# TYPE tokhub_ai_browser_accounts_cooling gauge\ntokhub_ai_browser_accounts_cooling %d\n", snapshot.AIBrowserAccountsCooling)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_accounts_locked Local browser accounts locked by a security challenge\n# TYPE tokhub_ai_browser_accounts_locked gauge\ntokhub_ai_browser_accounts_locked %d\n", snapshot.AIBrowserAccountsLocked)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_accounts_reauth Local browser accounts requiring login recognition\n# TYPE tokhub_ai_browser_accounts_reauth gauge\ntokhub_ai_browser_accounts_reauth %d\n", snapshot.AIBrowserAccountsReauth)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_accounts_paused Local browser accounts paused by their owner\n# TYPE tokhub_ai_browser_accounts_paused gauge\ntokhub_ai_browser_accounts_paused %d\n", snapshot.AIBrowserAccountsPaused)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_adapters_blocked Local browser accounts blocked by adapter incompatibility\n# TYPE tokhub_ai_browser_adapters_blocked gauge\ntokhub_ai_browser_adapters_blocked %d\n", snapshot.AIBrowserAdaptersBlocked)
	fmt.Fprintf(&out, "# HELP tokhub_ai_browser_rate_limit_events_current Provider rate-limit events in current account safety windows\n# TYPE tokhub_ai_browser_rate_limit_events_current gauge\ntokhub_ai_browser_rate_limit_events_current %d\n", snapshot.AIBrowserRateLimitEvents)
	out.WriteString("# HELP tokhub_ai_authorization_attempts AI account authorization attempts by current state\n# TYPE tokhub_ai_authorization_attempts gauge\n")
	for _, item := range authSnapshot.Attempts {
		fmt.Fprintf(&out, "tokhub_ai_authorization_attempts{provider=%q,method=%q,status=%q} %d\n", item.Provider, item.Method, item.Status, item.Count)
	}
	out.WriteString("# HELP tokhub_ai_oauth_connections OAuth-backed AI connections by authorization state\n# TYPE tokhub_ai_oauth_connections gauge\n")
	for _, item := range authSnapshot.Connections {
		fmt.Fprintf(&out, "tokhub_ai_oauth_connections{provider=%q,method=%q,auth_status=%q} %d\n", item.Provider, item.Method, item.AuthStatus, item.Count)
	}
	out.WriteString("# HELP tokhub_ai_oauth_refresh_failures_current Consecutive OAuth refresh failures on current credentials\n# TYPE tokhub_ai_oauth_refresh_failures_current gauge\n")
	for _, item := range authSnapshot.RefreshFailures {
		fmt.Fprintf(&out, "tokhub_ai_oauth_refresh_failures_current{provider=%q} %d\n", item.Provider, item.Count)
	}
	_, _ = w.Write([]byte(out.String()))
}

func (s *Server) openAPISpec(w http.ResponseWriter, r *http.Request) {
	file := filepath.Join(s.cfg.DocsDir, "openapi.yaml")
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		writeError(w, r, http.StatusNotFound, "openapi_not_found", "OpenAPI spec is not available")
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	http.ServeFile(w, r, file)
}

func (s *Server) siteConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.repo.SiteConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "site_config_unavailable", "Could not load site config")
		return
	}
	writeJSON(w, http.StatusOK, publicSiteConfig(cfg))
}

func publicSiteConfig(cfg store.SiteConfig) store.SiteConfig {
	cfg.AnalyticsCode = ""
	return cfg
}

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/gateway/") || strings.HasPrefix(r.URL.Path, "/site/") {
		writeError(w, r, http.StatusNotFound, "not_found", "Route not found")
		return
	}
	path := filepath.Clean(r.URL.Path)
	if path == "." || path == "/" {
		path = "/index.html"
	}
	file := filepath.Join(s.cfg.StaticDir, strings.TrimPrefix(path, "/"))
	if strings.Contains(path, "..") {
		writeError(w, r, http.StatusBadRequest, "bad_path", "Invalid path")
		return
	}
	serveIndex := path == "/index.html"
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		file = filepath.Join(s.cfg.StaticDir, "index.html")
		serveIndex = true
	}
	if serveIndex {
		s.frontendIndex(w, r, file)
		return
	}
	http.ServeFile(w, r, file)
}

func decodeJSON(r *http.Request, target any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":      code,
			"message":   message,
			"requestId": middleware.GetReqID(r.Context()),
		},
	})
}

func originFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
