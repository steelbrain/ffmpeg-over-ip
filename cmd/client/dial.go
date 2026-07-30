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

// ShuffleAddresses returns a randomized copy of addrs for fair 1/N load balancing.
// Fisher-Yates with crypto/rand.
func ShuffleAddresses(addrs []string) []string {
	if len(addrs) <= 1 {
		out := make([]string, len(addrs))
		copy(out, addrs)
		return out
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

// dialResult holds outcome of one dial attempt.
type dialResult struct {
	conn net.Conn
	addr string
	err  error
}

// dialWithFailover is shuffled sequential with hedged start for fast failover.
// - Shuffles list for fair 1/N LB
// - Happy path: 1 SYN
// - Blackholed node: instead of waiting full timeout (5s), we start next attempt
//   after hedgeDelay (250ms). Worst case 250ms to fail over, not 5s.
// This is faster than pure sequential, but not thundering herd like old racing dial
// which opened N connections at once.
func dialWithFailover(parent context.Context, addrs []string, timeout time.Duration) (net.Conn, string, error) {
	return dialHedged(parent, addrs, timeout, 250*time.Millisecond)
}

func dialHedged(parent context.Context, addrs []string, timeout, hedgeDelay time.Duration) (net.Conn, string, error) {
	if len(addrs) == 0 {
		return nil, "", errors.New("no server address configured")
	}
	if len(addrs) == 1 {
		network, target := config.ParseAddress(addrs[0])
		d := net.Dialer{Timeout: timeout}
		ctx, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		conn, err := d.DialContext(ctx, network, target)
		if err != nil {
			return nil, "", err
		}
		return conn, addrs[0], nil
	}

	shuffled := ShuffleAddresses(addrs)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	results := make(chan dialResult, len(shuffled))

	// Launch hedged dials: first immediate, next after hedgeDelay, etc.
	for i, addr := range shuffled {
		go func(idx int, a string) {
			if idx > 0 && hedgeDelay > 0 {
				timer := time.NewTimer(hedgeDelay * time.Duration(idx))
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					return
				}
			}
			// If already cancelled (winner found), skip dial
			if ctx.Err() != nil {
				return
			}
			network, target := config.ParseAddress(a)
			d := net.Dialer{Timeout: timeout}
			dCtx, dCancel := context.WithTimeout(ctx, timeout)
			conn, err := d.DialContext(dCtx, network, target)
			dCancel()
			select {
			case results <- dialResult{conn: conn, addr: a, err: err}:
			case <-ctx.Done():
				if conn != nil {
					conn.Close()
				}
			}
		}(i, addr)
	}

	var errs []error
	for i := 0; i < len(shuffled); i++ {
		select {
		case res := <-results:
			if res.err == nil {
				cancel() // stop other hedged attempts
				// Drain and close losers in background
				go func(remaining int) {
					for j := 0; j < remaining; j++ {
						r := <-results
						if r.conn != nil {
							r.conn.Close()
						}
					}
				}(len(shuffled) - i - 1)
				return res.conn, res.addr, nil
			}
			errs = append(errs, fmt.Errorf("%s: %w", res.addr, res.err))
		case <-parent.Done():
			cancel()
			go func(remaining int) {
				for j := 0; j < remaining; j++ {
					if r := <-results; r.conn != nil {
						r.conn.Close()
					}
				}
			}(len(shuffled) - i)
			return nil, "", parent.Err()
		}
	}
	return nil, "", errors.Join(errs...)
}

// alias for tests and gradual migration
func dialRandomizedWithFailover(parent context.Context, addrs []string, timeout time.Duration) (net.Conn, string, error) {
	return dialWithFailover(parent, addrs, timeout)
}
