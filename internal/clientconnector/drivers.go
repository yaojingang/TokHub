package clientconnector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type CodexDriver struct {
	Factory RPCFactory
}

func (d CodexDriver) Identify(ctx context.Context) TaskResult {
	rpc, err := d.start(ctx)
	if err != nil {
		return driverFailure(err)
	}
	defer rpc.Close()
	accountRaw, err := rpc.Call(ctx, "account/read", map[string]any{"refreshToken": false})
	if err != nil {
		return driverFailure(err)
	}
	modelsRaw, err := rpc.Call(ctx, "model/list", map[string]any{})
	if err != nil {
		return driverFailure(err)
	}
	account := decodeObject(accountRaw)
	accountID := firstString(account, "account.id", "account.accountId", "account.email", "id", "accountId", "email")
	accountMask := firstString(account, "account.email", "email", "account.name", "name")
	if accountID == "" && accountMask == "" {
		return TaskResult{ErrorCode: "login_required", ErrorMessage: "Codex account is not logged in"}
	}
	if accountID == "" {
		accountID = accountMask
	}
	return TaskResult{
		OK: true, AccountID: accountID, AccountMask: maskAccount(accountMask),
		IdentityAssurance: "account", Models: collectModelIDs(decodeAny(modelsRaw)),
	}
}

func (d CodexDriver) Generate(ctx context.Context, payload TaskPayload) TaskResult {
	if strings.TrimSpace(payload.Prompt) == "" {
		return TaskResult{ErrorCode: "invalid_prompt", ErrorMessage: "Prompt is required"}
	}
	rpc, err := d.start(ctx)
	if err != nil {
		return driverFailure(err)
	}
	defer rpc.Close()
	accountRaw, accountErr := rpc.Call(ctx, "account/read", map[string]any{"refreshToken": false})
	if accountErr != nil {
		return driverFailure(accountErr)
	}
	account := decodeObject(accountRaw)
	accountID := firstString(account, "account.id", "account.accountId", "account.email", "id", "accountId", "email")
	if accountID == "" {
		return TaskResult{ErrorCode: "login_required", ErrorMessage: "Codex account is not logged in"}
	}
	threadID := strings.TrimSpace(payload.PreviousSessionRef)
	threadConfig := map[string]any{
		"web_search": "disabled",
		"features": map[string]any{
			"apps": false, "browser_use": false, "computer_use": false,
			"shell_tool": false, "unified_exec": false, "web_search": false,
		},
	}
	if threadID == "" {
		resultRaw, err := rpc.Call(ctx, "thread/start", map[string]any{
			"cwd": "/workspace", "approvalPolicy": "never", "sandbox": "read-only",
			"model": nil, "config": threadConfig,
			"baseInstructions": "Return text only. Tool calls, file access, commands, applications, MCP, subagents, and web access are prohibited.",
		})
		if err != nil {
			return driverFailure(err)
		}
		threadID = firstString(decodeObject(resultRaw), "thread.id", "id", "threadId")
	} else {
		if _, err := rpc.Call(ctx, "thread/resume", map[string]any{
			"threadId": threadID, "cwd": "/workspace", "approvalPolicy": "never",
			"sandbox": "read-only", "config": threadConfig,
			"baseInstructions": "Return text only. Tool calls, file access, commands, applications, MCP, subagents, and web access are prohibited.",
		}); err != nil {
			return driverFailure(err)
		}
	}
	if threadID == "" {
		return TaskResult{ErrorCode: "adapter_incompatible", ErrorMessage: "Codex did not return a thread identifier"}
	}
	resultRaw, err := rpc.Call(ctx, "turn/start", map[string]any{
		"threadId":       threadID,
		"input":          []map[string]any{{"type": "text", "text": payload.Prompt}},
		"approvalPolicy": "never",
	})
	if err != nil {
		return driverFailure(err)
	}
	resultValue := decodeAny(resultRaw)
	if forbiddenClientValue(resultValue) {
		return TaskResult{ErrorCode: "tool_event", ErrorMessage: "Codex attempted a blocked capability", ToolEvent: true}
	}
	if responseFailed(resultValue) {
		return TaskResult{ErrorCode: "client_error", ErrorMessage: "Codex turn failed"}
	}
	content := extractText(resultValue)
	actualModel := firstString(decodeObject(resultRaw), "model", "turn.model", "thread.model")
	completed := responseCompleted(resultValue)
	for !completed {
		select {
		case <-ctx.Done():
			return TaskResult{ErrorCode: "cancelled", ErrorMessage: "Codex turn was interrupted"}
		case event, open := <-rpc.Events():
			if !open {
				return TaskResult{ErrorCode: "client_unavailable", ErrorMessage: "Codex app-server stopped before the turn completed"}
			}
			if forbiddenClientEvent(event) {
				return TaskResult{ErrorCode: "tool_event", ErrorMessage: "Codex attempted a blocked capability", ToolEvent: true}
			}
			content += eventText(event)
			if model := firstString(event, "params.model", "params.turn.model", "params.thread.model"); model != "" {
				actualModel = model
			}
			if eventFailed(event) {
				return TaskResult{ErrorCode: "client_error", ErrorMessage: "Codex turn failed"}
			}
			completed = eventCompleted(event)
		case <-time.After(90 * time.Second):
			return TaskResult{ErrorCode: "timeout", ErrorMessage: "Codex turn timed out"}
		}
	}
	if strings.TrimSpace(content) == "" {
		return TaskResult{ErrorCode: "empty_response", ErrorMessage: "Codex returned no text"}
	}
	return TaskResult{OK: true, Content: content, SessionRef: threadID, AccountID: accountID,
		AccountMask:       maskAccount(firstString(account, "account.email", "email", "account.name", "name")),
		IdentityAssurance: "account", ActualModel: actualModel}
}

