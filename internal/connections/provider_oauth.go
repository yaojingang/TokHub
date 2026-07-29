package connections

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrAdapterDisabled            = errors.New("authorization adapter is disabled")
	ErrCredentialReauth           = errors.New("credential requires reauthorization")
	ErrCredentialIdentityMismatch = errors.New("credential identity does not match the existing connection")
	ErrCredentialTemporary        = errors.New("credential provider is temporarily unavailable")
	ErrCredentialRejected         = errors.New("credential validation request was rejected")
	ErrCredentialUnsupported      = errors.New("credential operation is unsupported")
)

type oauthTokenResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	IDToken      string          `json:"id_token"`
	TokenType    string          `json:"token_type"`
	ExpiresIn    int64           `json:"expires_in"`
	Scope        string          `json:"scope"`
	Error        json.RawMessage `json:"error"`
	Description  string          `json:"error_description"`
}

func exchangeOAuthForm(ctx context.Context, client *http.Client, endpoint string, form url.Values) (oauthTokenResponse, error) {
	return exchangeOAuthRequest(ctx, client, endpoint, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
}

func exchangeOAuthJSON(ctx context.Context, client *http.Client, endpoint string, payload any) (oauthTokenResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	return exchangeOAuthRequest(ctx, client, endpoint, bytes.NewReader(body), "application/json")
}

func exchangeOAuthRequest(ctx context.Context, client *http.Client, endpoint string, requestBody io.Reader, contentType string) (oauthTokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, requestBody)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("%w: token endpoint request failed", ErrCredentialTemporary)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("%w: token endpoint response could not be read", ErrCredentialTemporary)
	}
	var token oauthTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return oauthTokenResponse{}, fmt.Errorf("%w: token endpoint returned an invalid response", ErrCredentialTemporary)
	}
	errorCode := oauthTokenErrorCode(token.Error)
	if response.StatusCode < 200 || response.StatusCode >= 300 || errorCode != "" {
		if oauthErrorRequiresReauth(errorCode) {
			return oauthTokenResponse{}, fmt.Errorf("%w: %s", ErrCredentialReauth, errorCode)
		}
		return oauthTokenResponse{}, fmt.Errorf("%w: token endpoint status %d", ErrCredentialTemporary, response.StatusCode)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return oauthTokenResponse{}, fmt.Errorf("%w: token endpoint omitted access token", ErrCredentialTemporary)
	}
	return token, nil
}

type oidcClaims struct {
	Issuer          string
	Subject         string
	Audience        []string
	Email           string
	Nonce           string
	AuthorizedParty string
	ExpiresAt       time.Time
	Raw             map[string]any
}

func parseOIDCClaims(raw string, expectedIssuer string, expectedAudience string, expectedNonce string, requireNonce bool, now time.Time) (oidcClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return oidcClaims{}, fmt.Errorf("id token has an invalid format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return oidcClaims{}, fmt.Errorf("id token payload is invalid")
	}
	values := map[string]any{}
	if err := json.Unmarshal(payload, &values); err != nil {
		return oidcClaims{}, fmt.Errorf("id token claims are invalid")
	}
	claims := oidcClaims{
		Issuer:          stringClaim(values, "iss"),
		Subject:         stringClaim(values, "sub"),
		Email:           stringClaim(values, "email"),
		Nonce:           stringClaim(values, "nonce"),
		AuthorizedParty: stringClaim(values, "azp"),
		Raw:             values,
	}
	switch audience := values["aud"].(type) {
	case string:
		claims.Audience = []string{audience}
	case []any:
		for _, item := range audience {
			if value, ok := item.(string); ok {
				claims.Audience = append(claims.Audience, value)
			}
		}
	}
	exp, err := numberClaim(values, "exp")
	if err != nil {
		return oidcClaims{}, err
	}
	claims.ExpiresAt = time.Unix(exp, 0)
	if !oidcIssuerMatches(claims.Issuer, expectedIssuer) ||
		claims.Subject == "" ||
		!containsString(claims.Audience, expectedAudience) ||
		(len(claims.Audience) > 1 && claims.AuthorizedParty != expectedAudience) {
		return oidcClaims{}, fmt.Errorf("id token issuer, audience, or subject is invalid")
	}
	if !claims.ExpiresAt.After(now.Add(-2 * time.Minute)) {
		return oidcClaims{}, fmt.Errorf("id token has expired")
	}
	if requireNonce && (claims.Nonce == "" || !SecureStateEqual(expectedNonce, claims.Nonce)) {
		return oidcClaims{}, fmt.Errorf("id token nonce is invalid")
	}
	return claims, nil
}

func oauthErrorRequiresReauth(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "invalid_grant",
		"invalid_token",
		"invalid_refresh_token",
		"token_expired",
		"app_session_terminated",
		"refresh_token_reused",
		"refresh_token_invalidated":
		return true
	default:
		return false
	}
}

func oauthTokenErrorCode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var code string
	if json.Unmarshal(raw, &code) == nil {
		return strings.TrimSpace(code)
	}
	var detail struct {
		Code string `json:"code"`
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &detail) != nil {
		return ""
	}
	if strings.TrimSpace(detail.Code) != "" {
		return strings.TrimSpace(detail.Code)
	}
	return strings.TrimSpace(detail.Type)
}

func oidcIssuerMatches(actual string, expected string) bool {
	actual = strings.TrimRight(strings.TrimSpace(actual), "/")
	expected = strings.TrimRight(strings.TrimSpace(expected), "/")
	if actual == expected {
		return true
	}
	return expected == "https://accounts.google.com" && actual == "accounts.google.com"
}

func tokenExpiry(token oauthTokenResponse, now time.Time) time.Time {
	if token.ExpiresIn > 0 && token.ExpiresIn <= int64((24*time.Hour).Seconds()) {
		return now.Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	if token.AccessToken != "" {
		parts := strings.Split(token.AccessToken, ".")
		if len(parts) == 3 {
			if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
				values := map[string]any{}
				if json.Unmarshal(payload, &values) == nil {
					if exp, err := numberClaim(values, "exp"); err == nil {
						return time.Unix(exp, 0)
					}
				}
			}
		}
	}
	return now.Add(time.Hour)
}

func stringClaim(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func numberClaim(values map[string]any, key string) (int64, error) {
	switch value := values[key].(type) {
	case float64:
		return int64(value), nil
	case json.Number:
		return value.Int64()
	case string:
		return strconv.ParseInt(value, 10, 64)
	default:
		return 0, fmt.Errorf("id token claim %s is missing", key)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func scopesFromToken(raw string) []string {
	return uniqueTrimmedStrings(strings.Fields(raw))
}

func uniqueTrimmedStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return ""
	}
	prefix := string([]rune(local)[0])
	return prefix + "***@" + domain
}

func nestedStringClaim(values map[string]any, namespace string, key string) string {
	nested, _ := values[namespace].(map[string]any)
	return stringClaim(nested, key)
}
