package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ssmhdssmhd/MXGT/internal/config"
)

// redisCache Redis 缓存实现（可选，对接 Redis 时启用）
type redisCache struct {
	client *redis.Client
	defTTL time.Duration
}

func newRedis(cfg *config.CacheConfig) (Cache, error) {
	ttl := time.Duration(cfg.TTL) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Address,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	// 连接检测
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &redisCache{client: client, defTTL: ttl}, nil
}

func (r *redisCache) Get(ctx context.Context, key string) (string, bool) {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

func (r *redisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = r.defTTL
	}
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *redisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *redisCache) Close() error {
	return r.client.Close()
}
