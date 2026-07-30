package browserconnector

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	ActionStatus   = "status"
	ActionAsk      = "ask"
	MaxPromptBytes = 64 << 10
)

var (
	ErrUnsupportedProvider = errors.New("browser provider is unsupported")
	ErrUnsupportedAction   = errors.New("browser action is unsupported")
	ErrUnsupportedRequest  = errors.New("browser request is unsupported")
)

type Task struct {
	Provider string `json:"provider"`
	Action   string `json:"action"`
	Prompt   string `json:"prompt,omitempty"`
}

type Result struct {
	OK                 bool   `json:"ok"`
	Content            string `json:"content,omitempty"`
	AccountMask        string `json:"accountMask,omitempty"`
	AccountFingerprint string `json:"accountFingerprint,omitempty"`
	ErrorCode          string `json:"errorCode,omitempty"`
	ErrorMessage       string `json:"errorMessage,omitempty"`
}

func SanitizeResult(result Result) (Result, error) {
	if result.OK {
		content := strings.TrimSpace(result.Content)
		accountMask := strings.TrimSpace(result.AccountMask)
		if content == "" && accountMask == "" {
			return Result{}, fmt.Errorf("%w: result is empty", ErrUnsupportedRequest)
		}
		if len(content) > MaxPromptBytes || !utf8.ValidString(content) {
			return Result{}, fmt.Errorf("%w: result content is too large", ErrUnsupportedRequest)
		}
		if len(accountMask) > 256 || !utf8.ValidString(accountMask) {
			return Result{}, fmt.Errorf("%w: account mask is too large", ErrUnsupportedRequest)
		}
		accountFingerprint := strings.ToLower(strings.TrimSpace(result.AccountFingerprint))
		if accountMask != "" && !validAccountFingerprint(accountFingerprint) {
			return Result{}, fmt.Errorf("%w: account fingerprint is invalid", ErrUnsupportedRequest)
		}
		if accountMask == "" {
			accountFingerprint = ""
		}
		return Result{
			OK: true, Content: content, AccountMask: accountMask,
			AccountFingerprint: accountFingerprint,
		}, nil
	}
	code := strings.ToLower(strings.TrimSpace(result.ErrorCode))
	switch code {
	case "login_required", "security_challenge", "identity_mismatch", "rate_limited",
		"access_denied", "adapter_incompatible", "upstream_unavailable",
		"opencli_failed", "opencli_command_failed", "task_rejected":
	default:
		code = "opencli_failed"
	}
	return Result{OK: false, ErrorCode: code, ErrorMessage: safeOpenCLIErrorMessage(code)}, nil
}

func BuildOpenCLICommand(task Task) ([]string, error) {
	command, err := providerCommand(task.Provider)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(task.Action)) {
	case ActionStatus:
		return []string{command, "whoami", "-f", "json"}, nil
	case ActionAsk:
		prompt := strings.TrimSpace(task.Prompt)
		if prompt == "" || len(prompt) > MaxPromptBytes || !utf8.ValidString(prompt) {
			return nil, fmt.Errorf("%w: prompt is empty or too large", ErrUnsupportedRequest)
		}
		return []string{command, "ask", prompt, "-f", "json", "--timeout", "90", "--new", "true"}, nil
	default:
		return nil, ErrUnsupportedAction
	}
}

func providerCommand(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "chatgpt":
		return "chatgpt", nil
	case "gemini":
		return "gemini", nil
	case "deepseek":
		return "deepseek", nil
	default:
		return "", ErrUnsupportedProvider
	}
}

func PromptFromOpenAIRequest(kind string, payload map[string]any) (string, error) {
	if value, _ := payload["stream"].(bool); value {
		return "", fmt.Errorf("%w: streaming is unavailable for personal browser connections", ErrUnsupportedRequest)
	}
	if hasNonEmptyCollection(payload["tools"]) || hasNonEmptyCollection(payload["functions"]) {
		return "", fmt.Errorf("%w: tools are unavailable for personal browser connections", ErrUnsupportedRequest)
	}
	var prompt string
	var err error
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "chat", "chat.completions", "chat_completion":
		prompt, err = promptFromMessages(payload["messages"])
	case "responses", "response":
		prompt, err = promptFromResponsesInput(payload["input"])
	default:
		err = fmt.Errorf("%w: request kind %q", ErrUnsupportedRequest, kind)
	}
	if err != nil {
		return "", err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(prompt) > MaxPromptBytes {
		return "", fmt.Errorf("%w: text input is empty or too large", ErrUnsupportedRequest)
	}
	return prompt, nil
}

func promptFromResponsesInput(input any) (string, error) {
	if text, ok := input.(string); ok {
		return text, nil
	}
	return promptFromMessages(input)
}

