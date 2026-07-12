package prober

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	gatewaycache "tokhub/internal/gateway"
)

const (
	l3ProbePrompt          = "Reply exactly: K"
	l3ProbeExpectedContent = "K"
	l3ProbeDefaultTokens   = 2
)

type ProbeTarget struct {
	ID             string
	OwnerType      string
	OwnerID        string
	Name           string
	Provider       string
	Type           string
	Endpoint       string
	Status         string
	Model          string
	ProviderConfig map[string]any
}

type StepResult struct {
	Step       string
	Status     string
	LatencyMs  int
	HTTPStatus int
	ErrorType  string
	Metadata   map[string]any
}

type Executor struct {
	client        *http.Client
	dialer        *net.Dialer
	upstream      *gatewaycache.UpstreamClient
	mockEndpoints bool
}

func NewExecutor() *Executor {
	return NewExecutorWithMockEndpoints(true)
}

func NewExecutorWithMockEndpoints(mockEndpoints bool) *Executor {
	return &Executor{
		client:        &http.Client{Timeout: 8 * time.Second},
		dialer:        &net.Dialer{Timeout: 4 * time.Second},
		upstream:      gatewaycache.NewUpstreamClient(),
		mockEndpoints: mockEndpoints,
	}
}

func (e *Executor) L1(ctx context.Context, target ProbeTarget) []StepResult {
	if e.mockEndpoints && isMockEndpoint(target.Endpoint) {
		return mockL1(target)
	}

	parsed, err := url.Parse(target.Endpoint)
	if err != nil || parsed.Hostname() == "" {
		return []StepResult{{Step: "parse_url", Status: "down", ErrorType: "bad_endpoint"}}
	}

	results := make([]StepResult, 0, 4)
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	address := net.JoinHostPort(host, port)

	start := time.Now()
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	results = append(results, step("dns", err == nil && len(ips) > 0, time.Since(start), errType(err, "dns_failed"), 0, nil))
	if err != nil {
		return markRemaining(results, "tcp", "tls", "http")
	}

	start = time.Now()
	conn, err := e.dialer.DialContext(ctx, "tcp", address)
	results = append(results, step("tcp", err == nil, time.Since(start), errType(err, "tcp_failed"), 0, nil))
	if err != nil {
		return markRemaining(results, "tls", "http")
	}
	_ = conn.Close()

	if parsed.Scheme == "https" {
		start = time.Now()
		tlsConn, err := tls.DialWithDialer(e.dialer, "tcp", address, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		meta := map[string]any{}
		if err == nil {
			state := tlsConn.ConnectionState()
			if len(state.PeerCertificates) > 0 {
				meta["cert_expires_at"] = state.PeerCertificates[0].NotAfter.Format(time.RFC3339)
			}
			_ = tlsConn.Close()
		}
		results = append(results, step("tls", err == nil, time.Since(start), errType(err, "tls_failed"), 0, meta))
		if err != nil {
			return markRemaining(results, "http")
		}
	} else {
		results = append(results, StepResult{Step: "tls", Status: "na"})
	}

	start = time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target.Endpoint, nil)
	if err != nil {
		results = append(results, step("http", false, 0, "bad_request", 0, nil))
		return results
	}
	resp, err := e.client.Do(req)
	ok := err == nil && resp.StatusCode < 500
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
		_ = resp.Body.Close()
	}
	results = append(results, step("http", ok, time.Since(start), httpErrorType(err, statusCode), statusCode, nil))
	return results
}

