package session

import (
	"io"
	"net"
	"time"
)

// Closing a TCP connection while data from the peer is still unread makes
// the kernel send a reset instead of a normal close, and a reset can destroy
// data the peer hasn't read yet, such as the exit code or error message just
// sent. A client may still be sending stdin when ffmpeg exits, so the server
// half-closes first and reads until the client hangs up.

// drainLimit bounds what CloseGracefully reads and drops.
const drainLimit = 64 * 1024 * 1024

// CloseGracefully half-closes conn, reads and drops whatever the peer still
// sends until it hangs up or timeout passes, then closes conn. Connections
// that can't half-close are closed at once.
func CloseGracefully(conn net.Conn, timeout time.Duration) {
	if cw, ok := conn.(interface{ CloseWrite() error }); ok && cw.CloseWrite() == nil {
		conn.SetReadDeadline(time.Now().Add(timeout))
		io.Copy(io.Discard, io.LimitReader(conn, drainLimit))
	}
	conn.Close()
}

// canHalfClose reports whether conn supports CloseWrite.
func canHalfClose(conn net.Conn) bool {
	_, ok := conn.(interface{ CloseWrite() error })
	return ok
}
