// Package idgorm provides the GORM integration for devops-id consumers.
package idgorm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"

	"gorm.io/gorm"
)

// ErrGeneratorNotInitialized is returned when a model is created before an ID generator is injected.
var ErrGeneratorNotInitialized = errors.New("ID 生成器未初始化（SetIDGenerator 未装配）")

// Generator allocates one ID for a registered biz_tag.
type Generator interface {
	Next(ctx context.Context, bizTag string) (int64, error)
}

// BizTagProvider optionally provides an explicit biz_tag for a model.
// Models that do not implement it use the snake_case Go type name.
type BizTagProvider interface {
	BizTag() string
}

// Hook resolves model biz_tags and assigns generated primary keys.
type Hook struct {
	mu          sync.RWMutex
	generator   Generator
	tableBizTag map[string]string
}

// New creates a GORM ID hook. The optional table-to-biz_tag map is copied and
// used as a compatibility override for models without an explicit BizTag().
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
	var model any
	if tx != nil && tx.Statement != nil {
		table = tx.Statement.Table
		model = tx.Statement.Model
	}
	var bizTag string
	var err error
	if _, hasOverride := model.(BizTagProvider); model != nil && hasOverride {
		bizTag, err = h.BizTagOf(model)
	} else if tag, ok := h.legacyBizTag(table); ok {
		bizTag = tag
	} else if model != nil {
		bizTag, err = h.BizTagOf(model)
	} else {
		// Preserve support for callers that only provide a table name through
		// the legacy mapping; normal GORM creates derive the tag from the model.
		bizTag, err = h.BizTagOf(table)
	}
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

// BizTagOf resolves a model to its biz_tag. String inputs are supported for
// compatibility with the legacy table-to-tag mapping.
func (h *Hook) BizTagOf(model any) (string, error) {
	if model == nil {
		return "", errors.New("biz_tag 模型不能为空")
	}

	if table, ok := model.(string); ok {
		return h.legacyBizTagOf(table)
	}

	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Pointer && modelValue.IsNil() {
		return "", errors.New("biz_tag 模型不能是 nil 指针")
	}

	if provider, ok := model.(BizTagProvider); ok {
		tag := strings.TrimSpace(provider.BizTag())
		if tag == "" {
			return "", errors.New("模型 BizTag() 不能返回空值")
		}
		return tag, nil
	}

	modelType := reflect.TypeOf(model)
	for modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}
	if modelType.Name() == "" {
		return "", fmt.Errorf("无法从模型类型 %s 推导 biz_tag", modelType)
	}
	return toSnakeCase(modelType.Name()), nil
}

func (h *Hook) legacyBizTagOf(table string) (string, error) {
	if bizTag, ok := h.legacyBizTag(table); ok {
		return bizTag, nil
	}
	return "", fmt.Errorf("表 %s 无对应 biz_tag（未配置发号映射）", table)
}

func (h *Hook) legacyBizTag(table string) (string, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	bizTag, ok := h.tableBizTag[table]
	return bizTag, ok
}

func toSnakeCase(value string) string {
	var result strings.Builder
	runes := []rune(value)
	for i, current := range runes {
		if unicode.IsUpper(current) {
			if i > 0 && (unicode.IsLower(runes[i-1]) ||
				unicode.IsDigit(runes[i-1]) ||
				(unicode.IsUpper(runes[i-1]) && i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				result.WriteByte('_')
			}
			result.WriteRune(unicode.ToLower(current))
			continue
		}
		result.WriteRune(current)
	}
	return result.String()
}

// Tags returns the unique sorted biz_tag values configured in the legacy mapping.
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
