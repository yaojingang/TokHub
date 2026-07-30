package browserconnector

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBuildOpenCLICommandAllowsOnlyPersonalBrowserActions(t *testing.T) {
	tests := []struct {
		name     string
		task     Task
		expected []string
	}{
		{
			name:     "chatgpt status",
			task:     Task{Provider: "openai", Action: ActionStatus},
			expected: []string{"chatgpt", "whoami", "-f", "json"},
		},
		{
			name:     "gemini ask",
			task:     Task{Provider: "gemini", Action: ActionAsk, Prompt: "你好"},
			expected: []string{"gemini", "ask", "你好", "-f", "json", "--timeout", "90", "--new", "true"},
		},
		{
			name:     "deepseek ask",
			task:     Task{Provider: "deepseek", Action: ActionAsk, Prompt: "解释量子纠缠"},
			expected: []string{"deepseek", "ask", "解释量子纠缠", "-f", "json", "--timeout", "90", "--new", "true"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := BuildOpenCLICommand(test.task)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.expected) {
				t.Fatalf("command = %#v, want %#v", got, test.expected)
			}
		})
	}
}

func TestBuildOpenCLICommandRejectsGenericBrowserAccess(t *testing.T) {
	for _, task := range []Task{
		{Provider: "claude", Action: ActionAsk, Prompt: "hello"},
		{Provider: "deepseek", Action: "eval", Prompt: "document.cookie"},
		{Provider: "deepseek", Action: ActionAsk},
		{Provider: "deepseek", Action: ActionAsk, Prompt: string(make([]byte, MaxPromptBytes+1))},
	} {
		if command, err := BuildOpenCLICommand(task); err == nil {
			t.Fatalf("unsafe task produced command %#v: %#v", command, task)
		}
	}
}

func TestPromptFromOpenAIChatPreservesTextConversation(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "回答要简洁"},
			map[string]any{"role": "user", "content": "你好"},
			map[string]any{"role": "assistant", "content": "你好！"},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "解释一下 Go channel"},
			}},
		},
	}
	prompt, err := PromptFromOpenAIRequest("chat", payload)
	if err != nil {
		t.Fatal(err)
	}
	want := "System: 回答要简洁\n\nUser: 你好\n\nAssistant: 你好！\n\nUser: 解释一下 Go channel"
	if prompt != want {
		t.Fatalf("prompt = %q, want %q", prompt, want)
	}
}

func TestPromptFromOpenAIRequestRejectsStreamingToolsAndImages(t *testing.T) {
	tests := []map[string]any{
		{"stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		{"tools": []any{map[string]any{"type": "function"}}, "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		{
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "image_url", "image_url": "https://example.test/x.png"},
					},
				},
			},
		},
	}
	for _, payload := range tests {
		if prompt, err := PromptFromOpenAIRequest("chat", payload); err == nil {
			t.Fatalf("unsupported payload produced prompt %q: %#v", prompt, payload)
		}
	}
}

func TestNormalizeOpenCLIResultDoesNotExposeRawSessionData(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"success": true,
		"data": map[string]any{
			"content": "这是模型回答",
			"email":   "person@example.com",
			"cookie":  "sensitive",
			"token":   "sensitive",
		},
	})
	result, err := NormalizeOpenCLIResult(ActionAsk, raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "这是模型回答" || result.AccountMask != "" {
		t.Fatalf("unexpected normalized result: %#v", result)
	}
	encoded, _ := json.Marshal(result)
	if string(encoded) == string(raw) {
		t.Fatalf("normalizer retained raw response: %s", encoded)
	}
}

func TestNormalizeOpenCLIResultClassifiesLoginAndChallengeFailures(t *testing.T) {
	tests := []struct {
		raw     string
		code    string
		message string
	}{
		{
			`{"success":false,"error":"Please login to DeepSeek first; token=secret-token"}`,
			"login_required",
			"请先在 Chrome 中登录对应账号",
		},
		{
			`{"success":false,"message":"Captcha challenge is visible; cookie=sensitive-cookie"}`,
			"security_challenge",
			"浏览器出现验证码或安全验证，请在 Chrome 中处理后重试",
		},
	}
	for _, test := range tests {
		result, err := NormalizeOpenCLIResult(ActionAsk, []byte(test.raw))
		if err != nil {
			t.Fatal(err)
		}
		if result.OK || result.ErrorCode != test.code || result.ErrorMessage != test.message {
			t.Fatalf("result = %#v, want code %q and safe message %q", result, test.code, test.message)
		}
		if strings.Contains(result.ErrorMessage, "secret") || strings.Contains(result.ErrorMessage, "sensitive") {
			t.Fatalf("provider error leaked sensitive text: %#v", result)
		}
	}
}

