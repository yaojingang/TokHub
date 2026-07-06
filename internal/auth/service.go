package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"tokhub/internal/store"
)

const CookieName = "tokhub_session"
const CSRFCookieName = "tokhub_csrf"

var ErrEmailNotVerified = errors.New("email not verified")
var ErrInvalidPassword = errors.New("invalid password")

type Service struct {
	repo         *store.Repository
	secretKey    string
	logger       *slog.Logger
	cookieSecure bool
}

type LoginInput struct {
	Identifier                string
	Email                     string
	Password                  string
	EmailVerificationRequired bool
	IP                        string
	UserAgent                 string
}

type Session struct {
	Token string
}

type RegisterInput struct {
	Email                     string
	Password                  string
	Name                      string
	EmailVerificationRequired bool
	IP                        string
	UserAgent                 string
}

func NewService(repo *store.Repository, secretKey string, cookieSecure bool, logger *slog.Logger) *Service {
	return &Service{repo: repo, secretKey: secretKey, cookieSecure: cookieSecure, logger: logger}
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (store.PublicUser, string, error) {
	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return store.PublicUser{}, "", err
	}
	user, err := s.repo.CreateUser(ctx, input.Email, passwordHash, input.Name, !input.EmailVerificationRequired)
	if err != nil {
		return store.PublicUser{}, "", err
	}
	verifyToken := store.EmailToken{}
	if input.EmailVerificationRequired {
		verifyToken = store.NewEmailToken()
		if err := s.repo.CreateEmailToken(ctx, user.ID, "verify_email", verifyToken.Hash, 24*time.Hour); err != nil {
			return store.PublicUser{}, "", err
		}
	}
	_ = s.repo.WriteAudit(ctx, store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "auth.register",
		ObjectType: "user",
		ObjectID:   user.ID,
		IP:         input.IP,
		Result:     "success",
		Metadata:   map[string]any{"email": user.Email},
	})
	return user.Public(), verifyToken.Token, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (Session, store.PublicUser, error) {
	identifier := strings.TrimSpace(input.Identifier)
	if identifier == "" {
		identifier = input.Email
	}
	user, err := s.repo.UserByLoginIdentifier(ctx, strings.ToLower(strings.TrimSpace(identifier)))
	if err != nil {
		return Session{}, store.PublicUser{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return Session{}, store.PublicUser{}, err
	}
	if requiresEmailVerificationForLogin(user, input.EmailVerificationRequired) {
		return Session{}, store.PublicUser{}, ErrEmailNotVerified
	}
	token := "sess_" + uuid.NewString() + uuid.NewString()
	sessionHash := hashToken(token)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := s.repo.CreateSession(ctx, user.ID, sessionHash, input.IP, input.UserAgent, expiresAt); err != nil {
		return Session{}, store.PublicUser{}, err
	}
	_ = s.repo.WriteAudit(ctx, store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "auth.login",
		ObjectType: "user",
		ObjectID:   user.ID,
		IP:         input.IP,
		Result:     "success",
		Metadata:   map[string]any{"email": user.Email},
	})
	return Session{Token: token}, user.Public(), nil
}

func (s *Service) VerifyPassword(ctx context.Context, userID string, password string) error {
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return ErrInvalidPassword
	}
	return nil
}

func requiresEmailVerificationForLogin(user store.User, emailVerificationRequired bool) bool {
	return emailVerificationRequired && user.Role == "user" && !user.EmailVerified
}

func (s *Service) CreatePasswordReset(ctx context.Context, email string, ip string) (string, error) {
	user, err := s.repo.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return "", nil
	}
	token := store.NewEmailToken()
	if err := s.repo.CreateEmailToken(ctx, user.ID, "reset_password", token.Hash, time.Hour); err != nil {
		return "", err
	}
	_ = s.repo.WriteAudit(ctx, store.AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "auth.password_reset_requested",
		ObjectType: "user",
		ObjectID:   user.ID,
		IP:         ip,
		Result:     "success",
		Metadata:   map[string]any{"email": user.Email},
	})
	return token.Token, nil
}

func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	userID, err := s.repo.VerifyEmail(ctx, store.HashOpaqueToken(token))
	if err != nil {
		return err
	}
	return s.repo.WriteAudit(ctx, store.AuditEvent{
		ActorType:  "user",
		ActorID:    userID,
		Action:     "auth.email_verified",
		ObjectType: "user",
		ObjectID:   userID,
		Result:     "success",
		Metadata:   map[string]any{},
	})
}

func (s *Service) ResetPassword(ctx context.Context, token string, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	userID, err := s.repo.ResetPassword(ctx, store.HashOpaqueToken(token), hash)
	if err != nil {
		return err
	}
	return s.repo.WriteAudit(ctx, store.AuditEvent{
		ActorType:  "user",
		ActorID:    userID,
		Action:     "auth.password_reset",
		ObjectType: "user",
		ObjectID:   userID,
		Result:     "success",
		Metadata:   map[string]any{},
	})
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.repo.RevokeSession(ctx, hashToken(token))
}

func (s *Service) RevokeOtherSessions(ctx context.Context, userID string, currentToken string) error {
	return s.repo.RevokeUserSessions(ctx, userID, hashToken(currentToken))
}

func (s *Service) UserForSession(ctx context.Context, token string) (store.PublicUser, error) {
	user, err := s.repo.UserBySession(ctx, hashToken(token))
	if err != nil {
		return store.PublicUser{}, err
	}
	return user.Public(), nil
}

func (s *Service) SessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
	}
}

func (s *Service) ExpiredSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
	}
}

func (s *Service) CSRFCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int((24 * time.Hour).Seconds()),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
	}
}

func NewCSRFToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "csrf_" + uuid.NewString()
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
