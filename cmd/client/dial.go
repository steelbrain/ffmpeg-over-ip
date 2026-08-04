package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/config"
)

const defaultDialTimeout = config.DefaultDialTimeout

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

func dialWithFailover(ctx context.Context, addrs []string) (net.Conn, string, error) {
	return dialWithFailoverTimeout(ctx, addrs, defaultDialTimeout)
}

func dialWithFailoverTimeout(ctx context.Context, addrs []string, timeout time.Duration) (net.Conn, string, error) {
	if len(addrs) == 0 {
		return nil, "", fmt.Errorf("no server address configured")
	}
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	shuffled := ShuffleAddresses(addrs)
	var lastErr error
	for _, addr := range shuffled {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		network, target := config.ParseAddress(addr)
		dialer := net.Dialer{Timeout: timeout}
		dialCtx, cancel := context.WithTimeout(ctx, timeout)
		conn, err := dialer.DialContext(dialCtx, network, target)
		cancel()
		if err == nil {
			return conn, addr, nil
		}
		lastErr = fmt.Errorf("%s: %w", addr, err)
	}
	return nil, "", lastErr
}
