package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/auth"
	"github.com/steelbrain/ffmpeg-over-ip/internal/config"
	"github.com/steelbrain/ffmpeg-over-ip/internal/filehandler"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
	"github.com/steelbrain/ffmpeg-over-ip/internal/session"
)

const (
	keepaliveSendInterval = 30 * time.Second
	keepaliveRecvTimeout  = 150 * time.Second
)

func main() {
	program := protocol.ProgramFFmpeg
	if strings.Contains(filepath.Base(os.Args[0]), "ffprobe") {
		program = protocol.ProgramFFprobe
	}
	args := os.Args[1:]
	for _, arg := range args {
		if arg == "--debug-print-search-paths" {
			for _, p := range config.SearchPaths("client") {
				fmt.Println(p)
			}
			return
		}
	}

	cfg, err := config.LoadClientConfig("")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	defer config.SetupLogging(cfg.Log)()

	addrs := cfg.Addresses()
	if len(addrs) == 0 {
		log.Fatalf("no server address")
	}

	// Try servers with hedged failover + busy retry
	remaining := ShuffleAddresses(addrs)
	var lastErr error

	for len(remaining) > 0 {
		conn, addr, err := dialWithFailover(context.Background(), remaining, cfg.DialTimeoutDuration())
		if err != nil {
			lastErr = err
			break
		}

		// Send command
		var nonce [protocol.NonceLength]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			conn.Close()
			lastErr = err
			remaining = removeAddr(remaining, addr)
			continue
		}
		sig := auth.Sign(cfg.AuthSecret, protocol.CurrentVersion, nonce, program, args)
		cmd := &protocol.CommandMessage{
			Nonce:     nonce,
			Signature: sig,
			Program:   program,
			Args:      args,
		}
		if err := protocol.WriteMessageTo(conn, protocol.MsgCommand, cmd.Encode()); err != nil {
			conn.Close()
			lastErr = err
			remaining = removeAddr(remaining, addr)
			continue
		}

		// Peek first response to detect busy fast
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		msg, err := protocol.ReadMessageFrom(conn)
		_ = conn.SetReadDeadline(time.Time{})
		if err != nil {
			conn.Close()
			lastErr = err
			remaining = removeAddr(remaining, addr)
			continue
		}
		if msg.Type == protocol.MsgError && strings.Contains(strings.ToLower(string(msg.Payload)), "busy") {
			log.Printf("server %s busy, trying next (%d remaining)", addr, len(remaining)-1)
			conn.Close()
			lastErr = fmt.Errorf("busy: %s", string(msg.Payload))
			remaining = removeAddr(remaining, addr)
			continue
		}

		// Not busy - run full session with this conn and first msg
		if len(addrs) > 1 {
			log.Printf("connected to %s", addr)
		}
		runSessionWithFirstMsg(conn, msg, cfg, program, args)
		return // runSession only returns on fatal error, it os.Exits on success
	}

	// All tried
	if cfg.FallbackToLocal {
		deps, derr := realFallbackDeps()
		if derr != nil {
			log.Fatalf("fallback: %v", derr)
		}
		os.Exit(runLocalFallback(deps, program, args, cfg.FallbackRewrites, cfg.Debug, lastErr, strings.Join(addrs, ",")))
	}
	log.Fatalf("all servers busy/unreachable (%s): %v", strings.Join(addrs, ","), lastErr)
}

func removeAddr(list []string, target string) []string {
	out := make([]string, 0, len(list)-1)
	for _, a := range list {
		if a != target {
			out = append(out, a)
		}
	}
	return out
}

func runSessionWithFirstMsg(conn net.Conn, firstMsg *protocol.Message, cfg *config.ClientConfig, program uint8, args []string) {
	defer conn.Close()

	w := session.NewWriter(conn)
	var lastRecv atomic.Int64
	lastRecv.Store(time.Now().UnixNano())
	exitCh := make(chan int, 1)

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				w.WriteMessage(protocol.MsgStdin, buf[:n])
			}
			if err != nil {
				w.WriteMessage(protocol.MsgStdinClose, nil)
				return
			}
		}
	}()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		w.WriteMessage(protocol.MsgCancel, nil)
		select {
		case <-exitCh:
		case <-time.After(5 * time.Second):
		}
		conn.Close()
		os.Exit(1)
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if time.Since(w.LastSendTime()) >= keepaliveSendInterval {
				w.WriteMessage(protocol.MsgPing, nil)
			}
			lr := time.Unix(0, lastRecv.Load())
			if time.Since(lr) >= keepaliveRecvTimeout {
				log.Printf("server keepalive timeout")
				conn.Close()
				os.Exit(1)
			}
		}
	}()

	handler := filehandler.NewHandler()
	defer handler.CloseAll()

	process := func(m *protocol.Message) {
		lastRecv.Store(time.Now().UnixNano())
		switch {
		case protocol.IsFileIORequest(m.Type):
			if m.Type == protocol.MsgRead {
				if err := handler.HandleReadTo(m.Payload, w); err != nil {
					log.Printf("file handler error: %v", err)
				}
				return
			}
			respType, respPayload, err := handler.HandleMessage(m.Type, m.Payload)
			if err != nil {
				log.Printf("file handler error: %v", err)
				return
			}
			w.WriteMessage(respType, respPayload)
		case m.Type == protocol.MsgStdout:
			os.Stdout.Write(m.Payload)
		case m.Type == protocol.MsgStderr:
			os.Stderr.Write(m.Payload)
		case m.Type == protocol.MsgExitCode:
			code := int(binary.BigEndian.Uint32(m.Payload))
			select {
			case exitCh <- code:
			default:
			}
			os.Exit(code)
		case m.Type == protocol.MsgError:
			fmt.Fprintf(os.Stderr, "server error: %s\n", string(m.Payload))
			os.Exit(1)
		case m.Type == protocol.MsgPing:
			w.WriteMessage(protocol.MsgPong, m.Payload)
		case m.Type == protocol.MsgPong:
		default:
			log.Printf("unknown message type 0x%02x, ignoring", m.Type)
		}
	}

	process(firstMsg)
	for {
		m, err := protocol.ReadMessageFrom(conn)
		if err != nil {
			if err == io.EOF {
				log.Fatal("server closed connection")
			}
			log.Fatalf("read error: %v", err)
		}
		process(m)
	}
}