func (d CodexDriver) DeleteSession(ctx context.Context, sessionRef string) TaskResult {
	if strings.TrimSpace(sessionRef) == "" {
		return TaskResult{OK: true}
	}
	rpc, err := d.start(ctx)
	if err != nil {
		return driverFailure(err)
	}
	defer rpc.Close()
	if _, err := rpc.Call(ctx, "thread/archive", map[string]any{"threadId": sessionRef}); err != nil {
		return driverFailure(err)
	}
	return TaskResult{OK: true}
}

func (d CodexDriver) start(ctx context.Context) (RPC, error) {
	factory := d.Factory
	if factory == nil {
		factory = StdioRPCFactory{}
	}
	rpc, err := factory.Start(ctx, "codex",
		"--config", `web_search="disabled"`,
		"--config", `features.apps=false`,
		"--config", `features.browser_use=false`,
		"--config", `features.computer_use=false`,
		"--config", `features.shell_tool=false`,
		"--config", `features.unified_exec=false`,
		"app-server")
	if err != nil {
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}
	if _, err := rpc.Call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "tokhub_official_connector", "title": "TokHub Official Client Connector", "version": "2.0.0-rc.2"},
		"capabilities": map[string]any{"experimentalApi": false},
	}); err != nil {
		rpc.Close()
		return nil, err
	}
	_ = rpc.Notify("initialized", map[string]any{})
	return rpc, nil
}

type GrokDriver struct {
	Factory RPCFactory
}

func (d GrokDriver) Identify(ctx context.Context) TaskResult {
	rpc, err := d.start(ctx)
	if err != nil {
		return driverFailure(err)
	}
	defer rpc.Close()
	raw, err := rpc.Call(ctx, "authenticate", map[string]any{"methodId": "cached_token"})
	if err != nil {
		return driverFailure(err)
	}
	account := decodeObject(raw)
	accountID := firstString(account, "account.id", "accountId", "id", "email")
	accountMask := firstString(account, "account.email", "email", "account.name", "name")
	assurance := "account"
	if accountID == "" {
		accountID, err = EnsureGrokDeviceIdentity()
		if err != nil {
			return driverFailure(err)
		}
		assurance = "device"
	}
	return TaskResult{OK: true, AccountID: accountID, AccountMask: maskAccount(accountMask), IdentityAssurance: assurance, Models: []string{"grok-personal"}}
}

