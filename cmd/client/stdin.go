package main

import (
	"io"
	"sync"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
	"github.com/steelbrain/ffmpeg-over-ip/internal/session"
)

// stdinCredit keeps at most protocol.StdinWindow bytes of stdin sent but not
// yet acknowledged by the server (MsgStdinAck), which bounds what the server
// has to queue for a child that isn't reading its stdin.
type stdinCredit struct {
	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
}

func newStdinCredit() *stdinCredit {
	c := &stdinCredit{}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// acquire blocks until n more bytes fit in the window.
func (c *stdinCredit) acquire(n int) {
	c.mu.Lock()
	for c.inflight+n > protocol.StdinWindow {
		c.cond.Wait()
	}
	c.inflight += n
	c.mu.Unlock()
}

// release returns n acknowledged bytes to the window.
func (c *stdinCredit) release(n int) {
	c.mu.Lock()
	c.inflight -= n
	if c.inflight < 0 {
		c.inflight = 0
	}
	c.mu.Unlock()
	c.cond.Broadcast()
}

// pumpStdin forwards r to the server as MsgStdin within the window, then
// sends MsgStdinClose at EOF or on a read error.
func pumpStdin(r io.Reader, w *session.Writer, credit *stdinCredit) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			credit.acquire(n)
			if w.WriteMessage(protocol.MsgStdin, buf[:n]) != nil {
				return
			}
		}
		if err != nil {
			w.WriteMessage(protocol.MsgStdinClose, nil)
			return
		}
	}
}
