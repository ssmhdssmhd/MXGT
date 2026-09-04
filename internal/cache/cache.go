// Package cache 缓存抽象：支持对接多个驱动（memory 默认 / redis 可选）。
// 通过接口编程，后续可扩展更多缓存后端。
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/ssmhdssmhd/MXGT/internal/config"
)

// Cache 缓存接口（可对接多个实现：内存 / Redis / 文件...）
type Cache interface {
	// Get 读取缓存，不存在返回 (nil, false)
	Get(ctx context.Context, key string) (string, bool)
	// Set 写入缓存（ttl<=0 时使用默认 TTL）
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	// Delete 删除缓存
	Delete(ctx context.Context, key string) error
	// Close 关闭连接
	Close() error
}

// New 按配置创建缓存实例（多驱动对接）
func New(cfg *config.CacheConfig) (Cache, error) {
	switch cfg.Driver {
	case "redis":
		return newRedis(cfg)
	case "memory", "":
		return newMemory(cfg.TTL), nil
	default:
		return nil, &UnknownDriverError{Driver: cfg.Driver}
	}
}

// UnknownDriverError 未知缓存驱动错误
type UnknownDriverError struct{ Driver string }

func (e *UnknownDriverError) Error() string {
	return "不支持的缓存驱动: " + e.Driver
}

// ---- 内存实现（默认，零依赖） ----

type memItem struct {
	value  string
	expire time.Time
}

type memoryCache struct {
	mu     sync.RWMutex
	items  map[string]memItem
	defTTL time.Duration
}

func newMemory(ttlSeconds int) *memoryCache {
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	return &memoryCache{
		items:  make(map[string]memItem),
		defTTL: time.Duration(ttlSeconds) * time.Second,
	}
}

func (m *memoryCache) Get(_ context.Context, key string) (string, bool) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return "", false
	}
	if !item.expire.IsZero() && time.Now().After(item.expire) {
		m.mu.Lock()
		delete(m.items, key)
		m.mu.Unlock()
		return "", false
	}
	return item.value, true
}

func (m *memoryCache) Set(_ context.Context, key, value string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = m.defTTL
	}
	m.mu.Lock()
	m.items[key] = memItem{value: value, expire: time.Now().Add(ttl)}
	m.mu.Unlock()
	return nil
}

func (m *memoryCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	return nil
}

func (m *memoryCache) Close() error { return nil }