func (d GrokDriver) Generate(ctx context.Context, payload TaskPayload) TaskResult {
	if strings.TrimSpace(payload.Prompt) == "" {
		return TaskResult{ErrorCode: "invalid_prompt", ErrorMessage: "Prompt is required"}
	}
	rpc, err := d.start(ctx)
	if err != nil {
		return driverFailure(err)
	}
	defer rpc.Close()
	authRaw, err := rpc.Call(ctx, "authenticate", map[string]any{"methodId": "cached_token"})
	if err != nil {
		return driverFailure(err)
	}
	account := decodeObject(authRaw)
	accountID := firstString(account, "account.id", "accountId", "id", "email")
	assurance := "account"
	if accountID == "" {
		accountID, err = EnsureGrokDeviceIdentity()
		if err != nil {
			return driverFailure(err)
		}
		assurance = "device"
	}
	sessionID := strings.TrimSpace(payload.PreviousSessionRef)
	if sessionID == "" {
		raw, err := rpc.Call(ctx, "session/new", map[string]any{
			"cwd": "/workspace", "mcpServers": []any{},
		})
		if err != nil {
			return driverFailure(err)
		}
		sessionID = firstString(decodeObject(raw), "sessionId", "session.id", "id")
	} else {
		if _, err := rpc.Call(ctx, "session/load", map[string]any{
			"sessionId": sessionID, "cwd": "/workspace", "mcpServers": []any{},
		}); err != nil {
			return driverFailure(err)
		}
	}
	if sessionID == "" {
		return TaskResult{ErrorCode: "adapter_incompatible", ErrorMessage: "Grok ACP did not return a session identifier"}
	}
	raw, err := rpc.Call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": payload.Prompt}},
	})
	if err != nil {
		return driverFailure(err)
	}
	resultValue := decodeAny(raw)
	if forbiddenClientValue(resultValue) {
		return TaskResult{ErrorCode: "tool_event", ErrorMessage: "Grok attempted a blocked capability", ToolEvent: true}
	}
	content := extractText(resultValue)
	actualModel := firstString(decodeObject(raw), "model", "modelId", "session.model", "session.modelId")
	// ACP session/prompt completes after the prompt turn. Drain already-buffered updates.
	for {
		select {
		case event, open := <-rpc.Events():
			if !open {
				return TaskResult{ErrorCode: "client_unavailable", ErrorMessage: "Grok Build ACP stopped before the turn completed"}
			}
			if forbiddenClientEvent(event) {
				return TaskResult{ErrorCode: "tool_event", ErrorMessage: "Grok attempted a blocked capability", ToolEvent: true}
			}
			content += eventText(event)
			if model := firstString(event, "params.model", "params.modelId", "params.update.model", "params.update.modelId"); model != "" {
				actualModel = model
			}
		default:
			if strings.TrimSpace(content) == "" {
				return TaskResult{ErrorCode: "empty_response", ErrorMessage: "Grok returned no text"}
			}
			if actualModel == "" {
				actualModel = "grok-personal"
			}
			return TaskResult{OK: true, Content: content, SessionRef: sessionID, AccountID: accountID,
				AccountMask:       maskAccount(firstString(account, "account.email", "email", "account.name", "name")),
				IdentityAssurance: assurance, ActualModel: actualModel}
		}
	}
}

func (d GrokDriver) DeleteSession(ctx context.Context, sessionRef string) TaskResult {
	if strings.TrimSpace(sessionRef) == "" {
		return TaskResult{OK: true}
	}
	rpc, err := d.start(ctx)
	if err != nil {
		return driverFailure(err)
	}
	defer rpc.Close()
	// ACP does not define a portable session-delete method. Stop any live turn;
	// the dedicated volume cleaner removes the persisted record at retention.
	_, _ = rpc.Call(ctx, "session/cancel", map[string]any{"sessionId": sessionRef})
	return TaskResult{OK: true}
}

func (d GrokDriver) start(ctx context.Context) (RPC, error) {
	factory := d.Factory
	if factory == nil {
		factory = StdioRPCFactory{}
	}
	rpc, err := factory.Start(ctx, "grok", "agent", "stdio")
	if err != nil {
		return nil, fmt.Errorf("start Grok Build ACP: %w", err)
	}
	if _, err := rpc.Call(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]any{"name": "tokhub_official_connector", "title": "TokHub Official Client Connector", "version": "2.0.0-rc.2"},
		"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": false, "writeTextFile": false}, "terminal": false},
	}); err != nil {
		rpc.Close()
		return nil, err
	}
	return rpc, nil
}

func driverFailure(err error) TaskResult {
	if err == nil {
		return TaskResult{ErrorCode: "client_error", ErrorMessage: "Official client request failed"}
	}
	message := strings.ToLower(err.Error())
	code := "client_error"
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(message, "timeout"):
		code = "timeout"
	case strings.Contains(message, "401"), strings.Contains(message, "unauthorized"), strings.Contains(message, "login"):
		code = "unauthorized"
	case strings.Contains(message, "403"), strings.Contains(message, "forbidden"), strings.Contains(message, "challenge"):
		code = "security_challenge"
	case strings.Contains(message, "429"), strings.Contains(message, "rate limit"):
		code = "rate_limited"
	case strings.Contains(message, "not found"), strings.Contains(message, "executable file"):
		code = "client_unavailable"
	}
	return TaskResult{ErrorCode: code, ErrorMessage: truncateMessage(err.Error())}
}

