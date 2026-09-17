// Package idgorm provides the GORM integration for devops-id consumers.
package idgorm

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

// ErrGeneratorNotInitialized is returned when a model is created before an ID generator is injected.
var ErrGeneratorNotInitialized = errors.New("ID 生成器未初始化（SetIDGenerator 未装配）")

// Generator allocates one ID for a registered biz_tag.
type Generator interface {
	Next(ctx context.Context, bizTag string) (int64, error)
}

// Hook maps GORM table names to biz_tag values and assigns generated primary keys.
type Hook struct {
	mu          sync.RWMutex
	generator   Generator
	tableBizTag map[string]string
}

// New creates a GORM ID hook. The table-to-biz_tag map is copied.
func New(generator Generator, tableBizTag map[string]string) *Hook {
	tags := make(map[string]string, len(tableBizTag))
	for table, bizTag := range tableBizTag {
		tags[table] = bizTag
	}
	return &Hook{generator: generator, tableBizTag: tags}
}

// SetGenerator injects or replaces the ID generator at assembly time.
func (h *Hook) SetGenerator(generator Generator) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.generator = generator
}

// Generator returns the currently injected generator.
func (h *Hook) Generator() Generator {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.generator
}

// Assign skips an explicit non-zero ID and otherwise assigns a generated ID.
func (h *Hook) Assign(tx *gorm.DB, dst *int64) error {
	if dst == nil {
		return errors.New("ID 目标不能为空")
	}
	if *dst != 0 {
		return nil
	}

	generator := h.Generator()
	if generator == nil {
		return ErrGeneratorNotInitialized
	}

	table := ""
	if tx != nil && tx.Statement != nil {
		table = tx.Statement.Table
	}
	bizTag, err := h.BizTagOf(table)
	if err != nil {
		return err
	}

	ctx := context.Background()
	if tx != nil && tx.Statement != nil && tx.Statement.Context != nil {
		ctx = tx.Statement.Context
	}
	id, err := generator.Next(ctx, bizTag)
	if err != nil {
		return fmt.Errorf("ID 生成失败(biz_tag=%s): %w", bizTag, err)
	}
	*dst = id
	return nil
}

// BizTagOf maps a table name to its registered biz_tag.
func (h *Hook) BizTagOf(table string) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	bizTag, ok := h.tableBizTag[table]
	if !ok {
		return "", fmt.Errorf("表 %s 无对应 biz_tag（未配置发号映射）", table)
	}
	return bizTag, nil
}

// Tags returns the unique sorted biz_tag values used by this hook.
func (h *Hook) Tags() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	seen := make(map[string]struct{}, len(h.tableBizTag))
	tags := make([]string, 0, len(h.tableBizTag))
	for _, bizTag := range h.tableBizTag {
		if _, ok := seen[bizTag]; ok {
			continue
		}
		seen[bizTag] = struct{}{}
		tags = append(tags, bizTag)
	}
	sort.Strings(tags)
	return tags
}