func (e *Executor) L2(ctx context.Context, target ProbeTarget, apiKey string) []StepResult {
	if e.mockEndpoints && isMockEndpoint(target.Endpoint) {
		return mockL2(target)
	}
	if l2ProbeMode(target) == "skip" {
		return []StepResult{{
			Step:      "models",
			Status:    "na",
			ErrorType: "models_probe_skipped",
			Metadata: map[string]any{
				"probe_mode": "l2_skip",
				"reason":     "provider_profile_skips_models_probe",
			},
		}}
	}

	start := time.Now()
	result, err := e.upstream.ModelsStrict(ctx, target.upstreamTarget(), strings.TrimSpace(apiKey))
	statusCode := result.StatusCode
	payload := result.Body
	status := err == nil && statusCode >= 200 && statusCode < 300
	meta := modelListMetadata(payload, target.Model)
	if summary := upstreamErrorSummary(payload); !status && summary != "" {
		meta["upstream_error_summary"] = summary
	}
	stepResult := step("models", status, time.Since(start), probeUpstreamErrorType(err, result.ErrorType, statusCode, payload), statusCode, meta)
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		stepResult.Status = "auth_error"
		stepResult.ErrorType = "auth_error"
	}
	if status && meta["model_found"] == false {
		stepResult.Status = "down"
		stepResult.ErrorType = "model_not_found"
	}
	return []StepResult{stepResult}
}

func (e *Executor) L3(ctx context.Context, target ProbeTarget, apiKey string) []StepResult {
	if e.mockEndpoints && isMockEndpoint(target.Endpoint) {
		return mockL3(target)
	}
	if l3ProbeMode(target) == "l2_only" {
		return []StepResult{{
			Step:      "generate",
			Status:    "na",
			ErrorType: "l3_probe_skipped",
			Metadata:  map[string]any{"probe_mode": "l2_only", "reason": "provider_requires_official_claude_code_cli"},
		}}
	}

	body := map[string]any{
		"model": target.Model,
		"messages": []map[string]string{
			{"role": "user", "content": l3ProbePrompt},
		},
		"max_tokens":  l3MaxTokens(target),
		"temperature": 0,
	}
	raw, _ := json.Marshal(body)
	start := time.Now()
	estimate := gatewaycache.UpstreamUsage{PromptTokens: 4, CompletionTokens: 1, TotalTokens: 5, Estimated: true}
	result, err := e.upstream.JSON(ctx, target.upstreamTarget(), strings.TrimSpace(apiKey), "chat", raw, estimate)
	latency := int(time.Since(start).Milliseconds())
	statusCode := result.StatusCode
	payload := result.Body
	meta := l3Metadata(payload, latency)
	applyUsageMetadata(meta, result.Usage, latency)
	contentStatus := l3ContentStatusForTarget(payload, target)
	meta["content_valid"] = contentStatus.Valid
	meta["content_error"] = contentStatus.ErrorType
	meta["first_token_ms"] = maxInt(1, latency/3)
	meta["usage_estimated"] = result.Usage.Estimated
	warnThreshold := l3WarnThresholdMs(target)
	meta["warn_threshold_ms"] = warnThreshold
	if summary := upstreamErrorSummary(payload); (err != nil || statusCode < 200 || statusCode >= 300) && summary != "" {
		meta["upstream_error_summary"] = summary
	}

	ok := err == nil && statusCode >= 200 && statusCode < 300 && contentStatus.Valid
	stepResult := step("generate", ok, time.Duration(latency)*time.Millisecond, probeUpstreamErrorType(err, result.ErrorType, statusCode, payload), statusCode, meta)
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		stepResult.Status = "auth_error"
		stepResult.ErrorType = "auth_error"
	}
	if statusCode == http.StatusTooManyRequests {
		stepResult.Status = "warn"
		stepResult.ErrorType = "rate_limited"
	}
	if statusCode >= 200 && statusCode < 300 && !ok {
		stepResult.Status = "down"
		stepResult.ErrorType = contentStatus.ErrorType
	}
	if latency > warnThreshold && stepResult.Status == "ok" {
		stepResult.Status = "warn"
		stepResult.ErrorType = "slow_response"
	}
	return []StepResult{stepResult}
}

func (target ProbeTarget) upstreamTarget() gatewaycache.Upstream {
	return gatewaycache.Upstream{
		Name:           target.Name,
		Provider:       target.Provider,
		Type:           target.Type,
		Endpoint:       target.Endpoint,
		Model:          target.Model,
		ProviderConfig: target.ProviderConfig,
	}
}

