package session

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// Writer is a thread-safe protocol message writer. Multiple goroutines can
// call WriteMessage concurrently without interleaving.
type Writer struct {
	mu        sync.Mutex
	w         io.Writer
	lastSend  atomic.Int64 // MonoNow of the last write
	writingAt atomic.Int64 // MonoNow the current write started; 0 if none
}

func NewWriter(w io.Writer) *Writer {
	sw := &Writer{w: w}
	sw.lastSend.Store(MonoNow())
	return sw
}

func (sw *Writer) WriteMessage(msgType uint8, payload []byte) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.write(msgType, payload)
}

func (sw *Writer) write(msgType uint8, payload []byte) error {
	now := MonoNow()
	sw.lastSend.Store(now)
	sw.writingAt.Store(now)
	defer sw.writingAt.Store(0)
	return protocol.WriteMessageTo(sw.w, msgType, payload)
}

// StuckFor reports how long the write in progress has been running, or 0 if
// none is. A write that runs for minutes means the peer stopped reading.
func (sw *Writer) StuckFor(now time.Time) time.Duration {
	at := sw.writingAt.Load()
	if at == 0 {
		return 0
	}
	return now.Sub(MonoTime(at))
}

// TryWriteMessage writes the message only if no other write is in progress,
// and reports whether it did. Keepalive pings and pongs use it: if another
// write is running, the peer is about to receive data anyway, and if that
// write is stuck, waiting behind it would stop the caller from noticing a
// dead peer.
func (sw *Writer) TryWriteMessage(msgType uint8, payload []byte) (bool, error) {
	if !sw.mu.TryLock() {
		return false, nil
	}
	defer sw.mu.Unlock()
	return true, sw.write(msgType, payload)
}

func (sw *Writer) LastSendTime() time.Time {
	return MonoTime(sw.lastSend.Load())
}