func TestNormalizeOpenCLIResultClassifiesProviderProtectionFailures(t *testing.T) {
	tests := []struct {
		raw  string
		code string
	}{
		{`{"success":false,"error":"HTTP 429 too many requests; request_id=sensitive"}`, "rate_limited"},
		{`{"success":false,"error":"HTTP 403 forbidden; cookie=sensitive"}`, "access_denied"},
		{`{"success":false,"error":"Message textarea selector element not found"}`, "adapter_incompatible"},
		{`{"success":false,"error":"Network timeout while waiting for response"}`, "upstream_unavailable"},
	}
	for _, test := range tests {
		result, err := NormalizeOpenCLIResult(ActionAsk, []byte(test.raw))
		if err != nil {
			t.Fatal(err)
		}
		if result.OK || result.ErrorCode != test.code {
			t.Fatalf("result = %#v, want code %q", result, test.code)
		}
		if strings.Contains(result.ErrorMessage, "sensitive") {
			t.Fatalf("provider error leaked sensitive text: %#v", result)
		}
	}
}

func TestNormalizeOpenCLICommandFailureClassifiesOfficialErrorEnvelope(t *testing.T) {
	tests := []struct {
		raw  string
		code string
	}{
		{"error:\n  code: AUTH_REQUIRED\n  message: ChatGPT session cookie missing", "login_required"},
		{"error:\n  code: BROWSER_CONNECT\n  message: Extension is not connected", "upstream_unavailable"},
		{"error:\n  code: COMMAND_EXEC\n  message: textarea selector not found", "adapter_incompatible"},
	}
	for _, test := range tests {
		result := NormalizeOpenCLICommandFailure([]byte(test.raw))
		if result.OK || result.ErrorCode != test.code || strings.Contains(result.ErrorMessage, "session cookie") {
			t.Fatalf("failure result = %#v, want safe code %q", result, test.code)
		}
	}
}

func TestNormalizeOpenCLIResultAcceptsNativeArrayAndSnakeCaseOutput(t *testing.T) {
	answer, err := NormalizeOpenCLIResult(ActionAsk, []byte(`[{"response":"来自 OpenCLI 的回答"}]`))
	if err != nil || !answer.OK || answer.Content != "来自 OpenCLI 的回答" {
		t.Fatalf("answer = %#v, err=%v", answer, err)
	}
	status, err := NormalizeOpenCLIResult(ActionStatus, []byte(`[{"logged_in":false,"site":"deepseek"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if status.OK || status.ErrorCode != "login_required" {
		t.Fatalf("status = %#v", status)
	}
}

func TestNormalizeOpenCLIStatusReturnsMaskedIdentityFingerprint(t *testing.T) {
	status, err := NormalizeOpenCLIResult(
		ActionStatus,
		[]byte(`[{"logged_in":true,"site":"deepseek","user_id":"user-sensitive-123","name":"Alice"}]`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !status.OK || status.AccountMask != "Al***e" || len(status.AccountFingerprint) != 64 {
		t.Fatalf("status = %#v", status)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "user-sensitive-123") || strings.Contains(string(encoded), "Alice") {
		t.Fatalf("status leaked raw identity: %s", encoded)
	}
	bound := BindAccountFingerprint(strings.Repeat("a", 43), status.AccountFingerprint)
	otherDevice := BindAccountFingerprint(strings.Repeat("b", 43), status.AccountFingerprint)
	if !AccountFingerprintMatches(bound, bound) || AccountFingerprintMatches(bound, otherDevice) {
		t.Fatalf("device-bound fingerprints were not isolated: bound=%q other=%q", bound, otherDevice)
	}
	if !IsValidAccountFingerprint(bound) || IsValidAccountFingerprint("invalid") {
		t.Fatalf("fingerprint validity contract is inconsistent")
	}
}

func TestNormalizeOpenCLIResultRejectsGeminiNoResponseSentinel(t *testing.T) {
	result, err := NormalizeOpenCLIResult(
		ActionAsk,
		[]byte(`[{"response":"💬 [NO RESPONSE] No Gemini response within 90s."}]`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.ErrorCode != "opencli_failed" {
		t.Fatalf("Gemini timeout sentinel was accepted as a model answer: %#v", result)
	}
}

func TestSanitizeResultRejectsOversizeAndRemovesDeviceErrorText(t *testing.T) {
	if _, err := SanitizeResult(Result{OK: true, Content: strings.Repeat("x", MaxPromptBytes+1)}); err == nil {
		t.Fatal("oversize device result was accepted")
	}
	if _, err := SanitizeResult(Result{OK: true, AccountMask: "A***e", AccountFingerprint: "invalid"}); err == nil {
		t.Fatal("malformed account fingerprint was accepted")
	}
	result, err := SanitizeResult(Result{
		OK: false, ErrorCode: "unexpected_code", ErrorMessage: "token=secret-from-device",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "opencli_failed" || strings.Contains(result.ErrorMessage, "secret") {
		t.Fatalf("device error was not sanitized: %#v", result)
	}
}
