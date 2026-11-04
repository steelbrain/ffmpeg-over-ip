package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/config"
	"github.com/steelbrain/ffmpeg-over-ip/internal/filehandler"
	"github.com/steelbrain/ffmpeg-over-ip/internal/handshake"
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

	defer config.SetupLogging(cfg.Log)()

	// Connect to server. Fallback only triggers on dial failure — once a
	// connection is established, mid-stream errors stay fatal so we don't
	// restart a partial transcode locally.
	network, addr := config.ParseAddress(cfg.Address)
	conn, err := net.Dial(network, addr)
	if err != nil {
		if cfg.FallbackToLocal {
			deps, derr := realFallbackDeps()
			if derr != nil {
				log.Fatalf("fallback: %v", derr)
			}
			os.Exit(runLocalFallback(deps, program, args, cfg.FallbackRewrites, cfg.Debug, err, addr))
		}
		log.Fatalf("failed to connect to %s: %v", addr, err)
	}
	defer conn.Close()

	// Both sides prove they know the secret, then the signed command goes
	// to the server.
	if err := handshake.Client(conn, cfg.AuthSecret, program, args); err != nil {
		// Always on stderr, like errors the server reports: the log is
		// discarded unless configured, and these need the user's attention.
		var serverErr *handshake.ServerError
		if !errors.As(err, &serverErr) && !errors.Is(err, handshake.ErrServerAuth) {
			err = fmt.Errorf("handshake with %s failed: %w", addr, err)
		}
		log.Print(err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Set up serialized writer for concurrent TCP writes
	w := session.NewWriter(conn)

	// How long the message loop has waited for the server, for keepalive
	var waiting waitTimer
	waiting.start()

	// Exit code channel — set when MsgExitCode is received
	exitCh := make(chan int, 1)

	// Stdin forwarding, at most protocol.StdinWindow bytes ahead of the
	// server's acks
	credit := newStdinCredit()
	go pumpStdin(os.Stdin, w, credit)

	// Signal handler (Ctrl-C)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		// From its own goroutine: the write can wait behind one stuck on a
		// dead connection, and the timeout below must start regardless.
		go w.WriteMessage(protocol.MsgCancel, nil)

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
			// Check for a silent server before pinging, and never wait
			// behind a write that may be stuck on a dead connection. A
			// write stuck that long also means the server is gone: it
			// always reads what the client sends.
			now := time.Now()
			if waiting.waited(now) >= keepaliveRecvTimeout || w.StuckFor(now) >= keepaliveRecvTimeout {
				log.Printf("server keepalive timeout")
				conn.Close()
				os.Exit(1)
			}
			if time.Since(w.LastSendTime()) >= keepaliveSendInterval {
				w.TryWriteMessage(protocol.MsgPing, nil)
			}
		}
	}()

	// File handler for I/O requests
	handler := filehandler.NewHandler()
	defer handler.CloseAll()

	// Message loop (main goroutine)
	for {
		waiting.start()
		msg, err := protocol.ReadMessageFrom(conn)
		if err != nil {
			if err == io.EOF {
				log.Fatal("server closed connection")
			}
			log.Fatalf("read error: %v", err)
		}
		waiting.stop()

		switch {
		case protocol.IsFileIORequest(msg.Type):
			if msg.Type == protocol.MsgRead {
				if err := handler.HandleReadTo(msg.Payload, w); err != nil {
					log.Printf("file handler error: %v", err)
				}
				continue
			}
			respType, respPayload, err := handler.HandleMessage(msg.Type, msg.Payload)
			if err != nil {
				log.Printf("file handler error: %v", err)
				continue
			}
			w.WriteMessage(respType, respPayload)

		case msg.Type == protocol.MsgStdout:
			if _, err := os.Stdout.Write(msg.Payload); err != nil {
				// Whoever read our stdout is gone. On Unix SIGPIPE ends the
				// client here; Windows has none, so exit the same way and
				// let the server stop ffmpeg instead of running it to the
				// end for nobody.
				log.Printf("writing stdout: %v", err)
				conn.Close()
				os.Exit(1)
			}

		case msg.Type == protocol.MsgStderr:
			os.Stderr.Write(msg.Payload)

		case msg.Type == protocol.MsgExitCode:
			if len(msg.Payload) < 4 {
				log.Fatalf("malformed exit code message (%d bytes)", len(msg.Payload))
			}
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
			w.TryWriteMessage(protocol.MsgPong, msg.Payload)

		case msg.Type == protocol.MsgStdinAck:
			n, err := protocol.DecodeStdinAck(msg.Payload)
			if err != nil {
				log.Fatalf("malformed stdin ack: %v", err)
			}
			credit.release(int(n))

		case msg.Type == protocol.MsgPong:
			// keepalive response, nothing to do

		default:
			log.Printf("unknown message type 0x%02x, ignoring", msg.Type)
		}
	}
}
