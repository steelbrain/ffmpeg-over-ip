package main

import (
	"sync/atomic"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/session"
)

// waitTimer measures how long the message loop has been waiting for the
// server. Only that waiting counts toward the keepalive timeout: the loop
// can be busy for a long time with a slow reader on stdout or a FIFO that
// has no writer yet, while the server is fine.
type waitTimer struct {
	since atomic.Int64 // session.MonoNow; 0 while the loop is busy
}

// start marks the loop as waiting for the next message.
func (w *waitTimer) start() { w.since.Store(session.MonoNow()) }

// stop marks the loop as busy handling a message.
func (w *waitTimer) stop() { w.since.Store(0) }

// waited reports how long the loop has been waiting, or 0 if it is busy.
func (w *waitTimer) waited(now time.Time) time.Duration {
	since := w.since.Load()
	if since == 0 {
		return 0
	}
	return now.Sub(session.MonoTime(since))
}