func promptFromMessages(raw any) (string, error) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return "", fmt.Errorf("%w: messages are required", ErrUnsupportedRequest)
	}
	parts := make([]string, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return "", fmt.Errorf("%w: message is invalid", ErrUnsupportedRequest)
		}
		role := strings.ToLower(strings.TrimSpace(stringValue(item["role"])))
		label := map[string]string{"system": "System", "developer": "Developer", "user": "User", "assistant": "Assistant"}[role]
		if label == "" {
			return "", fmt.Errorf("%w: message role %q", ErrUnsupportedRequest, role)
		}
		content, err := textContent(item["content"])
		if err != nil {
			return "", err
		}
		parts = append(parts, label+": "+content)
	}
	return strings.Join(parts, "\n\n"), nil
}

func textContent(raw any) (string, error) {
	if value, ok := raw.(string); ok {
		value = strings.TrimSpace(value)
		if value == "" {
			return "", fmt.Errorf("%w: message text is empty", ErrUnsupportedRequest)
		}
		return value, nil
	}
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return "", fmt.Errorf("%w: message text is required", ErrUnsupportedRequest)
	}
	texts := make([]string, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return "", fmt.Errorf("%w: content item is invalid", ErrUnsupportedRequest)
		}
		itemType := strings.ToLower(strings.TrimSpace(stringValue(item["type"])))
		if itemType != "text" && itemType != "input_text" && itemType != "output_text" {
			return "", fmt.Errorf("%w: content type %q", ErrUnsupportedRequest, itemType)
		}
		text := strings.TrimSpace(stringValue(item["text"]))
		if text == "" {
			return "", fmt.Errorf("%w: content text is empty", ErrUnsupportedRequest)
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, "\n"), nil
}

func NormalizeOpenCLIResult(action string, raw []byte) (Result, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Result{}, fmt.Errorf("OpenCLI returned invalid JSON: %w", err)
	}
	payload, err := openCLIResultObject(decoded)
	if err != nil {
		return Result{}, err
	}
	if success, ok := payload["success"].(bool); ok && !success {
		message := firstString(payload, "error", "message")
		if message == "" {
			message = "浏览器任务执行失败"
		}
		code := classifyOpenCLIError(message)
		return Result{OK: false, ErrorCode: code, ErrorMessage: safeOpenCLIErrorMessage(code)}, nil
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = payload
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case ActionAsk:
		content := firstString(data, "content", "answer", "text", "result", "response")
		if content == "" {
			return Result{}, errors.New("OpenCLI response does not contain model text")
		}
		if strings.Contains(strings.ToUpper(content), "[NO RESPONSE]") {
			return Result{
				OK: false, ErrorCode: "opencli_failed",
				ErrorMessage: safeOpenCLIErrorMessage("opencli_failed"),
			}, nil
		}
		return Result{OK: true, Content: truncateText(content, MaxPromptBytes)}, nil
	case ActionStatus:
		loggedIn := false
		if value, ok := data["loggedIn"].(bool); ok {
			loggedIn = value
		}
		if value, ok := data["logged_in"].(bool); ok {
			loggedIn = value
		}
		if value, ok := data["authenticated"].(bool); ok {
			loggedIn = value
		}
		if !loggedIn {
			return Result{OK: false, ErrorCode: "login_required", ErrorMessage: "请先在 Chrome 中登录对应账号"}, nil
		}
		identity := firstString(data, "user_id", "userId", "id", "email", "account", "username", "name", "displayName")
		if identity == "" {
			return Result{
				OK: false, ErrorCode: "opencli_failed",
				ErrorMessage: safeOpenCLIErrorMessage("opencli_failed"),
			}, nil
		}
		account := firstString(data, "email", "account", "username", "name", "displayName", "user_id", "userId", "id")
		return Result{
			OK: true, AccountMask: maskAccount(account),
			AccountFingerprint: accountFingerprint(firstString(data, "site"), identity),
		}, nil
	default:
		return Result{}, ErrUnsupportedAction
	}
}

func NormalizeOpenCLICommandFailure(raw []byte) Result {
	code := classifyOpenCLIError(string(raw))
	if code == "opencli_failed" {
		code = "opencli_command_failed"
	}
	return Result{
		OK:           false,
		ErrorCode:    code,
		ErrorMessage: safeOpenCLIErrorMessage(code),
	}
}

func openCLIResultObject(value any) (map[string]any, error) {
	if object, ok := value.(map[string]any); ok {
		return object, nil
	}
	items, ok := value.([]any)
	if !ok || len(items) != 1 {
		return nil, errors.New("OpenCLI response must contain exactly one result")
	}
	object, ok := items[0].(map[string]any)
	if !ok {
		return nil, errors.New("OpenCLI response result is invalid")
	}
	return object, nil
}

