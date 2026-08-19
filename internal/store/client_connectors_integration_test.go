package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestAIClientConnectorTaskSessionAndRiskLifecycle(t *testing.T) {
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
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	userID := "usr_client_" + suffix
	orgID := "org_client_" + suffix
	connectionID := "aic_client_" + suffix
	gatewayID := "gw_client_" + suffix
	gatewayKeyID := "gwk_client_" + suffix

	if _, err := db.Exec(ctx, `
		insert into users(id,email,password_hash,name,avatar,status,role,email_verified_at)
		values($1,$2,'integration-test','官方客户端测试','C','active','user',now())
	`, userID, suffix+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		insert into orgs(id,name,slug,plan,status)
		values($1,'官方客户端测试',$2,'starter','active')
	`, orgID, "official-client-it-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into org_members(org_id,user_id,role,status) values($1,$2,'owner','active')`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `delete from orgs where id=$1`, orgID)
		_, _ = db.Exec(context.Background(), `delete from users where id=$1`, userID)
	})

	created, err := repo.CreateAIClientConnector(ctx, userID, orgID, "我的官方客户端")
	if err != nil {
		t.Fatal(err)
	}
	if created.Connector.Status != "pending" || len(created.PairingCode) < 32 || created.Connector.PairingExpiresAt == nil {
		t.Fatalf("incomplete pairing result: %#v", created)
	}
	if _, err := repo.CreateAIClientConnector(ctx, userID, orgID, "重复连接器"); err == nil {
		t.Fatal("a second pending connector for the same owner was accepted")
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyText := base64.RawURLEncoding.EncodeToString(publicKey)
	if _, err := repo.PairAIClientConnector(ctx, "invalid-code", publicKeyText); !errors.Is(err, ErrAIClientConnectorPairingInvalid) {
		t.Fatalf("invalid pairing code was accepted: %v", err)
	}
	paired, err := repo.PairAIClientConnector(ctx, created.PairingCode, publicKeyText)
	if err != nil {
		t.Fatal(err)
	}
	if paired.Connector.ID != created.Connector.ID || len(paired.DeviceToken) < 40 || paired.Connector.PublicKey != publicKeyText {
		t.Fatalf("pairing did not bind the signed device: %#v", paired)
	}
	if _, err := repo.PairAIClientConnector(ctx, created.PairingCode, publicKeyText); !errors.Is(err, ErrAIClientConnectorPairingInvalid) {
		t.Fatalf("one-time pairing code was reusable: %v", err)
	}
	authenticated, err := repo.AuthenticateAIClientConnector(ctx, paired.DeviceToken)
	if err != nil || authenticated.ID != created.Connector.ID {
		t.Fatalf("device token authentication failed: connector=%#v err=%v", authenticated, err)
	}
	if _, err := repo.AuthenticateAIClientConnector(ctx, "invalid-device-token"); !errors.Is(err, ErrAIClientConnectorUnauthorized) {
		t.Fatalf("invalid device token was accepted: %v", err)
	}
	var tokenHash string
	if err := db.QueryRow(ctx, `select token_hash from ai_client_connectors where id=$1`, created.Connector.ID).Scan(&tokenHash); err != nil {
		t.Fatal(err)
	}
	if tokenHash == "" || strings.Contains(tokenHash, paired.DeviceToken) {
		t.Fatal("raw device token was retained in the database")
	}
	heartbeat, err := repo.HeartbeatAIClientConnector(ctx, created.Connector.ID,
		"2.0.0-rc.2"+strings.Repeat("x", 80), "codex-cli 0.148.0", "grok 1.0.5",
		[]string{"chatgpt", "grok", "unknown", "chatgpt"},
		map[string]any{"chatgpt": map[string]any{"loggedIn": true, "identityAssurance": "account"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !heartbeat.Online || len([]rune(heartbeat.ConnectorVersion)) != 64 || strings.Join(heartbeat.Capabilities, ",") != "chatgpt,grok" {
		t.Fatalf("heartbeat was not normalized: %#v", heartbeat)
	}

	if _, err := db.Exec(ctx, `
		insert into ai_connections(
			id,owner_user_id,org_id,provider,product_line,region,auth_method,protocol,
			adapter_type,endpoint,provider_config,display_name,status,validation_stage,
			auth_status,sharing_scope,risk_level,provider_adapter_version,terms_ack_version
		) values(
			$1,$2,$3,'openai','ChatGPT Codex','global','official_client','openai',
			'openai',$4,jsonb_build_object('connectorId',$5::text),'ChatGPT 个人客户端',
			'active','official_client_identity','active','personal','elevated','codex-app-server-v1','provider-policy-2026-08-19'
		)
	`, connectionID, userID, orgID, "client+codex://"+created.Connector.ID, created.Connector.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateAIClientConnectionRuntimeDetails(ctx, userID, orgID, connectionID,
		[]string{"gpt-current", "gpt-fast"}, "codex-cli 0.148.0"); err != nil {
		t.Fatal(err)
	}
	var actualModels []string
	var clientVersion string
	if err := db.QueryRow(ctx, `
		select array(select jsonb_array_elements_text(provider_config->'actualModels')),
			provider_config->>'clientVersion'
		from ai_connections where id=$1
	`, connectionID).Scan(&actualModels, &clientVersion); err != nil {
		t.Fatal(err)
	}
	if strings.Join(actualModels, ",") != "gpt-current,gpt-fast" || clientVersion != "codex-cli 0.148.0" {
		t.Fatalf("runtime details were not persisted: models=%v version=%q", actualModels, clientVersion)
	}
	if _, err := db.Exec(ctx, `
		insert into gateways(id,org_id,name,slug,base_url,policy,status,qps_limit,quota_month,created_by)
		values($1,$2,'官方客户端测试',$3,'https://example.test/gateway/v1','latency','active',1,80,$4)
	`, gatewayID, orgID, "official-client-"+suffix, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		insert into gateway_keys(id,org_id,gateway_id,name,key_hash,key_prefix,key_mask,quota_month,qps_limit,status,created_by)
		values($1,$2,$3,'测试密钥',$4,'sk-th-it','sk-th-it-••••',80,1,'active',$5)
	`, gatewayKeyID, orgID, gatewayID, "hash-"+suffix, userID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.CreateAIClientTask(ctx, AIClientTaskInput{
		ConnectorID: created.Connector.ID, OwnerUserID: userID, OrgID: orgID,
		ConnectionID: connectionID, Provider: "openai", Action: "generate", ExpiresAt: time.Now().Add(time.Minute),
	}); err == nil {
		t.Fatal("task metadata without an encrypted Redis payload key was accepted")
	}
	task, err := repo.CreateAIClientTask(ctx, AIClientTaskInput{
		ConnectorID: created.Connector.ID, OwnerUserID: userID, OrgID: orgID,
		ConnectionID: connectionID, Provider: "openai", Action: "generate",
		PayloadKey: "aicp:test-encrypted-payload", ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateAIClientTask(ctx, AIClientTaskInput{
		ConnectorID: created.Connector.ID, OwnerUserID: userID, OrgID: orgID,
		ConnectionID: connectionID, Provider: "openai", Action: "generate",
		PayloadKey: "aicp:second", ExpiresAt: time.Now().Add(time.Minute),
	}); !errors.Is(err, ErrAIClientConnectorBusy) {
		t.Fatalf("single-task connector invariant was bypassed: %v", err)
	}
	claimed, err := repo.ClaimAIClientTask(ctx, created.Connector.ID, 30*time.Second)
	if err != nil || claimed == nil || claimed.ID != task.ID || claimed.LeaseToken == "" {
		t.Fatalf("task claim failed: task=%#v err=%v", claimed, err)
	}
	active, err := repo.AIClientTaskLeaseActive(ctx, created.Connector.ID, task.ID, claimed.LeaseToken)
	if err != nil || !active {
		t.Fatalf("claimed lease is not active: active=%v err=%v", active, err)
	}
	if err := repo.CompleteAIClientTask(ctx, created.Connector.ID, task.ID, "wrong-lease", true, "aicr:result", "", ""); !errors.Is(err, ErrAIClientTaskLeaseInvalid) {
		t.Fatalf("invalid lease completed a task: %v", err)
	}
	if err := repo.CompleteAIClientTask(ctx, created.Connector.ID, task.ID, claimed.LeaseToken, true, "aicr:encrypted-result", "", ""); err != nil {
		t.Fatal(err)
	}
	completed, err := repo.AIClientTaskForOwner(ctx, userID, orgID, task.ID)
	if err != nil || completed.Status != "completed" || completed.ResultKey != "aicr:encrypted-result" {
		t.Fatalf("task result metadata is incomplete: task=%#v err=%v", completed, err)
	}
	var sensitiveColumns int
	if err := db.QueryRow(ctx, `
		select count(*) from information_schema.columns
		where table_schema=current_schema() and table_name='ai_client_tasks'
		  and column_name in ('prompt','content','request_json','response_json','access_token','cookie')
	`).Scan(&sensitiveColumns); err != nil {
		t.Fatal(err)
	}
	if sensitiveColumns != 0 {
		t.Fatalf("ai_client_tasks contains %d sensitive payload columns", sensitiveColumns)
	}

	if err := repo.InitializeAIClientRisk(ctx, connectionID, userID, orgID, created.Connector.ID, "openai", "subject-1", "account"); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	first, err := repo.ReserveAIClientRequest(ctx, userID, orgID, connectionID, base)
	if err != nil || !first.Allowed || first.Risk.RequestsHour != 1 || first.Risk.RequestsDay != 1 {
		t.Fatalf("first risk reservation failed: decision=%#v err=%v", first, err)
	}
	tooFast, err := repo.ReserveAIClientRequest(ctx, userID, orgID, connectionID, base.Add(5*time.Second))
	if err != nil || tooFast.Allowed || tooFast.Reason != "minimum_interval" || tooFast.RetryAt == nil {
		t.Fatalf("minimum interval was not enforced: decision=%#v err=%v", tooFast, err)
	}
	reauth, err := repo.RecordAIClientResult(ctx, userID, orgID, connectionID, false, "401", 0, base.Add(6*time.Second))
	if err != nil || reauth.State != "reauth_required" {
		t.Fatalf("401 state=%#v err=%v", reauth, err)
	}
	confirmed, matched, err := repo.ConfirmAIClientIdentity(ctx, userID, orgID, connectionID, "subject-1", "account", base.Add(7*time.Second))
	if err != nil || !matched || confirmed.State != "paused" {
		t.Fatalf("renewed identity did not require manual resume: risk=%#v matched=%v err=%v", confirmed, matched, err)
	}
	resumed, err := repo.SetAIClientConnectionPaused(ctx, userID, orgID, connectionID, false)
	if err != nil || resumed.State != "normal" {
		t.Fatalf("manual resume failed: risk=%#v err=%v", resumed, err)
	}
	locked, err := repo.RecordAIClientResult(ctx, userID, orgID, connectionID, false, "403", 0, base.Add(8*time.Second))
	if err != nil || locked.State != "security_locked" || locked.CooldownUntil != nil {
		t.Fatalf("403 did not create an indefinite security lock: risk=%#v err=%v", locked, err)
	}
	stillLocked, err := repo.RecordAIClientResult(ctx, userID, orgID, connectionID, true, "", 0, base.Add(9*time.Second))
	if err != nil || stillLocked.State != "security_locked" {
		t.Fatalf("success bypassed a security lock: risk=%#v err=%v", stillLocked, err)
	}
	if _, err := db.Exec(ctx, `update ai_client_account_risk set state='normal',cooldown_until=null where connection_id=$1`, connectionID); err != nil {
		t.Fatal(err)
	}
	first429, err := repo.RecordAIClientResult(ctx, userID, orgID, connectionID, false, "429", 10*time.Minute, base.Add(10*time.Second))
	if err != nil || first429.State != "cooldown" || first429.CooldownUntil == nil || first429.CooldownUntil.Before(base.Add(time.Hour)) {
		t.Fatalf("first 429 cooldown=%#v err=%v", first429, err)
	}
	second429, err := repo.RecordAIClientResult(ctx, userID, orgID, connectionID, false, "429", 0, base.Add(20*time.Second))
	if err != nil || second429.State != "manual_recovery" || second429.CooldownUntil != nil {
		t.Fatalf("second 429 did not require manual recovery: risk=%#v err=%v", second429, err)
	}
	if _, err := db.Exec(ctx, `update ai_client_account_risk set state='normal',cooldown_until=null,rate_limit_events=0 where connection_id=$1`, connectionID); err != nil {
		t.Fatal(err)
	}
	toolLocked, err := repo.RecordAIClientResult(ctx, userID, orgID, connectionID, false, "tool_event", 0, base.Add(30*time.Second))
	if err != nil || toolLocked.State != "policy_locked" {
		t.Fatalf("tool event did not lock the connection: risk=%#v err=%v", toolLocked, err)
	}
	if _, err := db.Exec(ctx, `update ai_client_account_risk set state='normal',cooldown_until=null where connection_id=$1`, connectionID); err != nil {
		t.Fatal(err)
	}
	identityLocked, matched, err := repo.ConfirmAIClientIdentity(ctx, userID, orgID, connectionID, "subject-2", "account", base.Add(40*time.Second))
	if err != nil || matched || identityLocked.State != "security_locked" || identityLocked.IdentityChanges != 1 {
		t.Fatalf("identity change was not locked: risk=%#v matched=%v err=%v", identityLocked, matched, err)
	}

	response, err := repo.CreateAIClientResponse(ctx, AIClientResponseInput{
		OwnerUserID: userID, OrgID: orgID, GatewayKeyID: gatewayKeyID, ConnectionID: connectionID,
		ConnectorID: created.Connector.ID, Provider: "openai", Model: "chatgpt-personal",
		SessionCiphertext: "sealed-session", SessionNonce: "sealed-nonce",
		IdleExpiresAt: base.Add(24 * time.Hour), AbsoluteExpiresAt: base.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AIClientResponseForUse(ctx, response.ID, userID, orgID, gatewayKeyID, connectionID, "chatgpt-personal"); err != nil {
		t.Fatalf("owned response could not be resumed: %v", err)
	}
	for name, values := range map[string][]string{
		"owner":       {"usr_other", orgID, gatewayKeyID, connectionID, "chatgpt-personal"},
		"org":         {userID, "org_other", gatewayKeyID, connectionID, "chatgpt-personal"},
		"gateway key": {userID, orgID, "gwk_other", connectionID, "chatgpt-personal"},
		"connection":  {userID, orgID, gatewayKeyID, "aic_other", "chatgpt-personal"},
		"model":       {userID, orgID, gatewayKeyID, connectionID, "grok-personal"},
	} {
		if _, err := repo.AIClientResponseForUse(ctx, response.ID, values[0], values[1], values[2], values[3], values[4]); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("%s mismatch could resume a response: %v", name, err)
		}
	}
	if _, err := db.Exec(ctx, `update ai_client_responses set idle_expires_at=now()-interval '1 second' where id=$1`, response.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AIClientResponseForUse(ctx, response.ID, userID, orgID, gatewayKeyID, connectionID, "chatgpt-personal"); !errors.Is(err, ErrAIClientSessionExpired) {
		t.Fatalf("expired response was resumable: %v", err)
	}
	deleted, err := repo.DeleteAIClientResponseForOwner(ctx, userID, orgID, response.ID)
	if err != nil || deleted.ID != response.ID {
		t.Fatalf("response deletion failed: response=%#v err=%v", deleted, err)
	}
	var responseStatus, sessionCiphertext, sessionNonce string
	if err := db.QueryRow(ctx, `select status,session_ciphertext,session_nonce from ai_client_responses where id=$1`, response.ID).Scan(&responseStatus, &sessionCiphertext, &sessionNonce); err != nil {
		t.Fatal(err)
	}
	if responseStatus != "deleted" || sessionCiphertext != "" || sessionNonce != "" {
		t.Fatalf("deleted response retained a session reference: status=%q ciphertext=%q nonce=%q", responseStatus, sessionCiphertext, sessionNonce)
	}

	metrics, err := repo.AIClientMetrics(ctx)
	if err != nil || len(metrics.Tasks) == 0 || len(metrics.Risks) == 0 {
		t.Fatalf("bounded official client metrics are unavailable: metrics=%#v err=%v", metrics, err)
	}
	if err := repo.RevokeAIClientConnector(ctx, userID, orgID, created.Connector.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AuthenticateAIClientConnector(ctx, paired.DeviceToken); !errors.Is(err, ErrAIClientConnectorUnauthorized) {
		t.Fatalf("revoked device token remained active: %v", err)
	}
	var connectionStatus string
	if err := db.QueryRow(ctx, `select status from ai_connections where id=$1`, connectionID).Scan(&connectionStatus); err != nil {
		t.Fatal(err)
	}
	if connectionStatus != "disabled" {
		t.Fatalf("revoking the connector left its connection %q", connectionStatus)
	}
}

func TestProviderPolicySafetyMigrationScrubsConsumerCredentials(t *testing.T) {
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
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	userID := "usr_migration_" + suffix
	orgID := "org_migration_" + suffix
	gatewayID := "gw_migration_" + suffix
	gatewayKeyID := "gwk_migration_" + suffix
	if _, err := tx.Exec(ctx, `
		insert into users(id,email,password_hash,name,avatar,status,role,email_verified_at)
		values($1,$2,'integration-test','安全迁移测试','M','active','user',now())
	`, userID, suffix+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into orgs(id,name,slug,plan,status) values($1,'安全迁移测试',$2,'starter','active')`, orgID, "safety-migration-it-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into org_members(org_id,user_id,role,status) values($1,$2,'owner','active')`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		id       string
		provider string
		method   string
	}{
		{"aic_codex_" + suffix, "openai", "codex_oauth"},
		{"aic_deepseek_" + suffix, "deepseek", "deepseek_web_token"},
		{"aic_opencli_" + suffix, "deepseek", "opencli_browser"},
	}
	for _, fixture := range fixtures {
		protocol, adapter := "openai_compatible", "openai-compatible"
		if fixture.provider == "openai" {
			protocol, adapter = "openai", "openai"
		}
		if _, err := tx.Exec(ctx, `
			insert into ai_connections(
				id,owner_user_id,org_id,provider,product_line,region,auth_method,protocol,adapter_type,
				endpoint,provider_config,display_name,status,validation_stage,auth_status,sharing_scope,
				risk_level,provider_adapter_version,terms_ack_version
			) values($1,$2,$3,$4,'Legacy Consumer','global',$5,$6,$7,$8,'{}'::jsonb,$9,
				'active','generation','active','personal','experimental','legacy-v1','legacy-terms')
		`, fixture.id, userID, orgID, fixture.provider, fixture.method, protocol, adapter,
			"https://consumer.example.test/"+fixture.provider, "Legacy "+fixture.method); err != nil {
			t.Fatal(err)
		}
		channelID := "ch_" + fixture.id
		if _, err := tx.Exec(ctx, `
			insert into channels(
				id,owner_type,owner_id,org_id,name,provider,type,model,upstream_model,endpoint,status,
				ai_connection_id,managed_source,public_visible,gateway_enabled
			) values($1,'user',$2,$3,$4,$5,'OpenAI Compatible','legacy-model','legacy-model',$6,'active',$7,'ai_connection',true,true)
		`, channelID, userID, orgID, "Legacy "+fixture.method, fixture.provider,
			"https://consumer.example.test/"+fixture.provider, fixture.id); err != nil {
			t.Fatal(err)
		}
		if fixture.method != "opencli_browser" {
			if _, err := tx.Exec(ctx, `
				insert into ai_connection_secrets(
					connection_id,ciphertext,nonce,mask,fingerprint,encryption_key_id,fingerprint_key_id,
					algorithm,secret_type,payload_format,subject_fingerprint,expires_at,next_refresh_at,
					last_refreshed_at,refresh_failures,last_refresh_error_code
				) values($1,$2,'consumer-nonce','consumer-mask','consumer-fingerprint','enc-test','fp-test',
					'aes-256-gcm','oauth_bundle','oauth_bundle_v1','consumer-subject',now()+interval '1 day',
					now()+interval '1 hour',now(),3,'previous-refresh-error')
			`, fixture.id, "encrypted-access-refresh-id-token-userToken-"+fixture.method); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := tx.Exec(ctx, `
		insert into gateways(id,org_id,name,slug,base_url,policy,status,qps_limit,quota_month,created_by)
		values($1,$2,'迁移测试网关',$3,'https://example.test/gateway/v1','latency','active',1,80,$4)
	`, gatewayID, orgID, "safety-migration-"+suffix, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		insert into gateway_keys(id,org_id,gateway_id,name,key_hash,key_prefix,key_mask,quota_month,qps_limit,status,created_by)
		values($1,$2,$3,'保留的 Gateway Key',$4,'sk-th-it','sk-th-it-••••',80,1,'active',$5)
	`, gatewayKeyID, orgID, gatewayID, "hash-"+suffix, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		insert into gateway_upstreams(id,gateway_id,channel_id,weight,priority,enabled)
		values($1,$2,$3,100,0,true)
	`, "gwu_migration_"+suffix, gatewayID, "ch_"+fixtures[0].id); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0051_provider_policy_safety.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("apply safety migration: %v", err)
	}
	for _, fixture := range fixtures[:2] {
		var status, authStatus, policyVersion string
		if err := tx.QueryRow(ctx, `select status,auth_status,policy_version from ai_connections where id=$1`, fixture.id).Scan(&status, &authStatus, &policyVersion); err != nil {
			t.Fatal(err)
		}
		if status != "disabled" || authStatus != "disabled" || policyVersion != "provider-policy-2026-08-19" {
			t.Fatalf("legacy connection %s was not disabled: status=%q auth=%q policy=%q", fixture.id, status, authStatus, policyVersion)
		}
		var ciphertext, nonce, fingerprint, subject, refreshError string
		var expiresAt, nextRefreshAt, refreshedAt *time.Time
		var refreshFailures int
		if err := tx.QueryRow(ctx, `
			select ciphertext,nonce,fingerprint,subject_fingerprint,expires_at,next_refresh_at,last_refreshed_at,
				refresh_failures,last_refresh_error_code
			from ai_connection_secrets where connection_id=$1
		`, fixture.id).Scan(&ciphertext, &nonce, &fingerprint, &subject, &expiresAt, &nextRefreshAt, &refreshedAt, &refreshFailures, &refreshError); err != nil {
			t.Fatal(err)
		}
		if ciphertext != "" || nonce != "" || fingerprint != "" || subject != "" || expiresAt != nil || nextRefreshAt != nil || refreshedAt != nil || refreshFailures != 0 || refreshError != "provider_policy_migrated" {
			t.Fatalf("legacy consumer credential %s was not fully scrubbed", fixture.id)
		}
	}
	for _, fixture := range fixtures {
		var status string
		var publicVisible, gatewayEnabled bool
		if err := tx.QueryRow(ctx, `select status,public_visible,gateway_enabled from channels where id=$1`, "ch_"+fixture.id).Scan(&status, &publicVisible, &gatewayEnabled); err != nil {
			t.Fatal(err)
		}
		if status != "disabled" || publicVisible || gatewayEnabled {
			t.Fatalf("managed channel for %s remained available", fixture.method)
		}
	}
	var openCLIStatus string
	if err := tx.QueryRow(ctx, `select status from ai_connections where id=$1`, fixtures[2].id).Scan(&openCLIStatus); err != nil {
		t.Fatal(err)
	}
	if openCLIStatus != "active" {
		t.Fatalf("OpenCLI device reference was destroyed: status=%q", openCLIStatus)
	}
	var keyStatus string
	if err := tx.QueryRow(ctx, `select status from gateway_keys where id=$1`, gatewayKeyID).Scan(&keyStatus); err != nil {
		t.Fatal(err)
	}
	if keyStatus != "active" {
		t.Fatalf("Gateway Key was deleted or revoked during containment: status=%q", keyStatus)
	}
}
