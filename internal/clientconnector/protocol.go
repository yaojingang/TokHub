package clientconnector

import (
	"context"
	"time"
)

const (
	ActionIdentify               = "identify"
	ActionGenerate               = "generate"
	ActionInterrupt              = "interrupt"
	ActionDeleteSession          = "delete_session"
	SealedSessionReferencePrefix = "device-v1."
)

type TaskPayload struct {
	Provider           string `json:"provider"`
	Action             string `json:"action"`
	Prompt             string `json:"prompt,omitempty"`
	Model              string `json:"model,omitempty"`
	PreviousSessionRef string `json:"previousSessionRef,omitempty"`
	Stream             bool   `json:"stream,omitempty"`
}

type TaskResult struct {
	OK                bool     `json:"ok"`
	Content           string   `json:"content,omitempty"`
	SessionRef        string   `json:"sessionRef,omitempty"`
	AccountID         string   `json:"accountId,omitempty"`
	AccountMask       string   `json:"accountMask,omitempty"`
	IdentityAssurance string   `json:"identityAssurance,omitempty"`
	Models            []string `json:"models,omitempty"`
	ActualModel       string   `json:"actualModel,omitempty"`
	ClientVersion     string   `json:"clientVersion,omitempty"`
	ErrorCode         string   `json:"errorCode,omitempty"`
	ErrorMessage      string   `json:"errorMessage,omitempty"`
	RetryAfterSeconds int      `json:"retryAfterSeconds,omitempty"`
	ToolEvent         bool     `json:"toolEvent,omitempty"`
}

type Heartbeat struct {
	ConnectorVersion string                    `json:"connectorVersion"`
	CodexVersion     string                    `json:"codexVersion,omitempty"`
	GrokVersion      string                    `json:"grokVersion,omitempty"`
	Capabilities     []string                  `json:"capabilities"`
	Identity         map[string]IdentityStatus `json:"identity,omitempty"`
}

type IdentityStatus struct {
	LoggedIn          bool      `json:"loggedIn"`
	AccountMask       string    `json:"accountMask,omitempty"`
	IdentityAssurance string    `json:"identityAssurance,omitempty"`
	CheckedAt         time.Time `json:"checkedAt"`
}

type ClaimedTask struct {
	ID         string      `json:"id"`
	Provider   string      `json:"provider"`
	Action     string      `json:"action"`
	LeaseToken string      `json:"leaseToken"`
	ExpiresAt  time.Time   `json:"expiresAt"`
	Payload    TaskPayload `json:"payload"`
}

type CompleteTaskRequest struct {
	LeaseToken string     `json:"leaseToken"`
	Result     TaskResult `json:"result"`
}

type Driver interface {
	Identify(ctx context.Context) TaskResult
	Generate(ctx context.Context, payload TaskPayload) TaskResult
	DeleteSession(ctx context.Context, sessionRef string) TaskResult
}