func Summarize(results []StepResult) LayerSummary {
	if len(results) == 0 {
		return LayerSummary{Status: "na"}
	}
	var total int
	var firstErrType string
	sawOK := false
	for _, result := range results {
		total += result.LatencyMs
		if firstErrType == "" && result.ErrorType != "" {
			firstErrType = result.ErrorType
		}
		if result.Status == "auth_error" {
			return LayerSummary{Status: "auth_error", LatencyMs: total, ErrorType: "auth_error"}
		}
		if result.Status == "down" {
			return LayerSummary{Status: "down", LatencyMs: total, ErrorType: result.ErrorType}
		}
		if result.Status == "warn" {
			return LayerSummary{Status: "warn", LatencyMs: total, ErrorType: result.ErrorType}
		}
		if result.Status == "ok" {
			sawOK = true
		}
	}
	if sawOK {
		return LayerSummary{Status: "ok", LatencyMs: total, ErrorType: firstErrType}
	}
	return LayerSummary{Status: "na", LatencyMs: total, ErrorType: firstErrType}
}

func step(name string, ok bool, duration time.Duration, errorType string, statusCode int, metadata map[string]any) StepResult {
	status := "down"
	if ok {
		status = "ok"
		errorType = ""
	}
	return StepResult{
		Step:       name,
		Status:     status,
		LatencyMs:  int(duration.Milliseconds()),
		HTTPStatus: statusCode,
		ErrorType:  errorType,
		Metadata:   metadata,
	}
}

func markRemaining(results []StepResult, steps ...string) []StepResult {
	for _, name := range steps {
		results = append(results, StepResult{Step: name, Status: "na"})
	}
	return results
}

func errType(err error, fallback string) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns_failed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return fallback
}

func httpErrorType(err error, statusCode int) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		return "http_failed"
	}
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return "auth_error"
	case statusCode == http.StatusTooManyRequests:
		return "rate_limited"
	case statusCode >= 500:
		return "upstream_error"
	case statusCode >= 400:
		return "http_error"
	default:
		return ""
	}
}

func probeUpstreamErrorType(err error, upstreamError string, statusCode int, body []byte) string {
	if err == nil && statusCode >= 200 && statusCode < 300 {
		return ""
	}
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || upstreamError == "upstream_auth_error":
		return "auth_error"
	case statusCode == http.StatusTooManyRequests || upstreamError == "upstream_rate_limited":
		return "rate_limited"
	case upstreamBodySuggestsModelUnavailable(body):
		return "model_unavailable"
	case upstreamError == "upstream_timeout":
		return "timeout"
	case upstreamError == "upstream_unreachable":
		return "http_failed"
	case upstreamError != "":
		return upstreamError
	default:
		return httpErrorType(err, statusCode)
	}
}

func upstreamBodySuggestsModelUnavailable(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	text := strings.ToLower(string(body))
	text = strings.ReplaceAll(text, "_", " ")
	text = strings.ReplaceAll(text, "-", " ")
	switch {
	case strings.Contains(text, "no available channel") && strings.Contains(text, "model"):
		return true
	case strings.Contains(text, "no available accounts") && strings.Contains(text, "model"):
		return true
	case strings.Contains(text, "model") && strings.Contains(text, "does not exist"):
		return true
	case strings.Contains(text, "model") && strings.Contains(text, "not found"):
		return true
	case strings.Contains(text, "模型") && strings.Contains(text, "无可用"):
		return true
	case strings.Contains(text, "无可用渠道") && strings.Contains(text, "gpt"):
		return true
	default:
		return false
	}
}

func isMockEndpoint(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	return err == nil && strings.HasSuffix(parsed.Hostname(), ".example")
}

