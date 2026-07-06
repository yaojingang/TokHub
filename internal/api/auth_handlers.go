package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"tokhub/internal/auth"
)

type loginRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type tokenRequest struct {
	Token string `json:"token"`
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	emailVerificationRequired, err := s.emailVerificationRequired(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "site_config_unavailable", "Could not load site config")
		return
	}
	session, user, err := s.auth.Login(r.Context(), auth.LoginInput{
		Identifier:                req.Identifier,
		Email:                     req.Email,
		Password:                  req.Password,
		EmailVerificationRequired: emailVerificationRequired,
		IP:                        r.RemoteAddr,
		UserAgent:                 r.UserAgent(),
	})
	if err != nil {
		if errors.Is(err, auth.ErrEmailNotVerified) {
			writeError(w, r, http.StatusForbidden, "email_not_verified", "请先完成邮箱验证后再登录")
			return
		}
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "Invalid account or password")
		return
	}
	http.SetCookie(w, s.auth.SessionCookie(session.Token))
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.repo.SiteConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "site_config_unavailable", "Could not load site config")
		return
	}
	if !cfg.RegistrationOpen {
		writeError(w, r, http.StatusForbidden, "registration_closed", "Registration is currently closed")
		return
	}
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !validEmail(req.Email) || len(req.Password) < 8 {
		writeError(w, r, http.StatusBadRequest, "invalid_registration", "Email and password are invalid")
		return
	}
	if cfg.EmailVerificationRequired && !s.cfg.ExposeDevTokens {
		if _, err := parseSMTPURL(s.cfg.SMTPURL); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "email_service_not_configured", "邮箱验证已开启，但邮件服务未配置，请联系管理员")
			return
		}
	}
	user, verifyToken, err := s.auth.Register(r.Context(), auth.RegisterInput{
		Email:                     req.Email,
		Password:                  req.Password,
		Name:                      req.Name,
		EmailVerificationRequired: cfg.EmailVerificationRequired,
		IP:                        clientIP(r),
		UserAgent:                 r.UserAgent(),
	})
	if err != nil {
		writeError(w, r, http.StatusConflict, "registration_failed", "这个邮箱已注册或暂时无法创建，请直接登录或更换邮箱")
		return
	}
	deliveredBy := ""
	if cfg.EmailVerificationRequired {
		mailStatus, mailError, mailDeliveredBy, _ := s.sendAuthMail(r.Context(), user.Email, "TokHub 邮箱验证", fmt.Sprintf("请打开以下链接完成邮箱验证：\n\n%s/login?verify=%s\n\n如果不是你本人操作，请忽略这封邮件。", strings.TrimRight(s.cfg.PublicURL, "/"), verifyToken))
		deliveredBy = mailDeliveredBy
		if mailStatus == "failed" {
			s.logger.Warn("verification email delivery failed", "delivered_by", deliveredBy, "error", mailError)
			deliveredBy = "failed"
		}
		http.SetCookie(w, s.auth.ExpiredSessionCookie())
	} else {
		session, loggedInUser, err := s.auth.Login(r.Context(), auth.LoginInput{
			Identifier:                user.Email,
			Email:                     user.Email,
			Password:                  req.Password,
			EmailVerificationRequired: cfg.EmailVerificationRequired,
			IP:                        clientIP(r),
			UserAgent:                 r.UserAgent(),
		})
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "registration_session_failed", "账号已创建，但自动登录失败，请返回登录页重试")
			return
		}
		http.SetCookie(w, s.auth.SessionCookie(session.Token))
		user = loggedInUser
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":                 user,
		"verificationRequired": cfg.EmailVerificationRequired,
		"emailDelivery":        deliveredBy,
		"devVerificationToken": s.devToken(verifyToken),
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err == nil {
		_ = s.auth.Logout(r.Context(), cookie.Value)
	}
	http.SetCookie(w, s.auth.ExpiredSessionCookie())
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if err := s.auth.VerifyEmail(r.Context(), req.Token); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_token", "Verification token is invalid or expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	resetToken, err := s.auth.CreatePasswordReset(r.Context(), req.Email, clientIP(r))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "reset_unavailable", "Could not create reset token")
		return
	}
	if resetToken != "" {
		mailStatus, mailError, deliveredBy, _ := s.sendAuthMail(r.Context(), req.Email, "TokHub 密码重置", fmt.Sprintf("请打开以下链接重置密码：\n\n%s/login?reset=%s\n\n如果不是你本人操作，请忽略这封邮件。", strings.TrimRight(s.cfg.PublicURL, "/"), resetToken))
		if mailStatus == "failed" {
			s.logger.Warn("password reset email delivery failed", "delivered_by", deliveredBy, "error", mailError)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"devResetToken": s.devToken(resetToken),
	})
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, r, http.StatusBadRequest, "weak_password", "Password must be at least 8 characters")
		return
	}
	if err := s.auth.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_token", "Reset token is invalid or expired")
		return
	}
	http.SetCookie(w, s.auth.ExpiredSessionCookie())
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (s *Server) revokeSessions(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return
	}
	user, err := s.auth.UserForSession(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return
	}
	if err := s.auth.RevokeOtherSessions(r.Context(), user.ID, cookie.Value); err != nil {
		writeError(w, r, http.StatusInternalServerError, "sessions_unavailable", "Could not revoke sessions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}
	user, err := s.auth.UserForSession(r.Context(), cookie.Value)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func validEmail(email string) bool {
	email = strings.TrimSpace(email)
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}

func (s *Server) devToken(token string) string {
	if !s.cfg.ExposeDevTokens {
		return ""
	}
	return token
}