func decodeAny(raw json.RawMessage) any {
	var value any
	_ = json.Unmarshal(raw, &value)
	return value
}

func decodeObject(raw json.RawMessage) map[string]any {
	value, _ := decodeAny(raw).(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func firstString(value map[string]any, paths ...string) string {
	for _, path := range paths {
		var current any = value
		for _, segment := range strings.Split(path, ".") {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = object[segment]
		}
		if text, ok := current.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func collectModelIDs(value any) []string {
	seen := map[string]struct{}{}
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			for _, key := range []string{"id", "model", "slug"} {
				if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
					seen[strings.TrimSpace(text)] = struct{}{}
					break
				}
			}
			for _, key := range []string{"data", "models", "items"} {
				visit(typed[key])
			}
		}
	}
	visit(value)
	out := make([]string, 0, len(seen))
	for model := range seen {
		out = append(out, model)
	}
	sort.Strings(out)
	return out
}

func forbiddenClientEvent(event map[string]any) bool {
	for _, path := range []string{"method", "type", "params.type", "params.item.type", "params.update.sessionUpdate", "params.update.type"} {
		if isForbiddenCapability(normalizeCapabilityDescriptor(firstString(event, path))) {
			return true
		}
	}
	return forbiddenClientValue(event)
}

func forbiddenClientValue(value any) bool {
	var visit func(any) bool
	visit = func(current any) bool {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				normalizedKey := normalizeCapabilityDescriptor(key)
				if isForbiddenCapability(normalizedKey) && capabilityValueEnabled(child) {
					return true
				}
				switch normalizedKey {
				case "method", "type", "kind", "sessionupdate":
					if descriptor, ok := child.(string); ok && isForbiddenCapability(normalizeCapabilityDescriptor(descriptor)) {
						return true
					}
				}
				if visit(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if visit(child) {
					return true
				}
			}
		}
		return false
	}
	return visit(value)
}

func normalizeCapabilityDescriptor(value string) string {
	return strings.ToLower(strings.NewReplacer("-", "_", "/", "_", ".", "_").Replace(strings.TrimSpace(value)))
}

func isForbiddenCapability(value string) bool {
	for _, forbidden := range []string{"tool_call", "tool_use", "command_execution", "commandexec", "file_change", "filechange", "mcp", "web_search", "websearch", "subagent", "terminal"} {
		if strings.Contains(value, forbidden) {
			return true
		}
	}
	return false
}

func capabilityValueEnabled(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized != "" && normalized != "false" && normalized != "disabled" && normalized != "none"
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func eventText(event map[string]any) string {
	method := strings.ToLower(firstString(event, "method"))
	if strings.Contains(method, "delta") || strings.Contains(method, "update") || strings.Contains(method, "message") {
		for _, path := range []string{"params.delta", "params.text", "params.message", "params.update.content.text", "params.update.text", "params.item.text"} {
			if text := firstString(event, path); text != "" {
				return text
			}
		}
	}
	return ""
}

func eventCompleted(event map[string]any) bool {
	method := strings.ToLower(firstString(event, "method", "type"))
	return strings.Contains(method, "turn/completed") || strings.Contains(method, "turn.completed") || strings.Contains(method, "turn/failed")
}

func eventFailed(event map[string]any) bool {
	method := strings.ToLower(firstString(event, "method", "type"))
	if strings.Contains(method, "turn/failed") || strings.Contains(method, "turn.failed") {
		return true
	}
	status := strings.ToLower(firstString(event, "params.turn.status", "params.status"))
	return status == "failed"
}

func responseCompleted(value any) bool {
	object, _ := value.(map[string]any)
	status := strings.ToLower(firstString(object, "status", "turn.status"))
	return status == "completed" || status == "failed"
}

func responseFailed(value any) bool {
	object, _ := value.(map[string]any)
	status := strings.ToLower(firstString(object, "status", "turn.status"))
	return status == "failed" || status == "cancelled"
}

func extractText(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, path := range []string{"output_text", "text", "content.text", "message.text", "turn.output_text"} {
		if text := firstString(object, path); text != "" {
			return text
		}
	}
	return ""
}

func maskAccount(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "device identity"
	}
	if at := strings.Index(value, "@"); at > 1 {
		return value[:1] + "***" + value[at:]
	}
	if len(value) > 8 {
		return value[:3] + "***" + value[len(value)-3:]
	}
	return "***"
}

func truncateMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return value[:512]
	}
	return value
}