func mockL1(target ProbeTarget) []StepResult {
	if target.Status == "connectivity_down" {
		return []StepResult{
			{Step: "dns", Status: "down", ErrorType: "dns_failed"},
			{Step: "tcp", Status: "na"},
			{Step: "tls", Status: "na"},
			{Step: "http", Status: "na"},
		}
	}
	seed := stableSeed(target.ID)
	return []StepResult{
		{Step: "dns", Status: "ok", LatencyMs: 15 + seed%30},
		{Step: "tcp", Status: "ok", LatencyMs: 35 + seed%45},
		{Step: "tls", Status: "ok", LatencyMs: 42 + seed%60, Metadata: map[string]any{"cert_expires_at": time.Now().AddDate(0, 2, seed%21).Format(time.RFC3339)}},
		{Step: "http", Status: "ok", LatencyMs: 60 + seed%100, HTTPStatus: 200},
	}
}

func mockL2(target ProbeTarget) []StepResult {
	seed := stableSeed(target.ID)
	switch target.Status {
	case "auth_error":
		return []StepResult{{Step: "models", Status: "auth_error", LatencyMs: 240 + seed%80, HTTPStatus: 401, ErrorType: "auth_error"}}
	case "connectivity_down":
		return []StepResult{{Step: "models", Status: "na"}}
	default:
		return []StepResult{{Step: "models", Status: "ok", LatencyMs: 220 + seed%260, HTTPStatus: 200, Metadata: map[string]any{"models_count": 12 + seed%40, "model_found": true}}}
	}
}

func mockL3(target ProbeTarget) []StepResult {
	seed := stableSeed(target.ID)
	switch target.Status {
	case "auth_error":
		return []StepResult{{Step: "generate", Status: "auth_error", LatencyMs: 260 + seed%120, HTTPStatus: 401, ErrorType: "auth_error"}}
	case "connectivity_down":
		return []StepResult{{Step: "generate", Status: "na", ErrorType: "connectivity_down"}}
	case "functional_down":
		return []StepResult{{Step: "generate", Status: "down", LatencyMs: 900 + seed%300, HTTPStatus: 200, ErrorType: "empty_content", Metadata: map[string]any{"content_valid": false, "tokens_used": 0, "first_token_ms": 0, "tokens_per_second": 0}}}
	case "degraded":
		return []StepResult{{Step: "generate", Status: "warn", LatencyMs: 3200 + seed%500, HTTPStatus: 200, ErrorType: "slow_response", Metadata: map[string]any{"content_valid": true, "tokens_used": 5, "first_token_ms": 1100 + seed%200, "tokens_per_second": 2}}}
	default:
		return []StepResult{{Step: "generate", Status: "ok", LatencyMs: 820 + seed%620, HTTPStatus: 200, Metadata: map[string]any{"content_valid": true, "tokens_used": 5, "first_token_ms": 180 + seed%220, "tokens_per_second": 4}}}
	}
}

func l3Metadata(payload []byte, latencyMs int) map[string]any {
	meta := map[string]any{"tokens_used": 0, "tokens_per_second": 0}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return meta
	}
	if usage, ok := body["usage"].(map[string]any); ok {
		total := numeric(usage["total_tokens"])
		if total == 0 {
			total = numeric(usage["prompt_tokens"]) + numeric(usage["completion_tokens"])
		}
		meta["tokens_used"] = total
		if latencyMs > 0 {
			meta["tokens_per_second"] = int(float64(total) / (float64(latencyMs) / 1000))
		}
	}
	return meta
}

func applyUsageMetadata(meta map[string]any, usage gatewaycache.UpstreamUsage, latencyMs int) {
	if usage.TotalTokens <= 0 {
		return
	}
	meta["tokens_used"] = usage.TotalTokens
	if latencyMs > 0 {
		meta["tokens_per_second"] = int(float64(usage.TotalTokens) / (float64(latencyMs) / 1000))
	}
}

func l3WarnThresholdMs(target ProbeTarget) int {
	if value, ok := probeConfigInt(target.ProviderConfig, "l3WarnMs", 500, 600000); ok {
		return value
	}
	if value, ok := probeConfigInt(target.ProviderConfig, "l3WarnThresholdMs", 500, 600000); ok {
		return value
	}
	kind := strings.ToLower(strings.TrimSpace(target.Type + " " + target.Provider))
	if strings.Contains(kind, "anthropic") || strings.Contains(kind, "claude") || strings.Contains(kind, "gemini") || strings.Contains(kind, "google") {
		return 6000
	}
	return 4000
}

