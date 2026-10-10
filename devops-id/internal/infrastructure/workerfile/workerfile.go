// Package workerfile 在本地磁盘持久化 worker 槽位与时间水位
// （{root}/{leaf_name}/{biz_tag}/worker_id 与 watermark）。
// worker_id 为纯十进制整数文本（与历史缓存兼容）；watermark 为 Unix 毫秒。
// 两者均以原子写（temp + rename）持久化。实现 domain 的 WorkerCache 端口。
package workerfile

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

// Cache 持久化单个 biz_tag 的 worker 槽位与时间水位。
type Cache struct {
	dir string
}

// New 按 {root}/{leafName}/{bizTag} 构造缓存目录。
func New(root, leafName, bizTag string) *Cache {
	return &Cache{dir: filepath.Join(root, leafName, bizTag)}
}

func (c *Cache) workerPath() string   { return filepath.Join(c.dir, "worker_id") }
func (c *Cache) watermarkPath() string { return filepath.Join(c.dir, "watermark") }

// Ensure 创建缓存所在的目录树。
func (c *Cache) Ensure() error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory %s: %w", c.dir, err)
	}
	return nil
}

// Load 读取缓存槽位；缺失或损坏返回 ok=false。
func (c *Cache) Load() (int64, bool) {
	return readInt64(c.workerPath())
}

// Save 原子写入槽位；失败返回错误（降级路径由调用方决定是否 fatal）。
func (c *Cache) Save(value int64) error {
	if err := writeAtomic(c.workerPath(), value); err != nil {
		slog.Warn("write worker cache failed", "path", c.workerPath(), "err", err)
		return err
	}
	return nil
}

// LoadWatermark 读取时间水位（Unix 毫秒）；缺失或损坏返回 ok=false。
func (c *Cache) LoadWatermark() (int64, bool) {
	return readInt64(c.watermarkPath())
}

// SaveWatermark 原子写入时间水位（Unix 毫秒）。
func (c *Cache) SaveWatermark(ts int64) error {
	return writeAtomic(c.watermarkPath(), ts)
}

// readInt64 读取十进制整数文件；缺失或损坏返回 ok=false。
func readInt64(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// writeAtomic 以 temp + rename 原子写入十进制整数。
func writeAtomic(path string, value int64) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Clean up temp file on error (rename would have removed it on success).
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.WriteString(strconv.FormatInt(value, 10)); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod temp file %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
