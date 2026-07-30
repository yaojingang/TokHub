package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestAIBrowserConnectorPairClaimAndComplete(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TOKHUB_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TOKHUB_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewRepository(db)
	suffix := uuid.NewString()
	userID := "usr_browser_" + suffix
	orgID := "org_browser_" + suffix
	if _, err := db.Exec(ctx, `
		insert into users(id,email,password_hash,name,avatar,status,role,email_verified_at)
		values($1,$2,'integration-test','浏览器连接器测试','B','active','user',now())
	`, userID, suffix+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		insert into orgs(id,name,slug,plan,status)
		values($1,'浏览器连接器测试',$2,'starter','active')
	`, orgID, "browser-connector-it-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		insert into org_members(org_id,user_id,role,status)
		values($1,$2,'owner','active')
	`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `delete from orgs where id=$1`, orgID)
		_, _ = db.Exec(context.Background(), `delete from users where id=$1`, userID)
	})

	created, err := repo.CreateAIBrowserConnector(ctx, userID, orgID, "我的 Chrome")
	if err != nil {
		t.Fatal(err)
	}
	if created.Connector.ID == "" || len(created.PairingCode) < 32 {
		t.Fatalf("pairing response is incomplete: %#v", created)
	}
	if _, err := db.Exec(ctx, `update users set status='disabled' where id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PairAIBrowserConnector(ctx, created.PairingCode); !errors.Is(err, ErrAIBrowserConnectorPairingInvalid) {
		t.Fatalf("disabled user could pair a connector: %v", err)
	}
	if _, err := db.Exec(ctx, `update users set status='active' where id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PairAIBrowserConnector(ctx, "invalid-code"); !errors.Is(err, ErrAIBrowserConnectorPairingInvalid) {
		t.Fatalf("invalid pairing code was accepted: %v", err)
	}
	paired, err := repo.PairAIBrowserConnector(ctx, created.PairingCode)
	if err != nil {
		t.Fatal(err)
	}
	if paired.DeviceToken == "" || paired.Connector.ID != created.Connector.ID {
		t.Fatalf("pairing did not issue a device token: %#v", paired)
	}
	if _, err := repo.PairAIBrowserConnector(ctx, created.PairingCode); !errors.Is(err, ErrAIBrowserConnectorPairingInvalid) {
		t.Fatalf("pairing code was reusable: %v", err)
	}
	authenticated, err := repo.AuthenticateAIBrowserConnector(ctx, paired.DeviceToken)
	if err != nil || authenticated.ID != created.Connector.ID {
		t.Fatalf("device token authentication failed: connector=%#v err=%v", authenticated, err)
	}
	if _, err := repo.AIBrowserConnectorForOwner(ctx, "usr_other", orgID, created.Connector.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another user could read the connector: %v", err)
	}
	if _, err := repo.AuthenticateAIBrowserConnector(ctx, "invalid-device-token"); !errors.Is(err, ErrAIBrowserConnectorUnauthorized) {
		t.Fatalf("invalid device token was accepted: %v", err)
	}
	if _, err := db.Exec(ctx, `update users set status='disabled' where id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AuthenticateAIBrowserConnector(ctx, paired.DeviceToken); !errors.Is(err, ErrAIBrowserConnectorUnauthorized) {
		t.Fatalf("disabled user device token was accepted: %v", err)
	}
	if _, err := db.Exec(ctx, `update users set status='active' where id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `update org_members set status='removed' where org_id=$1 and user_id=$2`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AuthenticateAIBrowserConnector(ctx, paired.DeviceToken); !errors.Is(err, ErrAIBrowserConnectorUnauthorized) {
		t.Fatalf("removed member device token was accepted: %v", err)
	}
	if _, err := db.Exec(ctx, `update org_members set status='active' where org_id=$1 and user_id=$2`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `update orgs set status='disabled' where id=$1`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AuthenticateAIBrowserConnector(ctx, paired.DeviceToken); !errors.Is(err, ErrAIBrowserConnectorUnauthorized) {
		t.Fatalf("disabled organization device token was accepted: %v", err)
	}
	if _, err := db.Exec(ctx, `update orgs set status='active' where id=$1`, orgID); err != nil {
		t.Fatal(err)
	}
	heartbeat, err := repo.HeartbeatAIBrowserConnector(
		ctx,
		created.Connector.ID,
		"1.8.6-"+strings.Repeat("x", 100),
		"1.0.20-"+strings.Repeat("y", 100),
		[]string{"openai", "gemini", "deepseek"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(heartbeat.OpenCLIVersion)) != 64 || len([]rune(heartbeat.ExtensionVersion)) != 64 || !heartbeat.Online {
		t.Fatalf("heartbeat did not mark connector online: %#v", heartbeat)
	}
	if _, err := repo.CreateAIBrowserTask(ctx, AIBrowserTaskInput{
		ConnectorID: created.Connector.ID, OwnerUserID: "usr_other", OrgID: orgID,
		Provider: "deepseek", Action: "ask", Request: map[string]any{"prompt": "越权请求"},
		ExpiresAt: time.Now().Add(time.Minute),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another user could enqueue a connector task: %v", err)
	}

	task, err := repo.CreateAIBrowserTask(ctx, AIBrowserTaskInput{
		ConnectorID: created.Connector.ID, OwnerUserID: userID, OrgID: orgID,
		Provider: "deepseek", Action: "ask", Request: map[string]any{"prompt": "你好"},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimAIBrowserTask(ctx, created.Connector.ID, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != task.ID || claimed.LeaseToken == "" {
		t.Fatalf("task claim is incomplete: %#v", claimed)
	}
	if second, err := repo.ClaimAIBrowserTask(ctx, created.Connector.ID, 30*time.Second); err != nil || second != nil {
		t.Fatalf("claimed task was delivered twice: task=%#v err=%v", second, err)
	}
	if err := repo.CompleteAIBrowserTask(ctx, created.Connector.ID, task.ID, "wrong-lease", true, map[string]any{"content": "no"}, "", ""); !errors.Is(err, ErrAIBrowserTaskLeaseInvalid) {
		t.Fatalf("invalid lease completed a task: %v", err)
	}
	if err := repo.CompleteAIBrowserTask(ctx, created.Connector.ID, task.ID, claimed.LeaseToken, true, map[string]any{"content": "你好！"}, "", ""); err != nil {
		t.Fatal(err)
	}
	completed, err := repo.AIBrowserTaskForOwner(ctx, userID, orgID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.Response["content"] != "你好！" || len(completed.Request) != 0 {
		t.Fatalf("completed task was not normalized or scrubbed: %#v", completed)
	}
	if err := repo.ScrubAIBrowserTaskPayloadForOwner(ctx, userID, orgID, task.ID); err != nil {
		t.Fatal(err)
	}
	scrubbed, err := repo.AIBrowserTaskForOwner(ctx, userID, orgID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(scrubbed.Request) != 0 || len(scrubbed.Response) != 0 {
		t.Fatalf("consumed task retained prompt or answer: %#v", scrubbed)
	}
	if _, err := db.Exec(ctx, `
		update ai_browser_tasks
		set request_json='{"prompt":"crash-retained"}'::jsonb,
			response_json='{"content":"crash-retained"}'::jsonb,
			completed_at=now()-interval '11 minutes'
		where id=$1
	`, task.ID); err != nil {
		t.Fatal(err)
	}
	maintained, err := repo.MaintainAIBrowserTasks(ctx, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if maintained < 1 {
		t.Fatalf("stale browser task maintenance changed %d rows, want at least 1", maintained)
	}
	scrubbedAfterCrash, err := repo.AIBrowserTaskForOwner(ctx, userID, orgID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(scrubbedAfterCrash.Request) != 0 || len(scrubbedAfterCrash.Response) != 0 {
		t.Fatalf("stale task payload survived maintenance: %#v", scrubbedAfterCrash)
	}

	activeTask, err := repo.CreateAIBrowserTask(ctx, AIBrowserTaskInput{
		ConnectorID: created.Connector.ID, OwnerUserID: userID, OrgID: orgID,
		Provider: "deepseek", Action: "ask", Request: map[string]any{"prompt": "撤销前敏感输入"},
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	activeClaim, err := repo.ClaimAIBrowserTask(ctx, created.Connector.ID, 30*time.Second)
	if err != nil || activeClaim == nil || activeClaim.ID != activeTask.ID {
		t.Fatalf("active task claim failed: task=%#v err=%v", activeClaim, err)
	}
	if _, err := db.Exec(ctx, `
		update ai_browser_tasks set response_json='{"content":"撤销前敏感输出"}'::jsonb where id=$1
	`, activeTask.ID); err != nil {
		t.Fatal(err)
	}

	connectionID := "aic_browser_" + suffix
	if _, err := db.Exec(ctx, `
		insert into ai_connections(
			id,owner_user_id,org_id,provider,product_line,region,auth_method,protocol,
			adapter_type,endpoint,provider_config,display_name,status,validation_stage,
			auth_status,sharing_scope,risk_level,provider_adapter_version,terms_ack_version
		)
		values(
			$1,$2,$3,'deepseek','DeepSeek Web','Global','opencli_browser','openai_compatible',
			'openai-compatible',$4,jsonb_build_object('connectorId',$5::text),'浏览器连接撤销测试',
			'active','browser_login','active','personal','experimental','opencli-browser-v1',
			'opencli-personal-browser-experimental-v1'
		)
	`, connectionID, userID, orgID, "browser+opencli://"+created.Connector.ID+"/deepseek", created.Connector.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		insert into ai_connection_secrets(
			connection_id,ciphertext,nonce,mask,fingerprint,encryption_key_id,fingerprint_key_id,
			algorithm,secret_type,payload_format,subject_fingerprint
		)
		values($1,'ciphertext','nonce','Local Browser · Te***r','reference-fingerprint',
			'test-key','test-fingerprint-key','aes-256-gcm','browser_connector',
			'browser_connector_v1',$2)
	`, connectionID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	_, duplicateErr := repo.CreateAIConnection(ctx, AIConnectionCreateInput{
		OwnerUserID: userID,
		OrgID:       orgID,
		Provider:    "deepseek",
		ProductLine: "DeepSeek Web",
		Region:      "Global",
		Protocol:    "openai_compatible",
		AdapterType: "openai-compatible",
		Endpoint:    "browser+opencli://another-connector/deepseek",
		ProviderConfig: map[string]any{
			"connectorId": "another-connector",
		},
		DisplayName: "重复浏览器连接",
		Models:      []string{"deepseek-web"},
		Credential: AIConnectionSecret{
			Ciphertext:         "other-ciphertext",
			Nonce:              "other-nonce",
			Mask:               "Local Browser · Ot***r",
			Fingerprint:        "other-reference-fingerprint",
			EncryptionKeyID:    "test-key",
			FingerprintKeyID:   "test-fingerprint-key",
			Algorithm:          "aes-256-gcm",
			SecretType:         "browser_connector",
			PayloadFormat:      "browser_connector_v1",
			SubjectFingerprint: strings.Repeat("b", 64),
		},
		Validation:             AIConnectionValidation{OK: true, Stage: "browser_login", ModelCount: 1},
		AuthMethod:             "opencli_browser",
		AuthStatus:             "active",
		SharingScope:           "personal",
		RiskLevel:              "experimental",
		ProviderAdapterVersion: "opencli-browser-v1",
	})
	if !errors.Is(duplicateErr, ErrAIConnectionDuplicate) {
		t.Fatalf("second browser connection for the same owner/provider was accepted: %v", duplicateErr)
	}
	riskNow := time.Now().UTC().Truncate(time.Second)
	riskPolicy := AIBrowserRiskPolicy{
		MinimumInterval: 15 * time.Second,
		HourlyLimit:     2,
		DailyLimit:      3,
	}
	firstRisk, err := repo.ReserveAIBrowserConnectionRequest(
		ctx, userID, orgID, connectionID, riskPolicy, riskNow,
	)
	if err != nil || !firstRisk.Allowed || firstRisk.Risk.RequestsHour != 1 {
		t.Fatalf("first account-scoped request = %#v, err=%v", firstRisk, err)
	}
	tooFast, err := repo.ReserveAIBrowserConnectionRequest(
		ctx, userID, orgID, connectionID, riskPolicy, riskNow.Add(5*time.Second),
	)
	if err != nil || tooFast.Allowed || tooFast.Reason != "minimum_interval" || tooFast.RetryAt == nil {
		t.Fatalf("minimum interval decision = %#v, err=%v", tooFast, err)
	}
	secondRisk, err := repo.ReserveAIBrowserConnectionRequest(
		ctx, userID, orgID, connectionID, riskPolicy, riskNow.Add(16*time.Second),
	)
	if err != nil || !secondRisk.Allowed || secondRisk.Risk.RequestsHour != 2 {
		t.Fatalf("second account-scoped request = %#v, err=%v", secondRisk, err)
	}
	hourlyLimited, err := repo.ReserveAIBrowserConnectionRequest(
		ctx, userID, orgID, connectionID, riskPolicy, riskNow.Add(32*time.Second),
	)
	if err != nil || hourlyLimited.Allowed || hourlyLimited.Reason != "hourly_limit" {
		t.Fatalf("hourly limit decision = %#v, err=%v", hourlyLimited, err)
	}
	for index := 0; index < 3; index++ {
		recorded, recordErr := repo.RecordAIBrowserConnectionResult(
			ctx, userID, orgID, connectionID, false, "rate_limited",
			riskNow.Add(time.Duration(index+1)*time.Minute),
		)
		if recordErr != nil {
			t.Fatal(recordErr)
		}
		if index == 0 {
			pauseDuringCooldown, pauseErr := repo.SetAIBrowserConnectionPaused(
				ctx, userID, orgID, connectionID, true,
			)
			if pauseErr != nil || pauseDuringCooldown.State != "cooldown" {
				t.Fatalf("manual pause bypassed provider cooldown: %#v, err=%v", pauseDuringCooldown, pauseErr)
			}
		}
		if index == 2 && (recorded.State != "cooldown" || recorded.CooldownUntil == nil ||
			recorded.CooldownUntil.Before(riskNow.Add(23*time.Hour))) {
			t.Fatalf("repeated provider rate limit did not open a durable cooldown: %#v", recorded)
		}
	}
	locked, err := repo.RecordAIBrowserConnectionResult(
		ctx, userID, orgID, connectionID, false, "security_challenge", riskNow.Add(4*time.Minute),
	)
	if err != nil || locked.State != "security_locked" || locked.LastChallengeAt == nil {
		t.Fatalf("security challenge state = %#v, err=%v", locked, err)
	}
	recovered, err := repo.RecordAIBrowserConnectionResult(
		ctx, userID, orgID, connectionID, true, "", riskNow.Add(5*time.Minute),
	)
	if err != nil || recovered.State != "normal" || recovered.ConsecutiveFailures != 0 {
		t.Fatalf("validated browser account did not recover: %#v, err=%v", recovered, err)
	}
	accessDenied, err := repo.RecordAIBrowserConnectionResult(
		ctx, userID, orgID, connectionID, false, "access_denied", riskNow.Add(6*time.Minute),
	)
	if err != nil || accessDenied.State != "security_locked" || accessDenied.CooldownUntil == nil ||
		accessDenied.CooldownUntil.Before(riskNow.Add(23*time.Hour)) {
		t.Fatalf("provider access denial did not open a durable security lock: %#v, err=%v", accessDenied, err)
	}
	stillLocked, err := repo.RecordAIBrowserConnectionResult(
		ctx, userID, orgID, connectionID, true, "", riskNow.Add(7*time.Minute),
	)
	if err != nil || stillLocked.State != "security_locked" || stillLocked.LastErrorCode != "access_denied" {
		t.Fatalf("early revalidation bypassed the access-denied lock: %#v, err=%v", stillLocked, err)
	}
	recovered, err = repo.RecordAIBrowserConnectionResult(
		ctx, userID, orgID, connectionID, true, "", riskNow.Add(25*time.Hour),
	)
	if err != nil || recovered.State != "normal" || recovered.CooldownUntil != nil {
		t.Fatalf("expired access-denied lock did not recover after revalidation: %#v, err=%v", recovered, err)
	}
	paused, err := repo.SetAIBrowserConnectionPaused(ctx, userID, orgID, connectionID, true)
	if err != nil || paused.State != "paused" {
		t.Fatalf("pause state = %#v, err=%v", paused, err)
	}
	resumed, err := repo.SetAIBrowserConnectionPaused(ctx, userID, orgID, connectionID, false)
	if err != nil || resumed.State != "normal" {
		t.Fatalf("resume state = %#v, err=%v", resumed, err)
	}
	if err := repo.RevokeAIBrowserConnector(ctx, userID, orgID, created.Connector.ID); err != nil {
		t.Fatalf("revoke connector with linked connection: %v", err)
	}
	var connectionStatus, authStatus string
	if err := db.QueryRow(ctx, `
		select status,auth_status from ai_connections where id=$1
	`, connectionID).Scan(&connectionStatus, &authStatus); err != nil {
		t.Fatal(err)
	}
	if connectionStatus != "disabled" || authStatus != "disabled" {
		t.Fatalf("linked connection states = (%q,%q), want disabled", connectionStatus, authStatus)
	}
	var taskStatus, leaseHash string
	var requestRaw, responseRaw []byte
	var leaseExpiresAt *time.Time
	if err := db.QueryRow(ctx, `
		select status,request_json,response_json,lease_hash,lease_expires_at
		from ai_browser_tasks where id=$1
	`, activeTask.ID).Scan(&taskStatus, &requestRaw, &responseRaw, &leaseHash, &leaseExpiresAt); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "cancelled" || string(requestRaw) != "{}" || string(responseRaw) != "{}" || leaseHash != "" || leaseExpiresAt != nil {
		t.Fatalf(
			"revoked task retained execution data: status=%q request=%s response=%s lease=%q expires=%v",
			taskStatus, requestRaw, responseRaw, leaseHash, leaseExpiresAt,
		)
	}
	if _, err := repo.AuthenticateAIBrowserConnector(ctx, paired.DeviceToken); !errors.Is(err, ErrAIBrowserConnectorUnauthorized) {
		t.Fatalf("revoked device token was still accepted: %v", err)
	}
	if _, err := repo.HeartbeatAIBrowserConnector(ctx, created.Connector.ID, "1.8.6", "", []string{"deepseek"}); !errors.Is(err, ErrAIBrowserConnectorUnauthorized) {
		t.Fatalf("revoked connector heartbeat was accepted: %v", err)
	}
}
