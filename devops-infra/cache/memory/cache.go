// Package memory 提供进程内缓存实现，适合单实例、测试和降级场景。
package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	cacheinf "devops-infra/cache"
)

type entry struct {
	value     string
	expiresAt time.Time
}

// Cache 是并发安全的内存缓存。
var _ cacheinf.Cache = (*Cache)(nil)

type Cache struct {
	mu      sync.RWMutex
	entries map[string]entry
	now     func() time.Time
}

// New 创建内存缓存。
func New() *Cache {
	return &Cache{
		entries: make(map[string]entry),
		now:     time.Now,
	}
}

// Get 读取缓存；key 不存在或已过期时返回 cache.ErrNotFound。
func (c *Cache) Get(_ context.Context, key string) (string, error) {
	c.mu.RLock()
	item, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return "", cacheinf.ErrNotFound
	}
	if !item.expiresAt.IsZero() && !c.now().Before(item.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return "", cacheinf.ErrNotFound
	}
	return item.value, nil
}

// Set 写入缓存；expiration <= 0 表示不过期。
func (c *Cache) Set(_ context.Context, key, value string, expiration time.Duration) error {
	var expiresAt time.Time
	if expiration > 0 {
		expiresAt = c.now().Add(expiration)
	}

	c.mu.Lock()
	c.entries[key] = entry{value: value, expiresAt: expiresAt}
	c.mu.Unlock()
	return nil
}

// Delete 删除一个或多个 key。
func (c *Cache) Delete(_ context.Context, keys ...string) error {
	c.mu.Lock()
	for _, key := range keys {
		delete(c.entries, key)
	}
	c.mu.Unlock()
	return nil
}

// Exists 判断 key 是否存在且未过期。
func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.Get(ctx, key)
	if errors.Is(err, cacheinf.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
