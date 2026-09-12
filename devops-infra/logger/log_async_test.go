package logger

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// slowWriter 模拟写入很慢的磁盘
type slowWriter struct {
	delay time.Duration
	mu    sync.Mutex
	count int
}

func (s *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
	return len(p), nil
}

// TestAsyncWriterDoesNotBlock 验证慢速磁盘下写入不会被拖住：
// 顺序写 50 条需要 50*20ms = 1s，异步模式下应远小于这个时间，多余的直接丢弃。
func TestAsyncWriterDoesNotBlock(t *testing.T) {
	const (
		delay   = 20 * time.Millisecond
		total   = 50
		bufSize = 2
	)

	dst := &slowWriter{delay: delay}
	w := newAsyncWriter(dst, bufSize)

	start := time.Now()
	for i := 0; i < total; i++ {
		_, err := w.Write([]byte("log line\n"))
		if err != nil && !errors.Is(err, ErrBufferFull) {
			t.Fatalf("非预期的写入错误: %v", err)
		}
	}
	elapsed := time.Since(start)

	// 顺序写需要约 1s，异步模式不应被阻塞
	if elapsed > 200*time.Millisecond {
		t.Errorf("写入耗时 %v，说明仍被磁盘 IO 阻塞（顺序写约 %v）", elapsed, delay*total)
	}

	if dropped := w.Dropped(); dropped == 0 {
		t.Error("缓冲区满时应产生丢弃计数")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
}

// TestAsyncWriterFlushesOnClose 验证缓冲区够用时一条不丢，且 Close 会把残留日志排空落盘。
func TestAsyncWriterFlushesOnClose(t *testing.T) {
	var buf bytes.Buffer
	w := newAsyncWriter(&buf, 128)

	const total = 10
	for i := 0; i < total; i++ {
		if _, err := w.Write([]byte("line\n")); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	if got := bytes.Count(buf.Bytes(), []byte("line\n")); got != total {
		t.Errorf("Close 后应落盘 %d 条，实际 %d 条", total, got)
	}

	if dropped := w.Dropped(); dropped != 0 {
		t.Errorf("缓冲区充足时不应丢弃，实际丢弃 %d 条", dropped)
	}
}
