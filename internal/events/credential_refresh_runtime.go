package events

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"tokhub/internal/connections"
	secretcrypto "tokhub/internal/crypto"
	"tokhub/internal/store"
)

type CredentialRefreshConfig struct {
	RedisURL            string
	Workers             int
	ProviderConcurrency int
	ProviderQPS         int
	AttemptTimeout      time.Duration
	RefreshSkew         time.Duration
	Interval            time.Duration
}

type CredentialRefreshRuntime struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	redis  *redis.Client
}

type credentialRefreshRunner struct {
	repo     *store.Repository
	keyring  *secretcrypto.CredentialKeyring
	registry *connections.AuthRegistry
	redis    *redis.Client
	logger   *slog.Logger
	cfg      CredentialRefreshConfig
	group    singleflight.Group
	gateMu   sync.Mutex
	gates    map[string]*credentialRefreshProviderGate
}

type credentialRefreshProviderGate struct {
	concurrency chan struct{}
	rateMu      sync.Mutex
	next        time.Time
	interval    time.Duration
}

func StartCredentialRefreshRuntime(
	parent context.Context,
	repo *store.Repository,
	keyring *secretcrypto.CredentialKeyring,
	registry *connections.AuthRegistry,
	cfg CredentialRefreshConfig,
	logger *slog.Logger,
) (*CredentialRefreshRuntime, error) {
	options, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(options)
	pingCtx, cancelPing := context.WithTimeout(parent, 800*time.Millisecond)
	defer cancelPing()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	if cfg.Workers <= 0 || cfg.Workers > 64 {
		cfg.Workers = 8
	}
	if cfg.ProviderConcurrency <= 0 || cfg.ProviderConcurrency > 32 {
		cfg.ProviderConcurrency = 4
	}
	if cfg.ProviderQPS <= 0 || cfg.ProviderQPS > 100 {
		cfg.ProviderQPS = 2
	}
	if cfg.AttemptTimeout < 5*time.Second || cfg.AttemptTimeout > 2*time.Minute {
		cfg.AttemptTimeout = 20 * time.Second
	}
	if cfg.RefreshSkew <= 0 {
		cfg.RefreshSkew = 5 * time.Minute
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Minute
	}
	ctx, cancel := context.WithCancel(parent)
	runtime := &CredentialRefreshRuntime{cancel: cancel, redis: client}
	runner := &credentialRefreshRunner{
		repo: repo, keyring: keyring, registry: registry, redis: client, logger: logger, cfg: cfg,
		gates: map[string]*credentialRefreshProviderGate{},
	}
	runtime.wg.Add(1)
	go func() {
		defer runtime.wg.Done()
		runner.run(ctx)
	}()
	return runtime, nil
}

func (r *CredentialRefreshRuntime) Close() {
	if r == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
	if r.redis != nil {
		_ = r.redis.Close()
	}
}

func (r *credentialRefreshRunner) run(ctx context.Context) {
	r.sweep(ctx)
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

func (r *credentialRefreshRunner) sweep(ctx context.Context) {
	candidates, err := r.repo.OAuthRefreshCandidates(ctx, r.cfg.Workers*4)
	if err != nil {
		r.logger.Warn("query OAuth refresh candidates", "error", err)
		return
	}
	sem := make(chan struct{}, r.cfg.Workers)
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			_, _, _ = r.group.Do(candidate.ConnectionID, func() (any, error) {
				gate := r.providerGate(candidate.Provider)
				release, err := gate.acquireConcurrency(ctx)
				if err != nil {
					return nil, err
				}
				defer release()
				if err := gate.waitForRate(ctx); err != nil {
					return nil, err
				}
				attemptCtx, cancel := context.WithTimeout(ctx, r.cfg.AttemptTimeout)
				defer cancel()
				return nil, r.refreshOne(attemptCtx, candidate)
			})
		}()
	}
	wg.Wait()
}

