package main

import (
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
	// Detect program from argv[0]
	program := protocol.ProgramFFmpeg
	if strings.Contains(filepath.Base(os.Args[0]), "ffprobe") {
		program = protocol.ProgramFFprobe
	}

	// All args go to ffmpeg — no --config flag
	args := os.Args[1:]

	// Check for debug flag anywhere in args
	for _, arg := range args {
		if arg == "--debug-print-search-paths" {
			for _, p := range config.SearchPaths("client") {
				fmt.Println(p)
			}
			return
		}
	}

	// Load config
	cfg, err := config.LoadClientConfig("")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	config.SetupLogging(cfg.Log)

	// Connect to server
	network, addr := config.ParseAddress(cfg.Address)
	conn, err := net.Dial(network, addr)
	if err != nil {
		log.Fatalf("failed to connect to %s: %v", addr, err)
	}
	defer conn.Close()

	// Generate nonce
	var nonce [protocol.NonceLength]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		log.Fatalf("failed to generate nonce: %v", err)
	}

	// Sign and send command
	sig := auth.Sign(cfg.AuthSecret, protocol.CurrentVersion, nonce, program, args)
	cmd := &protocol.CommandMessage{
		Nonce:     nonce,
		Signature: sig,
		Program:   program,
		Args:      args,
	}
	if err := protocol.WriteMessageTo(conn, protocol.MsgCommand, cmd.Encode()); err != nil {
		log.Fatalf("failed to send command: %v", err)
	}

	// Set up serialized writer for concurrent TCP writes
	w := session.NewWriter(conn)

	// Track last received message for keepalive
	var lastRecv atomic.Int64
	lastRecv.Store(time.Now().UnixNano())

	// Exit code channel — set when MsgExitCode is received
	exitCh := make(chan int, 1)

	// Stdin forwarding
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

	// Signal handler (Ctrl-C)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		w.WriteMessage(protocol.MsgCancel, nil)

		// Wait for exit code with timeout
		select {
		case <-exitCh:
		case <-time.After(5 * time.Second):
		}
		conn.Close()
		os.Exit(1)
	}()

	// Keepalive
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

	// File handler for I/O requests
	handler := filehandler.NewHandler()
	defer handler.CloseAll()

	// Message loop (main goroutine)
	for {
		msg, err := protocol.ReadMessageFrom(conn)
		if err != nil {
			if err == io.EOF {
				log.Fatal("server closed connection")
			}
			log.Fatalf("read error: %v", err)
		}

		lastRecv.Store(time.Now().UnixNano())

		switch {
		case protocol.IsFileIORequest(msg.Type):
			respType, respPayload, err := handler.HandleMessage(msg.Type, msg.Payload)
			if err != nil {
				log.Printf("file handler error: %v", err)
				continue
			}
			w.WriteMessage(respType, respPayload)

		case msg.Type == protocol.MsgStdout:
			os.Stdout.Write(msg.Payload)

		case msg.Type == protocol.MsgStderr:
			os.Stderr.Write(msg.Payload)

		case msg.Type == protocol.MsgExitCode:
			code := int(binary.BigEndian.Uint32(msg.Payload))
			select {
			case exitCh <- code:
			default:
			}
			os.Exit(code)

		case msg.Type == protocol.MsgError:
			fmt.Fprintf(os.Stderr, "server error: %s\n", string(msg.Payload))
			os.Exit(1)

		case msg.Type == protocol.MsgPing:
			w.WriteMessage(protocol.MsgPong, msg.Payload)

		case msg.Type == protocol.MsgPong:
			// keepalive response, nothing to do (lastRecv already updated)

		default:
			log.Printf("unknown message type 0x%02x, ignoring", msg.Type)
		}
	}
}
