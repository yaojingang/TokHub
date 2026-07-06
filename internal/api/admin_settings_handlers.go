package api

import (
	"net/http"

	"tokhub/internal/store"
)

type adminSettingsRequest struct {
	RegistrationOpen          *bool                       `json:"registrationOpen"`
	ShowRegisterCTA           *bool                       `json:"showRegisterCta"`
	EmailVerificationRequired *bool                       `json:"emailVerificationRequired"`
	BrandName                 *string                     `json:"brandName"`
	LogoMark                  *string                     `json:"logoMark"`
	Subtitle                  *string                     `json:"subtitle"`
	FooterText                *string                     `json:"footerText"`
	DefaultGatewayPolicy      *string                     `json:"defaultGatewayPolicy"`
	Timezone                  *string                     `json:"timezone"`
	MonitorModels             *[]store.MonitorModelConfig `json:"monitorModels"`
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.repo.SiteConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "settings_unavailable", "Could not load settings")
		return
	}
	summary, err := s.repo.AdminSettingsSummary(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "settings_summary_unavailable", "Could not load settings summary")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"site":    cfg,
		"summary": summary,
	})
}

func (s *Server) updateAdminSettings(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var req adminSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	cfg, err := s.repo.SiteConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "settings_unavailable", "Could not load settings")
		return
	}
	oldRegistrationOpen := cfg.RegistrationOpen
	if req.RegistrationOpen != nil {
		cfg.RegistrationOpen = *req.RegistrationOpen
	}
	if req.ShowRegisterCTA != nil {
		cfg.ShowRegisterCTA = *req.ShowRegisterCTA
	}
	if req.EmailVerificationRequired != nil {
		cfg.EmailVerificationRequired = *req.EmailVerificationRequired
	}
	if req.BrandName != nil {
		cfg.BrandName = *req.BrandName
	}
	if req.LogoMark != nil {
		cfg.LogoMark = *req.LogoMark
	}
	if req.Subtitle != nil {
		cfg.Subtitle = *req.Subtitle
	}
	if req.FooterText != nil {
		cfg.FooterText = *req.FooterText
	}
	if req.DefaultGatewayPolicy != nil {
		cfg.DefaultGatewayPolicy = *req.DefaultGatewayPolicy
	}
	if req.Timezone != nil {
		cfg.Timezone = *req.Timezone
	}
	if req.MonitorModels != nil {
		cfg.MonitorModels = *req.MonitorModels
	}
	cfg, validationError := cleanSiteConfigInput(cfg)
	if validationError != "" {
		writeError(w, r, http.StatusBadRequest, "settings_invalid", validationError)
		return
	}
	if err := s.repo.SetSiteConfigBy(r.Context(), cfg, user.ID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "settings_failed", "Could not save settings")
		return
	}
	action := "admin.settings.updated"
	if oldRegistrationOpen != cfg.RegistrationOpen {
		if cfg.RegistrationOpen {
			action = "admin.registration.opened"
		} else {
			action = "admin.registration.closed"
		}
	}
	_ = s.repo.WriteAudit(r.Context(), store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     action,
		ObjectType: "site_config",
		ObjectID:   "site",
		IP:         clientIP(r),
		Result:     "success",
		Metadata:   map[string]any{"registration_open": cfg.RegistrationOpen, "email_verification_required": cfg.EmailVerificationRequired, "default_gateway_policy": cfg.DefaultGatewayPolicy, "timezone": cfg.Timezone},
	})
	writeJSON(w, http.StatusOK, map[string]any{"site": cfg})
}
