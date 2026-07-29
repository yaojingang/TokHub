package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrUnavailable = errors.New("gateway cache unavailable")

type Cache struct {
	client *redis.Client
	logger *slog.Logger
}

func NewCache(ctx context.Context, redisURL string, logger *slog.Logger) *Cache {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Warn("redis url invalid; gateway cache disabled", "error", err)
		return nil
	}
	client := redis.NewClient(options)
	pingCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		logger.Warn("redis unavailable; gateway cache disabled", "error", err)
		return nil
	}
	return &Cache{client: client, logger: logger}
}

func (c *Cache) Close() {
	if c != nil && c.client != nil {
		_ = c.client.Close()
	}
}

func (c *Cache) AllowQPS(ctx context.Context, key string, limit int) (bool, error) {
	if c == nil || c.client == nil {
		return false, ErrUnavailable
	}
	if limit <= 0 {
		return true, nil
	}
	bucket := "gateway:qps:" + key + ":" + time.Now().Format("20060102150405")
	count, err := c.client.Incr(ctx, bucket).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		_ = c.client.Expire(ctx, bucket, 2*time.Second).Err()
	}
	return count <= int64(limit), nil
}

func (c *Cache) AcquireConcurrency(ctx context.Context, key string, limit int, ttl time.Duration) (string, bool, error) {
	if c == nil || c.client == nil {
		return "", false, ErrUnavailable
	}
	if limit <= 0 {
		return "", true, nil
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	var tokenBytes [16]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return "", false, err
	}
	token := hex.EncodeToString(tokenBytes[:])
	now := time.Now()
	expiresAt := now.Add(ttl)
	script := redis.NewScript(`
		redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
		if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[2]) then
			return 0
		end
		redis.call('ZADD', KEYS[1], ARGV[3], ARGV[4])
		redis.call('PEXPIRE', KEYS[1], ARGV[5])
		return 1
	`)
	acquired, err := script.Run(ctx, c.client, []string{"gateway:concurrency:" + key},
		now.UnixMilli(), limit, expiresAt.UnixMilli(), token, ttl.Milliseconds()).Int()
	if err != nil {
		return "", false, err
	}
	return token, acquired == 1, nil
}

func (c *Cache) ReleaseConcurrency(ctx context.Context, key string, token string) error {
	if c == nil || c.client == nil {
		return ErrUnavailable
	}
	if token == "" {
		return nil
	}
	return c.client.ZRem(ctx, "gateway:concurrency:"+key, token).Err()
}

func (c *Cache) OpenCircuit(ctx context.Context, channelID string, ttl time.Duration) error {
	if c == nil || c.client == nil {
		return ErrUnavailable
	}
	return c.client.Set(ctx, "gateway:circuit:"+channelID, "open", ttl).Err()
}

func (c *Cache) CircuitOpen(ctx context.Context, channelID string) (bool, error) {
	if c == nil || c.client == nil {
		return false, ErrUnavailable
	}
	count, err := c.client.Exists(ctx, "gateway:circuit:"+channelID).Result()
	return count > 0, err
}

func (c *Cache) StoreRoutePlan(ctx context.Context, gatewayID string, channelIDs []string) error {
	if c == nil || c.client == nil {
		return ErrUnavailable
	}
	payload, err := json.Marshal(channelIDs)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, "gateway:"+gatewayID+":upstreams", payload, 30*time.Second).Err()
}