func classifyOpenCLIError(message string) string {
	normalized := strings.ToLower(message)
	switch {
	case strings.Contains(normalized, "captcha"),
		strings.Contains(normalized, "challenge"),
		strings.Contains(normalized, "verify you are human"),
		strings.Contains(normalized, "验证码"),
		strings.Contains(normalized, "人机验证"):
		return "security_challenge"
	case strings.Contains(normalized, "429"),
		strings.Contains(normalized, "too many request"),
		strings.Contains(normalized, "rate limit"),
		strings.Contains(normalized, "频率限制"),
		strings.Contains(normalized, "请求过于频繁"):
		return "rate_limited"
	case strings.Contains(normalized, "403"),
		strings.Contains(normalized, "forbidden"),
		strings.Contains(normalized, "access denied"),
		strings.Contains(normalized, "访问被拒绝"):
		return "access_denied"
	case strings.Contains(normalized, "selector"),
		strings.Contains(normalized, "element not found"),
		strings.Contains(normalized, "cannot find element"),
		strings.Contains(normalized, "页面结构"),
		strings.Contains(normalized, "适配器"):
		return "adapter_incompatible"
	case strings.Contains(normalized, "browser_connect"),
		strings.Contains(normalized, "extension") && strings.Contains(normalized, "not connected"),
		strings.Contains(normalized, "profile") && strings.Contains(normalized, "not connected"),
		strings.Contains(normalized, "daemon") && strings.Contains(normalized, "not running"),
		strings.Contains(normalized, "service unavailable"):
		return "upstream_unavailable"
	case strings.Contains(normalized, "timeout"),
		strings.Contains(normalized, "timed out"),
		strings.Contains(normalized, "network"),
		strings.Contains(normalized, "connection reset"),
		strings.Contains(normalized, "超时"),
		strings.Contains(normalized, "网络"):
		return "upstream_unavailable"
	case strings.Contains(normalized, "auth_required"),
		strings.Contains(normalized, "not logged"),
		strings.Contains(normalized, "cookie missing"),
		strings.Contains(normalized, "anonymous"),
		strings.Contains(normalized, "login"),
		strings.Contains(normalized, "log in"),
		strings.Contains(normalized, "sign in"),
		strings.Contains(normalized, "unauthorized"),
		strings.Contains(normalized, "登录"):
		return "login_required"
	default:
		return "opencli_failed"
	}
}

func safeOpenCLIErrorMessage(code string) string {
	switch code {
	case "login_required":
		return "请先在 Chrome 中登录对应账号"
	case "security_challenge":
		return "浏览器出现验证码或安全验证，请在 Chrome 中处理后重试"
	case "identity_mismatch":
		return "当前网页登录账号与连接创建时不一致，请切换回原账号或重新建立连接"
	case "rate_limited":
		return "服务商提示请求过于频繁，TokHub 已暂停该账号的网页中转"
	case "access_denied":
		return "服务商拒绝了当前网页请求，TokHub 已锁定该账号的网页中转"
	case "adapter_incompatible":
		return "网页结构已变化，当前 OpenCLI 适配器需要更新"
	case "upstream_unavailable":
		return "网页或网络暂时不可用，TokHub 已进入冷却保护"
	default:
		return "OpenCLI 网页任务执行失败，请检查 Chrome 与扩展连接"
	}
}

func accountFingerprint(site string, identity string) string {
	sum := sha256.Sum256([]byte(
		"tokhub-opencli-account-v1\x00" +
			strings.ToLower(strings.TrimSpace(site)) + "\x00" +
			strings.TrimSpace(identity),
	))
	return fmt.Sprintf("%x", sum[:])
}

func validAccountFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func AccountFingerprintMatches(expected string, actual string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	actual = strings.ToLower(strings.TrimSpace(actual))
	if !validAccountFingerprint(expected) || !validAccountFingerprint(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func IsValidAccountFingerprint(value string) bool {
	return validAccountFingerprint(strings.ToLower(strings.TrimSpace(value)))
}

func BindAccountFingerprint(deviceToken string, fingerprint string) string {
	deviceToken = strings.TrimSpace(deviceToken)
	fingerprint = strings.ToLower(strings.TrimSpace(fingerprint))
	if len(deviceToken) < 32 || !validAccountFingerprint(fingerprint) {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(deviceToken))
	_, _ = mac.Write([]byte("tokhub-opencli-account-binding-v1\x00" + fingerprint))
	return fmt.Sprintf("%x", mac.Sum(nil))
}

func hasNonEmptyCollection(value any) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return false
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringValue(values[key])); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func maskAccount(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "已识别登录账号"
	}
	if at := strings.IndexByte(value, '@'); at > 1 {
		return value[:1] + "***" + value[at:]
	}
	runes := []rune(value)
	if len(runes) <= 3 {
		return "***"
	}
	return string(runes[:2]) + "***" + string(runes[len(runes)-1:])
}

func truncateText(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