func l3MaxTokens(target ProbeTarget) int {
	if value, ok := probeConfigInt(target.ProviderConfig, "l3ProbeMaxTokens", -200000, 200000); ok {
		return clampInt(value, 1, 8)
	}
	if strings.EqualFold(strings.TrimSpace(target.Model), "gpt-5.5") {
		return 8
	}
	return l3ProbeDefaultTokens
}

func l3ProbeMode(target ProbeTarget) string {
	value, ok := probeConfigString(target.ProviderConfig, "l3ProbeMode")
	if !ok {
		return "generate"
	}
	switch strings.ToLower(value) {
	case "l2_only", "l2-only", "skip":
		return "l2_only"
	default:
		return "generate"
	}
}

func l2ProbeMode(target ProbeTarget) string {
	for _, key := range []string{"l2ProbeMode", "modelsProbeMode"} {
		value, ok := probeConfigString(target.ProviderConfig, key)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.ReplaceAll(value, "-", "_")) {
		case "skip", "disabled", "off", "l3_only", "l3only":
			return "skip"
		default:
			return "required"
		}
	}
	if isClaudeCodeProfile(target) {
		return "skip"
	}
	return "required"
}

func isClaudeCodeProfile(target ProbeTarget) bool {
	values := []string{target.Name, target.Provider, target.Endpoint}
	for _, value := range values {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
		if strings.Contains(normalized, "claudecode") || strings.Contains(normalized, "claude-code") {
			return true
		}
	}
	if profile, ok := probeConfigString(target.ProviderConfig, "clientProfile"); ok {
		normalized := strings.ToLower(strings.ReplaceAll(profile, "_", "-"))
		return normalized == "claude-code"
	}
	return false
}

func l3ContentStatusForTarget(payload []byte, target ProbeTarget) l3ContentCheck {
	policy, _ := probeConfigString(target.ProviderConfig, "l3ContentPolicy")
	expected, _ := probeConfigString(target.ProviderConfig, "l3ExpectedContent")
	if expected == "" {
		expected = l3ProbeExpectedContent
	}
	return l3ContentStatusWithPolicy(payload, policy, expected)
}

