package logging

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// ErrBufferFull 表示异步缓冲区已满，该条日志被丢弃。
// 丢弃是刻意的设计：宁可少记几条日志，也不能让磁盘 IO 拖慢业务。
var ErrBufferFull = errors.New("logger: 缓冲区已满，日志被丢弃")

// asyncWriter 把同步的文件写入转成异步模式。
//
// 调用方（slog Handler）只做一次非阻塞的 channel 投递，
// 真正落盘由后台 goroutine 完成，因此磁盘 IO 不会阻塞业务协程。
type asyncWriter struct {
	ch   chan []byte
	dst  io.Writer
	done chan struct{}
	wg   sync.WaitGroup

	closed  atomic.Bool
	dropped atomic.Uint64
}

func newAsyncWriter(dst io.Writer, bufSize int) *asyncWriter {
	if bufSize <= 0 {
		bufSize = defaultBufferSize
	}
	writer := &asyncWriter{
		ch:   make(chan []byte, bufSize),
		dst:  dst,
		done: make(chan struct{}),
	}
	writer.wg.Add(1)
	go writer.run()
	return writer
}

func (writer *asyncWriter) run() {
	defer writer.wg.Done()

	for {
		select {
		case line := <-writer.ch:
			_, _ = writer.dst.Write(line)
		case <-writer.done:
			for {
				select {
				case line := <-writer.ch:
					_, _ = writer.dst.Write(line)
				default:
					return
				}
			}
		}
	}
}

// Write 实现 io.Writer。缓冲区满时直接丢弃并返回错误，绝不阻塞调用方。
func (writer *asyncWriter) Write(line []byte) (int, error) {
	// slog 会复用底层 buffer，必须拷贝一份再投递，否则后台读到的是被覆盖的数据
	buffer := make([]byte, len(line))
	copy(buffer, line)

	select {
	case writer.ch <- buffer:
		return len(line), nil
	default:
		writer.dropped.Add(1)
		return len(line), ErrBufferFull
	}
}

// Close 通知后台协程排空并退出，会等待落盘完成。
func (writer *asyncWriter) Close() error {
	if !writer.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(writer.done)
	writer.wg.Wait()
	return nil
}

// Dropped 返回运行期间因缓冲区满而丢弃的日志条数。
func (writer *asyncWriter) Dropped() uint64 {
	return writer.dropped.Load()
}
