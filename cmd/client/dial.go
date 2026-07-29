package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/config"
)

// dialResult is the outcome of one address's dial attempt. Exactly one of
// conn / err is set.
type dialResult struct {
	conn net.Conn
	addr string
	err  error
}

// ShuffleAddresses returns a randomized copy of addrs to distribute dial attempts
// evenly across available nodes without thundering herd concurrency.
func ShuffleAddresses(addrs []string) []string {
	if len(addrs) <= 1 {
		return addrs
	}
	shuffled := make([]string, len(addrs))
	copy(shuffled, addrs)
	for i := len(shuffled) - 1; i > 0; i-- {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			break
		}
		j := int(binary.BigEndian.Uint64(b[:]) % uint64(i+1))
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	return shuffled
}

// dialRandomizedWithFailover shuffles candidate addresses to distribute load
// across cluster nodes and dials them sequentially with a per-attempt timeout,
// failing over immediately if an endpoint is offline or unavailable.
func dialRandomizedWithFailover(parent context.Context, addrs []string, timeout time.Duration) (net.Conn, string, error) {
	if len(addrs) == 0 {
		return nil, "", errors.New("no server address configured")
	}

	shuffled := ShuffleAddresses(addrs)
	var errs []error

	for _, addr := range shuffled {
		network, target := config.ParseAddress(addr)
		d := net.Dialer{Timeout: timeout}
		ctx, cancel := context.WithTimeout(parent, timeout)
		conn, err := d.DialContext(ctx, network, target)
		cancel()

		if err == nil {
			return conn, addr, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", addr, err))
	}

	return nil, "", errors.Join(errs...)
}

// dialFirstAvailable dials every address concurrently and returns the first
// connection to come up, along with the address that won. Losing attempts are
// cancelled and any connection they still manage to establish is closed.
//
// Only the dial is raced, never the command: the client sends its signed
// CommandMessage after this returns, so a transcode is never started on more
// than one server.
//
// Racing arbitrates on TCP handshake latency, which on a LAN is a proxy for
// "is this server up", not "is this server idle". Treat it as fast failover
// across interchangeable transcode nodes, not as load balancing.
//
// A zero timeout leaves the per-attempt deadline to the OS. The parent context
// bounds the race as a whole.
func dialFirstAvailable(parent context.Context, addrs []string, timeout time.Duration) (net.Conn, string, error) {
	if len(addrs) == 0 {
		return nil, "", errors.New("no server address configured")
	}

	ctx, cancel := context.WithCancel(parent)

	// Buffered to len(addrs) so no dial goroutine can block on send once the
	// race has stopped reading — that is what keeps them from leaking.
	ch := make(chan dialResult, len(addrs))
	for _, addr := range addrs {
		go func(addr string) {
			network, target := config.ParseAddress(addr)
			d := net.Dialer{Timeout: timeout}
			conn, err := d.DialContext(ctx, network, target)
			ch <- dialResult{conn: conn, addr: addr, err: err}
		}(addr)
	}

	// Cancelling races the TCP handshake, so a loser can still come back with
	// a live connection after the winner is picked. Nobody is reading the
	// channel by then, so each straggler has to be drained and closed or we
	// leak an established socket (and leave the server holding a session it
	// will never get a command on) per extra endpoint.
	drain := func(remaining int) {
		go func() {
			for range remaining {
				if res := <-ch; res.conn != nil {
					res.conn.Close()
				}
			}
		}()
	}

	errs := make([]error, 0, len(addrs))
	for i := range addrs {
		select {
		case res := <-ch:
			if res.err == nil {
				cancel()
				drain(len(addrs) - i - 1)
				return res.conn, res.addr, nil
			}
			errs = append(errs, fmt.Errorf("%s: %w", res.addr, res.err))
		case <-parent.Done():
			cancel()
			drain(len(addrs) - i)
			return nil, "", parent.Err()
		}
	}

	cancel()
	return nil, "", errors.Join(errs...)
}
