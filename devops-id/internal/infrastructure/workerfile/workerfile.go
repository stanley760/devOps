// Package workerfile 在本地磁盘持久化 worker 槽位缓存
// （{root}/{leaf_name}/{biz_tag}/worker_id）。etcd 不可用时，发号器回退
// 复用该槽位，避免与仍持有 etcd 槽位的实例冲突属于概率事件之外的
// 常见部署形态（单实例 / 全实例同时失联）。实现 domain 的 WorkerCache 端口。
package workerfile

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

// Cache 持久化单个 biz_tag 的 worker 槽位。
type Cache struct {
	path string
}

// New 按 {root}/{leafName}/{bizTag}/worker_id 构造缓存。
func New(root, leafName, bizTag string) *Cache {
	return &Cache{path: filepath.Join(root, leafName, bizTag, "worker_id")}
}

// Ensure 创建缓存所在的目录树。
func (c *Cache) Ensure() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory %s: %w", dir, err)
	}
	return nil
}

// Load 读取缓存槽位；缺失或损坏返回 ok=false。
func (c *Cache) Load() (int64, bool) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// Save 写入槽位；失败仅告警（缓存写失败不应中断发号）。
func (c *Cache) Save(value int64) {
	if err := os.WriteFile(c.path, []byte(strconv.FormatInt(value, 10)), 0o644); err != nil {
		slog.Warn("write worker cache failed", "path", c.path, "err", err)
	}
}
