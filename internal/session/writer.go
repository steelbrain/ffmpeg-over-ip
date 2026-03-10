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
	mu       sync.Mutex
	w        io.Writer
	lastSend atomic.Int64 // unix nano of last write
}

func NewWriter(w io.Writer) *Writer {
	sw := &Writer{w: w}
	sw.lastSend.Store(time.Now().UnixNano())
	return sw
}

func (sw *Writer) WriteMessage(msgType uint8, payload []byte) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.lastSend.Store(time.Now().UnixNano())
	return protocol.WriteMessageTo(sw.w, msgType, payload)
}

func (sw *Writer) LastSendTime() time.Time {
	return time.Unix(0, sw.lastSend.Load())
}
