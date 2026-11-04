package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/internal/config"
	"github.com/steelbrain/ffmpeg-over-ip/internal/handshake"
	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
	"github.com/steelbrain/ffmpeg-over-ip/internal/rewrite"
	"github.com/steelbrain/ffmpeg-over-ip/internal/session"
)

func main() {
	configPath := flag.String("config", "", "path to server config file")
	debugPaths := flag.Bool("debug-print-search-paths", false, "print config search paths and exit")
	flag.Parse()

	if *debugPaths {
		for _, p := range config.SearchPaths("server") {
			fmt.Println(p)
		}
		return
	}

	cfg, err := config.LoadServerConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	defer config.SetupLogging(cfg.Log)()

	// Resolve ffmpeg/ffprobe paths from server binary's directory
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to resolve executable path: %v", err)
	}
	exeDir := filepath.Dir(exePath)
	ffmpegPath := filepath.Join(exeDir, "ffmpeg")
	ffprobePath := filepath.Join(exeDir, "ffprobe")

	// Set up signal-aware context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	network, addr := config.ParseAddress(cfg.Address)
	listener, err := net.Listen(network, addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", addr, err)
	}
	defer listener.Close()

	if network == "unix" {
		if err := os.Chmod(addr, 0777); err != nil {
			log.Printf("warning: failed to chmod socket: %v", err)
		}
	}

	log.Printf("listening on %s (%s)", addr, network)

	// Stop accepting on context cancellation; clean up Unix socket
	go func() {
		<-ctx.Done()
		listener.Close()
		if network == "unix" {
			os.Remove(addr)
		}
	}()

	var sessions sync.WaitGroup
	var acceptDelay time.Duration
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break // shutting down
			}
			// Back off on errors such as EMFILE, which would otherwise
			// repeat at once and fill the log.
			if acceptDelay == 0 {
				acceptDelay = 5 * time.Millisecond
			} else if acceptDelay *= 2; acceptDelay > time.Second {
				acceptDelay = time.Second
			}
			log.Printf("accept error: %v; retrying in %v", err, acceptDelay)
			time.Sleep(acceptDelay)
			continue
		}
		acceptDelay = 0

		sessions.Add(1)
		go func() {
			defer sessions.Done()
			handleConnection(ctx, conn, cfg, ffmpegPath, ffprobePath)
		}()
	}

	// Every session sees ctx canceled and stops its child. Wait for them so
	// no ffmpeg outlives the server. Restore default signal handling first,
	// so a second Ctrl-C still exits immediately.
	cancel()
	done := make(chan struct{})
	go func() {
		sessions.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(process.KillTimeout + 5*time.Second):
		log.Printf("shutdown: timed out waiting for sessions to stop")
	}
}

func handleConnection(ctx context.Context, conn net.Conn, cfg *config.ServerConfig, ffmpegPath, ffprobePath string) {
	// After an error message (or a finished session, which has closed conn
	// already), let the client read it before the connection goes away.
	defer session.CloseGracefully(conn, 5*time.Second)

	// Both sides prove they know the secret, then the client sends the
	// signed command (bounded by handshake.Timeout).
	cmd, err := handshake.Server(conn, cfg.AuthSecret)
	if err != nil {
		var rejected *handshake.Rejected
		if errors.As(err, &rejected) && rejected.Msg == "authentication failed" {
			log.Printf("auth failed from %s", conn.RemoteAddr())
		} else {
			log.Printf("handshake with %s failed: %v", conn.RemoteAddr(), err)
		}
		return
	}

	if ctx.Err() != nil {
		sendError(conn, "server is shutting down")
		return
	}

	// Determine binary path
	var binaryPath string
	switch cmd.Program {
	case protocol.ProgramFFmpeg:
		binaryPath = ffmpegPath
	case protocol.ProgramFFprobe:
		binaryPath = ffprobePath
	default:
		sendError(conn, fmt.Sprintf("unknown program: 0x%02x", cmd.Program))
		return
	}

	// Apply rewrites
	args := rewrite.Apply(cmd.Args, cfg.Rewrites)

	if cfg.Debug {
		log.Printf("[debug] original args: %v", cmd.Args)
		log.Printf("[debug] rewritten args: %v", args)
	}
	log.Printf("running %s %v (from %s)", filepath.Base(binaryPath), args, conn.RemoteAddr())

	// Start process
	proc := process.NewProcess(binaryPath, args)
	if err := proc.Start(ctx); err != nil {
		sendError(conn, fmt.Sprintf("failed to start process: %v", err))
		return
	}

	// Run session
	sess := session.NewSession(conn, proc)
	exitCode, err := sess.Run(ctx)
	if err != nil {
		log.Printf("session error: %v", err)
	}

	log.Printf("process exited with code %d (from %s)", exitCode, conn.RemoteAddr())
}

func sendError(conn net.Conn, msg string) {
	protocol.WriteMessageTo(conn, protocol.MsgError, []byte(msg))
}