func (r *credentialRefreshRunner) refreshOne(ctx context.Context, candidate store.OAuthRefreshCandidate) error {
	lockKey := "ai-auth:refresh:" + candidate.ConnectionID
	lockValue := uuid.NewString()
	lockTTL := r.cfg.AttemptTimeout + 5*time.Second
	acquired, err := r.redis.SetNX(ctx, lockKey, lockValue, lockTTL).Result()
	if err != nil || !acquired {
		return err
	}
	defer func() {
		_, _ = r.redis.Eval(context.Background(), `
			if redis.call("get",KEYS[1]) == ARGV[1] then
				return redis.call("del",KEYS[1])
			end
			return 0
		`, []string{lockKey}, lockValue).Result()
	}()

	plain, err := r.keyring.Decrypt(candidate.OwnerUserID, candidate.Provider, secretcrypto.CredentialEnvelope{
		Ciphertext: candidate.Secret.Ciphertext, Nonce: candidate.Secret.Nonce,
		EncryptionKeyID: candidate.Secret.EncryptionKeyID,
		Fingerprint:     candidate.Secret.Fingerprint, FingerprintKeyID: candidate.Secret.FingerprintKeyID,
		Mask: candidate.Secret.Mask, Algorithm: candidate.Secret.Algorithm,
	})
	if err != nil {
		return r.recordFailure(ctx, candidate, err)
	}
	bundle, err := connections.ParseCredentialBundle(plain)
	if err != nil {
		return r.recordFailure(ctx, candidate, err)
	}
	adapter, ok := r.registry.Adapter(candidate.Provider, candidate.AuthMethod)
	if !ok {
		return r.recordFailure(ctx, candidate, connections.ErrAdapterDisabled)
	}
	refreshed, err := adapter.Refresh(ctx, bundle)
	if err != nil {
		return r.recordFailure(ctx, candidate, err)
	}
	raw, err := refreshed.Marshal()
	if err != nil {
		return r.recordFailure(ctx, candidate, err)
	}
	fingerprintSource := candidate.AuthMethod + "\x00" + refreshed.ProviderSubject + "\x00" + refreshed.AccountID
	encrypted, err := r.keyring.EncryptWithFingerprint(candidate.OwnerUserID, candidate.Provider, raw, fingerprintSource)
	if err != nil {
		return r.recordFailure(ctx, candidate, err)
	}
	encrypted.Mask = candidate.Secret.Mask
	nextRefresh := refreshed.ExpiresAt.Add(-r.cfg.RefreshSkew - credentialRefreshJitter(candidate.ConnectionID, candidate.Secret.Version))
	if nextRefresh.Before(time.Now().Add(time.Minute)) {
		nextRefresh = time.Now().Add(time.Minute)
	}
	secret := store.AIConnectionSecret{
		Ciphertext: encrypted.Ciphertext, Nonce: encrypted.Nonce, Mask: encrypted.Mask,
		Fingerprint: encrypted.Fingerprint, EncryptionKeyID: encrypted.EncryptionKeyID,
		FingerprintKeyID: encrypted.FingerprintKeyID, Algorithm: encrypted.Algorithm,
		SubjectFingerprint: encrypted.Fingerprint, ExpiresAt: &refreshed.ExpiresAt,
		NextRefreshAt: &nextRefresh,
	}
	if err := r.repo.UpdateOAuthConnectionSecret(ctx, candidate.ConnectionID, candidate.Secret.Version, secret); err != nil {
		if store.IsOptimisticCredentialConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

func (r *credentialRefreshRunner) recordFailure(ctx context.Context, candidate store.OAuthRefreshCandidate, refreshErr error) error {
	code, reauth := classifyCredentialRefreshError(refreshErr)
	next := time.Now().Add(credentialRefreshBackoff(candidate.Secret.RefreshFailures + 1))
	mutationCtx, cancel, err := credentialRefreshMutationContext(ctx)
	if err != nil {
		return refreshErr
	}
	defer cancel()
	if err := r.repo.MarkOAuthRefreshFailure(mutationCtx, candidate.ConnectionID, candidate.Secret.Version, reauth, code, next); err != nil {
		if store.IsOptimisticCredentialConflict(err) {
			return refreshErr
		}
		return err
	}
	r.logger.Warn("OAuth credential refresh failed", "provider", candidate.Provider, "reason", code)
	return refreshErr
}

func credentialRefreshMutationContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	switch ctx.Err() {
	case nil:
		return ctx, func() {}, nil
	case context.DeadlineExceeded:
		mutationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		return mutationCtx, cancel, nil
	default:
		return nil, func() {}, ctx.Err()
	}
}

func credentialRefreshBackoff(failures int) time.Duration {
	switch failures {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 15 * time.Minute
	default:
		return time.Hour
	}
}

func classifyCredentialRefreshError(err error) (string, bool) {
	switch {
	case errors.Is(err, connections.ErrCredentialReauth):
		return "invalid_grant", true
	case errors.Is(err, connections.ErrAdapterDisabled):
		return "adapter_disabled", false
	default:
		return "refresh_temporary", false
	}
}

func credentialRefreshJitter(connectionID string, version int) time.Duration {
	sum := sha256.Sum256([]byte(connectionID + "\x00" + strconv.Itoa(version)))
	seconds := int(sum[0]) % 61
	return time.Duration(seconds) * time.Second
}

func (r *credentialRefreshRunner) providerGate(provider string) *credentialRefreshProviderGate {
	provider = strings.ToLower(strings.TrimSpace(provider))
	r.gateMu.Lock()
	defer r.gateMu.Unlock()
	if gate := r.gates[provider]; gate != nil {
		return gate
	}
	gate := newCredentialRefreshProviderGate(r.cfg.ProviderConcurrency, r.cfg.ProviderQPS)
	r.gates[provider] = gate
	return gate
}

func newCredentialRefreshProviderGate(concurrency int, qps int) *credentialRefreshProviderGate {
	if concurrency <= 0 {
		concurrency = 1
	}
	if qps <= 0 {
		qps = 1
	}
	return &credentialRefreshProviderGate{
		concurrency: make(chan struct{}, concurrency),
		interval:    time.Second / time.Duration(qps),
	}
}

func (g *credentialRefreshProviderGate) acquireConcurrency(ctx context.Context) (func(), error) {
	select {
	case g.concurrency <- struct{}{}:
		return func() { <-g.concurrency }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (g *credentialRefreshProviderGate) reserveRateSlot(now time.Time) time.Time {
	g.rateMu.Lock()
	defer g.rateMu.Unlock()
	if g.next.Before(now) {
		g.next = now
	}
	slot := g.next
	g.next = g.next.Add(g.interval)
	return slot
}

func (g *credentialRefreshProviderGate) waitForRate(ctx context.Context) error {
	wait := time.Until(g.reserveRateSlot(time.Now()))
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
