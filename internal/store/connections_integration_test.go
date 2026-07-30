package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestAIConnectionSecretRotationOnlyAcceptsOfficialAPIKeyMethods(t *testing.T) {
	for _, method := range []string{"api_key", "api_key_guided"} {
		if !supportsAIConnectionSecretRotation(method) {
			t.Fatalf("%s connection could not rotate its official API key", method)
		}
	}
	for _, method := range []string{"", "oauth", "codex_oauth", "deepseek_web_token", "opencli_browser"} {
		if supportsAIConnectionSecretRotation(method) {
			t.Fatalf("%s managed connection accepted raw API key rotation", method)
		}
	}
}

func TestAIConnectionCanCreateIdempotentManagedRelay(t *testing.T) {
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
	userID := "usr_it_" + suffix
	orgID := "org_it_" + suffix
	if _, err := db.Exec(ctx, `
		insert into users(id,email,password_hash,name,avatar,status,role,email_verified_at)
		values($1,$2,'integration-test','连接测试','T','active','user',now())
	`, userID, suffix+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		insert into orgs(id,name,slug,plan,status)
		values($1,'连接测试工作区',$2,'starter','active')
	`, orgID, "connection-it-"+suffix); err != nil {
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

	connectionInput := AIConnectionCreateInput{
		OwnerUserID: userID, OrgID: orgID, Provider: "openai", ProductLine: "OpenAI Platform",
		Region: "global", Protocol: "openai", AdapterType: "openai", Endpoint: "https://api.openai.com/v1",
		ProviderConfig: map[string]any{"connectionProvider": "openai"}, DisplayName: "个人 OpenAI",
		Models: []string{"gpt-integration"},
		Credential: AIConnectionSecret{
			Ciphertext: "encrypted-value", Nonce: "nonce-value", Mask: "sk-****test",
			Fingerprint: "fingerprint", EncryptionKeyID: "enc-v1", FingerprintKeyID: "fp-v1",
			Algorithm: "aes-256-gcm",
		},
		Validation: AIConnectionValidation{OK: false, Stage: "generation", ModelCount: 0, ErrorCode: "invalid_key"},
	}
	connection, err := repo.CreateAIConnection(ctx, connectionInput)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(connection)
	if strings.Contains(string(body), "encrypted-value") || len(connection.Models) != 1 {
		t.Fatalf("connection response leaked ciphertext or lost models: %s", body)
	}
	if connection.Status != "attention" || connection.Models[0].VerificationStatus != "unverified" {
		t.Fatalf("failed validation was exposed as verified: %#v", connection)
	}
	duplicateInput := connectionInput
	duplicateInput.DisplayName = "重复 OpenAI"
	if _, err := repo.CreateAIConnection(ctx, duplicateInput); !errors.Is(err, ErrAIConnectionDuplicate) {
		t.Fatalf("duplicate credential was accepted: %v", err)
	}
	if _, err := repo.AIConnectionForOwnerOrg(ctx, "usr_other", orgID, connection.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another user accessed the connection: %v", err)
	}
	if _, err := repo.AIConnectionSecretForOwnerOrg(ctx, userID, "org_other", connection.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another workspace accessed the credential: %v", err)
	}
	connection, err = repo.UpdateAIConnectionValidation(ctx, userID, orgID, connection.ID, 1, AIConnectionValidation{
		OK: true, Stage: "generation", ModelCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if connection.Status != "active" || connection.Models[0].VerificationStatus != "verified" {
		t.Fatalf("successful revalidation did not activate models: %#v", connection)
	}
	connection, err = repo.RotateAIConnectionSecret(ctx, userID, orgID, connection.ID, AIConnectionSecret{
		Ciphertext: "encrypted-value-v2", Nonce: "nonce-value-v2", Mask: "sk-****st-v2",
		Fingerprint: "fingerprint-v2", EncryptionKeyID: "enc-v2", FingerprintKeyID: "fp-v2",
		Algorithm: "aes-256-gcm",
	}, AIConnectionValidation{OK: true, Stage: "generation", ModelCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateAIConnectionValidation(ctx, userID, orgID, connection.ID, 1, AIConnectionValidation{
		OK: false, Stage: "generation", ErrorCode: "stale_validation",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale validation overwrote a rotated credential: %v", err)
	}

	input := QuickRelayInput{
		OwnerUserID: userID, OrgID: orgID, ConnectionID: connection.ID,
		ModelIDs: []string{connection.Models[0].ID}, Name: "个人中转", Policy: "latency",
		BaseURL: "https://tokhub.example.test/gateway/v1", IdempotencyKey: "integration-key-0001",
		RequestHash: "request-hash-1", PlainKey: "sk-th-integration-" + suffix,
		Reveal: QuickRelayReveal{
			Ciphertext: "relay-ciphertext", Nonce: "relay-nonce", EncryptionKeyID: "enc-v1",
			Fingerprint: "relay-fingerprint", FingerprintKeyID: "fp-v1", Mask: "sk-th-••••cret",
		},
	}
	created, err := repo.CreateQuickRelay(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Replay || created.Gateway.ID == "" || created.Key.PlainKey != input.PlainKey {
		t.Fatalf("unexpected quick relay result: %#v", created)
	}
	credential, err := repo.GatewayChannelCredential(ctx, orgID, created.Gateway.Upstreams[0].ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if credential.ConnectionID != connection.ID || credential.Ciphertext != "encrypted-value-v2" {
		t.Fatalf("managed channel did not resolve the central connection secret: %#v", credential)
	}
	if _, err := db.Exec(ctx, `update users set status='disabled' where id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GatewayChannelCredential(ctx, orgID, created.Gateway.Upstreams[0].ChannelID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("disabled connection owner retained runtime credential access: %v", err)
	}
	if _, err := db.Exec(ctx, `update users set status='active' where id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `update org_members set status='removed' where org_id=$1 and user_id=$2`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GatewayChannelCredential(ctx, orgID, created.Gateway.Upstreams[0].ChannelID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("removed connection owner retained runtime credential access: %v", err)
	}
	if _, err := db.Exec(ctx, `update org_members set status='active' where org_id=$1 and user_id=$2`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GatewayChannelCredential(ctx, "org_other", created.Gateway.Upstreams[0].ChannelID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another workspace resolved the managed credential: %v", err)
	}
	if _, err := repo.PrivateChannelForOrg(ctx, orgID, created.Gateway.Upstreams[0].ChannelID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("managed channel was exposed through private channel APIs: %v", err)
	}
	if err := repo.DeletePrivateChannelForOrg(ctx, orgID, userID, created.Gateway.Upstreams[0].ChannelID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("managed channel was mutable through private channel APIs: %v", err)
	}
	available, err := repo.AvailableGatewayUpstreamsForOrg(ctx, orgID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, upstream := range available {
		if upstream.ChannelID == created.Gateway.Upstreams[0].ChannelID {
			t.Fatalf("managed channel was exposed to general gateway composition: %#v", upstream)
		}
	}
	replayed, err := repo.CreateQuickRelay(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replay || replayed.Gateway.ID != created.Gateway.ID || replayed.Key.ID != created.Key.ID {
		t.Fatalf("idempotent retry returned a different relay: %#v", replayed)
	}
	if len(replayed.Gateway.Upstreams) != len(created.Gateway.Upstreams) || replayed.Gateway.Stats.KeysActive != created.Gateway.Stats.KeysActive {
		t.Fatalf("idempotent retry returned an incomplete gateway: created=%#v replayed=%#v", created.Gateway, replayed.Gateway)
	}

	concurrentInput := input
	concurrentInput.IdempotencyKey = "integration-concurrent-0001"
	concurrentInput.RequestHash = "request-hash-concurrent"
	concurrentInput.PlainKey += "-concurrent"
	start := make(chan struct{})
	results := make(chan QuickRelayResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, createErr := repo.CreateQuickRelay(ctx, concurrentInput)
			results <- result
			errs <- createErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	var concurrentGatewayID string
	for createErr := range errs {
		if createErr != nil {
			t.Fatalf("concurrent idempotent request failed: %v", createErr)
		}
	}
	for result := range results {
		if concurrentGatewayID == "" {
			concurrentGatewayID = result.Gateway.ID
		}
		if result.Gateway.ID != concurrentGatewayID {
			t.Fatalf("concurrent idempotent request created multiple gateways: first=%s next=%s", concurrentGatewayID, result.Gateway.ID)
		}
	}

	if _, err := db.Exec(ctx, `
		update ai_quick_relay_requests set expires_at=now()-interval '1 minute'
		where owner_user_id=$1 and org_id=$2
	`, userID, orgID); err != nil {
		t.Fatal(err)
	}
	expired, err := repo.ExpireAIQuickRelayReveals(ctx)
	if err != nil || expired < 2 {
		t.Fatalf("expired relay reveals = %d, err=%v", expired, err)
	}
	var revealCiphertext, revealNonce, revealStatus string
	if err := db.QueryRow(ctx, `
		select reveal_ciphertext,reveal_nonce,status
		from ai_quick_relay_requests
		where owner_user_id=$1 and org_id=$2
		limit 1
	`, userID, orgID).Scan(&revealCiphertext, &revealNonce, &revealStatus); err != nil {
		t.Fatal(err)
	}
	if revealCiphertext != "" || revealNonce != "" || revealStatus != "expired" {
		t.Fatalf("expired reveal was retained: ciphertext=%q nonce=%q status=%q", revealCiphertext, revealNonce, revealStatus)
	}

	deleteReplayInput := input
	deleteReplayInput.IdempotencyKey = "integration-delete-replay-0001"
	deleteReplayInput.RequestHash = "request-hash-delete-replay"
	deleteReplayInput.PlainKey += "-delete-replay"
	deleteReplayInput.Reveal.Ciphertext = "delete-replay-ciphertext"
	deleteReplayInput.Reveal.Nonce = "delete-replay-nonce"
	if _, err := repo.CreateQuickRelay(ctx, deleteReplayInput); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAIConnection(ctx, userID, orgID, connection.ID); err != nil {
		t.Fatal(err)
	}
	var gatewayStatus, gatewayKeyStatus, scrubbedCiphertext string
	if err := db.QueryRow(ctx, `select status from gateways where id=$1`, created.Gateway.ID).Scan(&gatewayStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `select status from gateway_keys where id=$1`, created.Key.ID).Scan(&gatewayKeyStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `select ciphertext from ai_connection_secrets where connection_id=$1`, connection.ID).Scan(&scrubbedCiphertext); err != nil {
		t.Fatal(err)
	}
	if gatewayStatus != "paused" || gatewayKeyStatus != "revoked" || scrubbedCiphertext != "" {
		t.Fatalf("connection deletion left relay access active: gateway=%s key=%s ciphertext=%q", gatewayStatus, gatewayKeyStatus, scrubbedCiphertext)
	}
	if _, err := repo.CreateQuickRelay(ctx, deleteReplayInput); err == nil || !strings.Contains(err.Error(), "reveal window expired") {
		t.Fatalf("deleted connection replay exposed a stale one-time reveal: %v", err)
	}

	userDeleteInput := connectionInput
	userDeleteInput.DisplayName = "待注销连接"
	userDeleteInput.Models = []string{"gpt-delete-ok", "gpt-delete-denied"}
	userDeleteInput.Credential.Fingerprint = "fingerprint-user-delete"
	userDeleteInput.Validation = AIConnectionValidation{
		OK: false, Stage: "generation", ModelCount: 2, ErrorCode: "upstream_auth_error",
		ErrorMessage: "1/2 models verified",
		Models: []AIConnectionModelValidation{
			{ProviderModelID: "gpt-delete-ok", OK: true, LatencyMs: 12},
			{ProviderModelID: "gpt-delete-denied", OK: false, LatencyMs: 15, ErrorCode: "upstream_auth_error", ErrorMessage: "denied"},
		},
	}
	userDeleteConnection, err := repo.CreateAIConnection(ctx, userDeleteInput)
	if err != nil {
		t.Fatal(err)
	}
	if userDeleteConnection.Status != "attention" || userDeleteConnection.Models[0].VerificationStatus != "verified" || userDeleteConnection.Models[1].VerificationStatus != "unverified" {
		t.Fatalf("partial model validation was not preserved: %#v", userDeleteConnection)
	}
	userDeleteRelay, err := repo.CreateQuickRelay(ctx, QuickRelayInput{
		OwnerUserID: userID, OrgID: orgID, ConnectionID: userDeleteConnection.ID,
		ModelIDs: []string{userDeleteConnection.Models[0].ID}, Name: "部分可用中转", Policy: "latency",
		BaseURL: "https://tokhub.example.test/gateway/v1", IdempotencyKey: "integration-partial-0001",
		RequestHash: "request-hash-partial", PlainKey: "sk-th-partial-" + suffix,
		Reveal: QuickRelayReveal{
			Ciphertext: "partial-relay-ciphertext", Nonce: "partial-relay-nonce", EncryptionKeyID: "enc-v1",
			Fingerprint: "partial-relay-fingerprint", FingerprintKeyID: "fp-v1", Mask: "sk-th-••••tial",
		},
	})
	if err != nil {
		t.Fatalf("verified model on an attention connection could not create a relay: %v", err)
	}
	experimentalInput := connectionInput
	experimentalInput.DisplayName = "ChatGPT Codex 实验连接"
	experimentalInput.Models = []string{"gpt-codex-integration"}
	experimentalInput.AuthMethod = "codex_oauth"
	experimentalInput.AuthStatus = "active"
	experimentalInput.RiskLevel = "experimental"
	experimentalInput.Credential.Fingerprint = "fingerprint-codex-" + suffix
	experimentalInput.Credential.SubjectFingerprint = "subject-codex-" + suffix
	experimentalInput.Credential.SecretType = "oauth_bundle"
	experimentalInput.Credential.PayloadFormat = "oauth_bundle_v1"
	experimentalInput.Validation = AIConnectionValidation{OK: true, Stage: "generation", ModelCount: 1}
	authorizationID := "authz_" + uuid.NewString()
	if _, err := repo.CreateAIAuthorizationAttempt(ctx, AIAuthorizationAttemptInput{
		ID: authorizationID, OwnerUserID: userID, OrgID: orgID, Provider: "openai",
		AuthMethod: "codex_oauth", CompletionMode: "paste_callback", ExpiresAt: time.Now().Add(10 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAIAuthorizationValidating(ctx, userID, authorizationID); err != nil {
		t.Fatal(err)
	}
	experimentalInput.AuthorizationID = authorizationID
	experimentalConnection, err := repo.CreateAIConnection(ctx, experimentalInput)
	if err != nil {
		t.Fatal(err)
	}
	completedAuthorization, err := repo.AIAuthorizationAttemptForOwner(ctx, userID, authorizationID)
	if err != nil || completedAuthorization.Status != "completed" || completedAuthorization.ConnectionID != experimentalConnection.ID {
		t.Fatalf("authorization and connection were not committed together: attempt=%#v err=%v", completedAuthorization, err)
	}
	staleSecret, err := repo.AIConnectionSecretForOwnerOrg(ctx, userID, orgID, experimentalConnection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkOAuthRefreshFailure(
		ctx,
		experimentalConnection.ID,
		staleSecret.Version,
		true,
		"invalid_grant",
		time.Time{},
	); err != nil {
		t.Fatalf("current refresh failure could not require reauthorization: %v", err)
	}
	experimentalConnection, err = repo.AIConnectionForOwnerOrg(ctx, userID, orgID, experimentalConnection.ID)
	if err != nil || experimentalConnection.AuthStatus != "reauth_required" {
		t.Fatalf("current invalid grant was not quarantined: connection=%#v err=%v", experimentalConnection, err)
	}
	if _, err := db.Exec(ctx, `
		update ai_connection_secrets
		set version=version+1,updated_at=now()
		where connection_id=$1
	`, experimentalConnection.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
		update ai_connections
		set auth_status='active',last_error_code='',last_error_message='',updated_at=now()
		where id=$1
	`, experimentalConnection.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkOAuthRefreshFailure(
		ctx,
		experimentalConnection.ID,
		staleSecret.Version,
		true,
		"invalid_grant",
		time.Time{},
	); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale refresh failure overwrote a newer authorization: %v", err)
	}
	experimentalConnection, err = repo.AIConnectionForOwnerOrg(ctx, userID, orgID, experimentalConnection.ID)
	if err != nil || experimentalConnection.AuthStatus != "active" {
		t.Fatalf("newer authorization was quarantined by stale refresh failure: connection=%#v err=%v", experimentalConnection, err)
	}
	experimentalRelayInput := QuickRelayInput{
		OwnerUserID: userID, OrgID: orgID, ConnectionID: experimentalConnection.ID,
		ModelIDs: []string{experimentalConnection.Models[0].ID}, Name: "Codex 实验中转", Policy: "latency",
		QPSLimit: 99, QuotaMonth: 1000, BaseURL: "https://tokhub.example.test/gateway/v1",
		IdempotencyKey: "integration-codex-0001", RequestHash: "request-hash-codex-1",
		PlainKey: "sk-th-codex-" + suffix,
		Reveal: QuickRelayReveal{
			Ciphertext: "codex-relay-ciphertext", Nonce: "codex-relay-nonce", EncryptionKeyID: "enc-v1",
			Fingerprint: "codex-relay-fingerprint", FingerprintKeyID: "fp-v1", Mask: "sk-th-••••odex",
		},
	}
	experimentalRelay, err := repo.CreateQuickRelay(ctx, experimentalRelayInput)
	if err != nil {
		t.Fatalf("experimental relay creation failed: %v", err)
	}
	if experimentalRelay.Gateway.QPSLimit != 1 || experimentalRelay.Key.QPSLimit != 1 {
		t.Fatalf("experimental relay QPS was not clamped: gateway=%d key=%d", experimentalRelay.Gateway.QPSLimit, experimentalRelay.Key.QPSLimit)
	}
	secondExperimentalRelay := experimentalRelayInput
	secondExperimentalRelay.IdempotencyKey = "integration-codex-0002"
	secondExperimentalRelay.RequestHash = "request-hash-codex-2"
	secondExperimentalRelay.PlainKey += "-second"
	if _, err := repo.CreateQuickRelay(ctx, secondExperimentalRelay); !errors.Is(err, ErrExperimentalRelayExists) {
		t.Fatalf("second experimental relay error = %v, want ErrExperimentalRelayExists", err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := deactivateDeletedUsersRuntimeResources(ctx, tx, []string{userID}, "usr_admin_test"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var deletedConnectionStatus, deletedConnectionCiphertext string
	if err := db.QueryRow(ctx, `
		select c.status,s.ciphertext
		from ai_connections c
		join ai_connection_secrets s on s.connection_id=c.id
		where c.id=$1
	`, userDeleteConnection.ID).Scan(&deletedConnectionStatus, &deletedConnectionCiphertext); err != nil {
		t.Fatal(err)
	}
	if deletedConnectionStatus != "deleted" || deletedConnectionCiphertext != "" {
		t.Fatalf("user deletion retained AI connection credential: status=%q ciphertext=%q", deletedConnectionStatus, deletedConnectionCiphertext)
	}
	var deletedGatewayStatus, deletedGatewayKeyStatus string
	if err := db.QueryRow(ctx, `select status from gateways where id=$1`, userDeleteRelay.Gateway.ID).Scan(&deletedGatewayStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `select status from gateway_keys where id=$1`, userDeleteRelay.Key.ID).Scan(&deletedGatewayKeyStatus); err != nil {
		t.Fatal(err)
	}
	if deletedGatewayStatus != "paused" || deletedGatewayKeyStatus != "revoked" {
		t.Fatalf("user deletion retained shared-workspace relay access: gateway=%q key=%q", deletedGatewayStatus, deletedGatewayKeyStatus)
	}
}
