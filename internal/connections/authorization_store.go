package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisAuthorizationStore struct {
	client *redis.Client
	now    func() time.Time
}

var consumeBoundAuthorizationScript = redis.NewScript(`
	local raw = redis.call("get", KEYS[1])
	if not raw then
		return "__tokhub_missing__"
	end
	local ok, value = pcall(cjson.decode, raw)
	if not ok then
		redis.call("del", KEYS[1])
		return "__tokhub_invalid__"
	end
	if value.userId ~= ARGV[1] or value.sessionHash ~= ARGV[2] then
		return "__tokhub_binding__"
	end
	redis.call("del", KEYS[1])
	return raw
`)

func NewRedisAuthorizationStore(ctx context.Context, redisURL string) (*RedisAuthorizationStore, error) {
	options, err := redis.ParseURL(strings.TrimSpace(redisURL))
	if err != nil {
		return nil, fmt.Errorf("parse authorization redis URL: %w", err)
	}
	client := redis.NewClient(options)
	pingCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("authorization redis unavailable: %w", err)
	}
	return &RedisAuthorizationStore{client: client, now: time.Now}, nil
}

func (s *RedisAuthorizationStore) Put(ctx context.Context, transaction AuthorizationTransaction) error {
	if transaction.ID == "" || transaction.UserID == "" || transaction.SessionHash == "" || transaction.ExpiresAt.IsZero() {
		return fmt.Errorf("authorization transaction is incomplete")
	}
	ttl := time.Until(transaction.ExpiresAt)
	if s.now != nil {
		ttl = transaction.ExpiresAt.Sub(s.now())
	}
	if ttl <= 0 {
		return ErrAuthorizationExpired
	}
	raw, err := json.Marshal(transaction)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, authorizationRedisKey(transaction.ID), raw, ttl).Err()
}

func (s *RedisAuthorizationStore) Get(ctx context.Context, id string) (AuthorizationTransaction, error) {
	raw, err := s.client.Get(ctx, authorizationRedisKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return AuthorizationTransaction{}, ErrAuthorizationNotFound
	}
	if err != nil {
		return AuthorizationTransaction{}, err
	}
	return decodeAuthorizationTransaction(raw, s.currentTime())
}

func (s *RedisAuthorizationStore) Consume(ctx context.Context, id string, userID string, sessionHash string) (AuthorizationTransaction, error) {
	raw, err := s.consumeBound(ctx, authorizationRedisKey(id), userID, sessionHash)
	if err != nil {
		return AuthorizationTransaction{}, err
	}
	transaction, err := decodeAuthorizationTransaction(raw, s.currentTime())
	if err != nil {
		return AuthorizationTransaction{}, err
	}
	if !secureEqual(transaction.UserID, userID) || !secureEqual(transaction.SessionHash, sessionHash) {
		return AuthorizationTransaction{}, ErrAuthorizationBinding
	}
	return transaction, nil
}

func (s *RedisAuthorizationStore) Delete(ctx context.Context, id string, userID string, sessionHash string) error {
	_, err := s.Consume(ctx, id, userID, sessionHash)
	return err
}

func (s *RedisAuthorizationStore) PutStepUp(ctx context.Context, grant StepUpGrant) error {
	if grant.Token == "" || grant.UserID == "" || grant.SessionHash == "" || grant.ExpiresAt.IsZero() {
		return fmt.Errorf("step-up grant is incomplete")
	}
	ttl := grant.ExpiresAt.Sub(s.currentTime())
	if ttl <= 0 {
		return ErrAuthorizationExpired
	}
	raw, err := json.Marshal(grant)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, stepUpRedisKey(grant.Token), raw, ttl).Err()
}

func (s *RedisAuthorizationStore) ConsumeStepUp(ctx context.Context, token string, userID string, sessionHash string) error {
	raw, err := s.consumeBound(ctx, stepUpRedisKey(token), userID, sessionHash)
	if err != nil {
		return err
	}
	var grant StepUpGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		return ErrAuthorizationNotFound
	}
	if !grant.ExpiresAt.After(s.currentTime()) {
		return ErrAuthorizationExpired
	}
	return nil
}

func (s *RedisAuthorizationStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *RedisAuthorizationStore) AcquireRefreshLock(ctx context.Context, connectionID string, ttl time.Duration) (string, bool, error) {
	token, err := GenerateOpaqueToken("lock_")
	if err != nil {
		return "", false, err
	}
	acquired, err := s.client.SetNX(ctx, refreshRedisKey(connectionID), token, ttl).Result()
	return token, acquired, err
}

func (s *RedisAuthorizationStore) ReleaseRefreshLock(ctx context.Context, connectionID string, token string) error {
	_, err := s.client.Eval(ctx, `
		if redis.call("get",KEYS[1]) == ARGV[1] then
			return redis.call("del",KEYS[1])
		end
		return 0
	`, []string{refreshRedisKey(connectionID)}, token).Result()
	return err
}

func (s *RedisAuthorizationStore) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *RedisAuthorizationStore) consumeBound(ctx context.Context, key string, userID string, sessionHash string) ([]byte, error) {
	result, err := consumeBoundAuthorizationScript.Run(ctx, s.client, []string{key}, userID, sessionHash).Text()
	if err != nil {
		return nil, err
	}
	switch result {
	case "__tokhub_missing__", "__tokhub_invalid__":
		return nil, ErrAuthorizationNotFound
	case "__tokhub_binding__":
		return nil, ErrAuthorizationBinding
	default:
		return []byte(result), nil
	}
}

func decodeAuthorizationTransaction(raw []byte, now time.Time) (AuthorizationTransaction, error) {
	var transaction AuthorizationTransaction
	if err := json.Unmarshal(raw, &transaction); err != nil {
		return AuthorizationTransaction{}, ErrAuthorizationNotFound
	}
	if !transaction.ExpiresAt.After(now) {
		return AuthorizationTransaction{}, ErrAuthorizationExpired
	}
	return transaction, nil
}

func authorizationRedisKey(id string) string {
	return "ai-auth:transaction:" + opaqueRedisSuffix(id)
}

func stepUpRedisKey(token string) string {
	return "ai-auth:step-up:" + opaqueRedisSuffix(token)
}

func refreshRedisKey(connectionID string) string {
	return "ai-auth:refresh:" + strings.TrimSpace(connectionID)
}

func opaqueRedisSuffix(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
