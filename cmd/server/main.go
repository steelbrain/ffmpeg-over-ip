package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/steelbrain/ffmpeg-over-ip/internal/auth"
	"github.com/steelbrain/ffmpeg-over-ip/internal/config"
	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
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

	config.SetupLogging(cfg.Log)

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

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return // shutting down
			}
			log.Printf("accept error: %v", err)
			continue
		}

		go handleConnection(ctx, conn, cfg, ffmpegPath, ffprobePath)
	}
}

func handleConnection(ctx context.Context, conn net.Conn, cfg *config.ServerConfig, ffmpegPath, ffprobePath string) {
	defer conn.Close()

	// Read command message
	msg, err := protocol.ReadMessageFrom(conn)
	if err != nil {
		log.Printf("failed to read command: %v", err)
		return
	}
	if msg.Type != protocol.MsgCommand {
		sendError(conn, fmt.Sprintf("expected command message (0x%02x), got 0x%02x", protocol.MsgCommand, msg.Type))
		return
	}

	// Decode command
	cmd, err := protocol.DecodeCommandMessage(msg.Payload)
	if err != nil {
		sendError(conn, fmt.Sprintf("invalid command: %v", err))
		return
	}

	// Verify HMAC
	if !auth.Verify(cfg.AuthSecret, protocol.CurrentVersion, cmd.Nonce, cmd.Signature, cmd.Program, cmd.Args) {
		sendError(conn, "authentication failed")
		log.Printf("auth failed from %s", conn.RemoteAddr())
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
	args := applyRewrites(cmd.Args, cfg.Rewrites)

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

func applyRewrites(args []string, rewrites [][2]string) []string {
	if len(rewrites) == 0 {
		return args
	}
	result := make([]string, len(args))
	for i, arg := range args {
		result[i] = arg
		for _, rw := range rewrites {
			result[i] = strings.ReplaceAll(result[i], rw[0], rw[1])
		}
	}
	return result
}

func sendError(conn net.Conn, msg string) {
	protocol.WriteMessageTo(conn, protocol.MsgError, []byte(msg))
}