func probeConfigString(config map[string]any, key string) (string, bool) {
	if len(config) == 0 {
		return "", false
	}
	value, ok := config[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func probeConfigInt(config map[string]any, key string, min int, max int) (int, bool) {
	if len(config) == 0 {
		return 0, false
	}
	value, ok := config[key]
	if !ok {
		return 0, false
	}
	var out int
	switch v := value.(type) {
	case int:
		out = v
	case int64:
		out = int(v)
	case float64:
		out = int(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		out = int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		out = n
	default:
		return 0, false
	}
	if out < min || out > max {
		return 0, false
	}
	return out, true
}

func modelListMetadata(payload []byte, targetModel string) map[string]any {
	meta := map[string]any{"models_count": 0, "model_found": strings.TrimSpace(targetModel) == ""}
	if len(payload) == 0 {
		return meta
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return meta
	}
	targetModel = strings.TrimSpace(targetModel)
	seen := map[string]bool{}
	if data, ok := body["data"].([]any); ok {
		for _, row := range data {
			if item, ok := row.(map[string]any); ok {
				if id, ok := item["id"].(string); ok && strings.TrimSpace(id) != "" {
					seen[id] = true
				}
			}
		}
	}
	if models, ok := body["models"].([]any); ok {
		for _, row := range models {
			switch item := row.(type) {
			case string:
				seen[item] = true
			case map[string]any:
				if id, ok := item["id"].(string); ok && strings.TrimSpace(id) != "" {
					seen[id] = true
				}
				if name, ok := item["name"].(string); ok && strings.TrimSpace(name) != "" {
					seen[name] = true
				}
			}
		}
	}
	meta["models_count"] = len(seen)
	if targetModel != "" {
		meta["model_found"] = seen[targetModel]
	}
	return meta
}

var (
	authorizationSecretPattern = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*["']?[a-z0-9._~+/=-]+(?:\s+[a-z0-9._~+/=-]+)?`)
	bearerSecretPattern        = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	namedSecretPattern         = regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password)\b\s*["']?\s*[:=]\s*["']?\s*[^\s,;"}']+`)
	skSecretPattern            = regexp.MustCompile(`\bsk-[a-zA-Z0-9._-]{6,}`)
	jwtSecretPattern           = regexp.MustCompile(`\b[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\b`)
	googleAPIKeyPattern        = regexp.MustCompile(`\bAIza[a-zA-Z0-9_-]{20,}\b`)
)

func upstreamErrorSummary(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	fields := make([]string, 0, 3)
	appendFields := func(values map[string]any) {
		for _, key := range []string{"code", "type"} {
			value, ok := values[key].(string)
			value = strings.TrimSpace(value)
			if ok && value != "" {
				fields = append(fields, key+"="+value)
			}
		}
	}
	if nested, ok := body["error"].(map[string]any); ok {
		appendFields(nested)
	} else {
		appendFields(body)
	}
	if len(fields) == 0 {
		return ""
	}
	summary := strings.Join(fields, " · ")
	summary = authorizationSecretPattern.ReplaceAllString(summary, "Authorization: [redacted]")
	summary = bearerSecretPattern.ReplaceAllString(summary, "Bearer [redacted]")
	summary = namedSecretPattern.ReplaceAllString(summary, "$1=[redacted]")
	summary = skSecretPattern.ReplaceAllString(summary, "sk-[redacted]")
	summary = jwtSecretPattern.ReplaceAllString(summary, "[redacted-jwt]")
	summary = googleAPIKeyPattern.ReplaceAllString(summary, "[redacted-google-key]")
	runes := []rune(summary)
	if len(runes) > 512 {
		summary = string(runes[:512])
	}
	return summary
}

type l3ContentCheck struct {
	Valid     bool
	ErrorType string
}

func l3ContentStatus(payload []byte) l3ContentCheck {
	return l3ContentStatusWithPolicy(payload, "exact", l3ProbeExpectedContent)
}

func l3ContentStatusWithPolicy(payload []byte, policy string, expected string) l3ContentCheck {
	text, ok := assistantText(payload)
	if !ok || strings.TrimSpace(text) == "" {
		if assistantReasoningContent(payload) != "" {
			return l3ContentCheck{ErrorType: "reasoning_only"}
		}
		return l3ContentCheck{ErrorType: "empty_content"}
	}
	if strings.EqualFold(strings.TrimSpace(policy), "non_empty") {
		return l3ContentCheck{Valid: true}
	}
	if strings.TrimSpace(text) != expected {
		return l3ContentCheck{ErrorType: "content_mismatch"}
	}
	return l3ContentCheck{Valid: true}
}

func contentValid(payload []byte) bool {
	return l3ContentStatus(payload).Valid
}

func assistantText(payload []byte) (string, bool) {
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", false
	}
	choices, ok := body["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", false
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return "", false
	}
	if message, ok := first["message"].(map[string]any); ok {
		if content, ok := message["content"].(string); ok {
			return content, true
		}
	}
	if text, ok := first["text"].(string); ok {
		return text, true
	}
	return "", false
}

func assistantReasoningContent(payload []byte) string {
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	choices, ok := body["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	if message, ok := first["message"].(map[string]any); ok {
		if content, ok := message["reasoning_content"].(string); ok {
			return strings.TrimSpace(content)
		}
	}
	if content, ok := first["reasoning_content"].(string); ok {
		return strings.TrimSpace(content)
	}
	return ""
}

func numeric(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func stableSeed(value string) int {
	total := 0
	for _, r := range value {
		total = total*31 + int(r)
	}
	if total < 0 {
		return -total
	}
	return total
}
