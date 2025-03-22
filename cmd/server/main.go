package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/pkg/common"
	"github.com/steelbrain/ffmpeg-over-ip/pkg/config"
	"github.com/steelbrain/ffmpeg-over-ip/pkg/protocol"
)

var (
	flagConfigPath       = flag.String("config", "", "Path to config file")
	flagDebugSearchPaths = flag.Bool("debug-print-search-paths", false, "Print config search paths and exit")
)

func main() {
	flag.Parse()

	// Get config paths
	configPaths := config.GetServerConfigPaths()
	if *flagConfigPath != "" {
		configPaths = []string{*flagConfigPath}
	}

	// Debug print config paths if requested
	if *flagDebugSearchPaths {
		common.PrintConfigPaths(configPaths)
		return
	}

	// Load config
	cfg, configPath, err := config.LoadServerConfig(configPaths)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	fmt.Printf("Loaded config from: %s\n", configPath)

	// Setup logger
	logDest, err := common.SetupLogger(cfg.Log.(string))
	if err != nil {
		log.Fatalf("Failed to setup logger: %v", err)
	}
	if logDest != nil && logDest != os.Stdout && logDest != os.Stderr {
		defer logDest.Close()
	}

	// Parse the address
	connInfo, err := protocol.ParseAddress(cfg.Address)
	if err != nil {
		log.Fatalf("Invalid address: %v", err)
	}

	// For Unix sockets, remove any existing socket file
	if connInfo.Network == "unix" {
		if err := os.Remove(connInfo.Address); err != nil && !os.IsNotExist(err) {
			log.Fatalf("Failed to remove existing socket file: %v", err)
		}
	}

	// Create listener
	listener, err := net.Listen(connInfo.Network, connInfo.Address)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	// Create a context that can be canceled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		fmt.Println("Received interrupt signal, shutting down...")
		cancel()
		listener.Close()
	}()

	fmt.Printf("Server listening on %s (%s)\n", cfg.Address, connInfo.Network)

	// Track active connections
	var wg sync.WaitGroup
	activeProcesses := make(map[string]*exec.Cmd)
	activeProcessesMutex := sync.Mutex{}

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				// Context canceled, we're shutting down
				break
			default:
				log.Printf("Error accepting connection: %v", err)
				continue
			}
			break
		}

		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()
			defer conn.Close()

			// Create a context for this connection that can be canceled
			connCtx, connCancel := context.WithCancel(ctx)
			defer connCancel()

			// Handle the connection
			handleConnection(connCtx, conn, cfg, &activeProcessesMutex, activeProcesses)
		}(conn)
	}

	// Wait for all connections to finish
	wg.Wait()
}

// handleConnection processes a client connection, authenticates it, and runs the requested ffmpeg command
// It manages the lifecycle of the ffmpeg process, including forwarding stdin/stdout/stderr and handling cancellation
func handleConnection(ctx context.Context, conn net.Conn, cfg *config.ServerConfig, mutex *sync.Mutex, activeProcesses map[string]*exec.Cmd) {
	if conn == nil {
		log.Printf("Error: received nil connection")
		return
	}

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("New connection from %s", remoteAddr)

	// Read the initial message, which should be a command
	msg, err := protocol.ReadMessage(conn)
	if err != nil {
		log.Printf("Error reading initial message from %s: %v", remoteAddr, err)
		return
	}

	// Validate message type
	if msg.Type != protocol.MessageTypeCommand {
		log.Printf("Expected command message, got type %d from %s", msg.Type, remoteAddr)
		// Attempt to send error message to client
		if err := protocol.WriteMessage(conn, protocol.MessageTypeError, []byte("Expected command message")); err != nil {
			log.Printf("Failed to send error message to %s: %v", remoteAddr, err)
		}
		return
	}

	// Parse the command message
	version, signature, args, err := protocol.ParseCommandMessage(msg.Payload)
	if err != nil {
		log.Printf("Error parsing command message: %v", err)
		protocol.WriteMessage(conn, protocol.MessageTypeError, []byte(fmt.Sprintf("Invalid command message: %v", err)))
		return
	}

	// Check protocol version
	if version != protocol.ProtocolVersion {
		log.Printf("Unsupported protocol version: %d", version)
		protocol.WriteMessage(conn, protocol.MessageTypeError, []byte(fmt.Sprintf("Unsupported protocol version: %d", version)))
		return
	}

	// Verify signature
	if !protocol.VerifySignature(cfg.AuthSecret, signature, args) {
		log.Printf("Invalid signature from %s", remoteAddr)
		protocol.WriteMessage(conn, protocol.MessageTypeError, []byte("Authentication failed: invalid signature"))
		return
	}

	// Apply path rewrites
	rewrittenArgs := common.RewriteCommandArgs(args, cfg.Rewrites)

	// Ensure ffmpeg path exists
	ffmpegPath := cfg.FFmpegPath
	if _, err := os.Stat(ffmpegPath); os.IsNotExist(err) {
		log.Printf("FFmpeg not found at configured path: %s", ffmpegPath)
		protocol.WriteMessage(conn, protocol.MessageTypeError, []byte(fmt.Sprintf("FFmpeg not found at %s", ffmpegPath)))
		return
	}

	// Prepare command
	cmd := exec.CommandContext(ctx, ffmpegPath, rewrittenArgs...)

	// Setup stdin pipe for the command
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("Error creating stdin pipe: %v", err)
		protocol.WriteMessage(conn, protocol.MessageTypeError, []byte(fmt.Sprintf("Error creating stdin pipe: %v", err)))
		return
	}

	// Setup stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Error creating stdout pipe: %v", err)
		protocol.WriteMessage(conn, protocol.MessageTypeError, []byte(fmt.Sprintf("Error creating stdout pipe: %v", err)))
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("Error creating stderr pipe: %v", err)
		protocol.WriteMessage(conn, protocol.MessageTypeError, []byte(fmt.Sprintf("Error creating stderr pipe: %v", err)))
		return
	}

	// Generate a unique ID for this process
	processID := fmt.Sprintf("%s-%d", remoteAddr, time.Now().UnixNano())

	// Store the command in the active processes map
	mutex.Lock()
	activeProcesses[processID] = cmd
	mutex.Unlock()

	// Remove the process from the map when it's done
	defer func() {
		mutex.Lock()
		delete(activeProcesses, processID)
		mutex.Unlock()
	}()

	// Start the command
	log.Printf("Starting FFmpeg: %s %v", ffmpegPath, rewrittenArgs)
	if err := cmd.Start(); err != nil {
		log.Printf("Error starting FFmpeg: %v", err)
		protocol.WriteMessage(conn, protocol.MessageTypeError, []byte(fmt.Sprintf("Error starting FFmpeg: %v", err)))
		return
	}

	// Setup a context for this specific command
	cmdCtx, cmdCancel := context.WithCancel(ctx)
	defer cmdCancel()

	// Wait for command completion or cancellation
	go func() {
		<-cmdCtx.Done()
		// If we're shutting down, terminate the process
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	// Handle stdin from client
	go handleStdin(cmdCtx, conn, stdin, cmdCancel)

	// Forward stdout to client
	go forwardOutput(cmdCtx, stdout, conn, protocol.MessageTypeStdout, cfg.Debug, "stdout")

	// Forward stderr to client
	go forwardOutput(cmdCtx, stderr, conn, protocol.MessageTypeStderr, cfg.Debug, "stderr")

	// Wait for the command to finish
	err = cmd.Wait()

	// Send exit code
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			log.Printf("Command error: %v", err)
			exitCode = 1
		}
	}

	log.Printf("FFmpeg process completed with exit code %d", exitCode)
	protocol.WriteMessage(conn, protocol.MessageTypeExitCode, []byte{byte(exitCode)})
}

// handleStdin manages bidirectional communication between the client and the ffmpeg process stdin.
//
// This function serves multiple purposes:
// 1. Forwards stdin data from client to the ffmpeg process
// 2. Processes control messages (stdin close, cancellation)
// 3. Detects client disconnection and triggers appropriate cleanup
// 4. Continues monitoring for cancellation requests even after stdin is closed
//
// The function uses a non-blocking approach for context cancellation while
// using blocking I/O for connection reads, providing efficient resource usage
// while maintaining responsiveness to cancellation signals.

func handleStdin(ctx context.Context, conn net.Conn, stdin io.WriteCloser, cmdCancel context.CancelFunc) {
	// Flag to track if cancellation has already been requested
	var canceled atomic.Bool
	// Validate parameters
	if conn == nil || stdin == nil || cmdCancel == nil {
		log.Printf("Error: received nil connection, stdin, or cmdCancel in handleStdin")
		return
	}

	// Keep track of the remote address for logging
	remoteAddr := "unknown"
	if conn.RemoteAddr() != nil {
		remoteAddr = conn.RemoteAddr().String()
	}

	// Ensure stdin is closed when we're done, but only if it's not nil
	defer func() {
		if stdin != nil {
			stdin.Close()
			log.Printf("Closed stdin for client %s during cleanup", remoteAddr)
		}
	}()

	log.Printf("Starting stdin handler for client %s", remoteAddr)

	// Create a dedicated goroutine to monitor for context cancellation
	// This allows us to read from conn without timeouts while still respecting ctx
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			reason := "unknown reason"
			if ctx.Err() != nil {
				reason = ctx.Err().Error()
			}
			log.Printf("Context canceled (%s), stopping stdin handler for %s", reason, remoteAddr)

			// Set a short timeout for graceful shutdown before force-closing
			// This gives any in-flight operations a chance to complete
			timeout := 100 * time.Millisecond
			_ = conn.SetReadDeadline(time.Now().Add(timeout))

			// If there's still activity after timeout, force close
			time.AfterFunc(timeout, func() {
				if err := conn.Close(); err != nil {
					log.Printf("Error closing connection to %s: %v", remoteAddr, err)
				} else {
					log.Printf("Force closed connection to %s after timeout", remoteAddr)
				}
			})
		case <-done:
			// Normal exit, do nothing
		}
	}()

	defer close(done)

	// Set no deadline - we'll just wait for the connection to close or for messages
	conn.SetReadDeadline(time.Time{})

	for {
		// Read a message from the client - this will block until data arrives or connection closes
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			// Handle various connection error conditions
			if err == io.EOF {
				log.Printf("Client %s closed connection gracefully (EOF)", remoteAddr)
				// Client disconnected - cancel the command if not already canceled
				if !canceled.Swap(true) {
					log.Printf("Canceling command due to client disconnection: %s", remoteAddr)
					cmdCancel()
				}
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout errors should only happen if the connection was explicitly set with a deadline
				log.Printf("Timeout reading from client %s: %v", remoteAddr, err)
			} else {
				// Other network errors typically indicate an abrupt disconnection
				log.Printf("Error reading stdin message from %s: %v", remoteAddr, err)
				// Client likely disconnected - cancel the command if not already canceled
				if !canceled.Swap(true) {
					log.Printf("Canceling command due to connection error: %s", remoteAddr)
					cmdCancel()
				}
			}
			return
		}

		// Handle different message types
		switch msg.Type {
		case protocol.MessageTypeStdin:
			// Check if stdin is already closed
			if stdin == nil {
				// Stdin already closed, ignore this data
				log.Printf("Received stdin data after stdin was closed, ignoring")
				continue
			}

			// Write data to stdin
			bytes, err := stdin.Write(msg.Payload)
			if err != nil {
				log.Printf("Error writing %d bytes to stdin: %v", len(msg.Payload), err)
				return
			}
			if bytes != len(msg.Payload) {
				log.Printf("Warning: only wrote %d/%d bytes to stdin", bytes, len(msg.Payload))
			}
		case protocol.MessageTypeStdinClose:
			// Only close stdin if it's not already closed
			if stdin != nil {
				// Close stdin to signal EOF to the process
				stdin.Close()
				// Set stdin to nil to indicate it's closed, but continue listening for cancel
				stdin = nil
				log.Printf("Stdin closed for %s, monitoring for cancellation", remoteAddr)
			}
		case protocol.MessageTypeCancel:
			// Client requested cancellation - actively cancel the command
			// Only process the first cancellation to avoid calling cmdCancel multiple times
			if !canceled.Swap(true) {
				log.Printf("Received cancellation request from client %s", remoteAddr)
				cmdCancel()
			}
			return
		default:
			log.Printf("Unexpected message type %d from %s for stdin handler", msg.Type, remoteAddr)
		}
	}
}

// forwardOutput streams data from a reader (stdout/stderr) to the client connection
// It handles context cancellation and properly reports any errors that occur during reading or writing
// When debug is enabled, it also logs the output to the server logs
func forwardOutput(ctx context.Context, src io.Reader, conn net.Conn, msgType uint8, debug bool, streamType string) {
	// Validate parameters
	if src == nil || conn == nil {
		log.Printf("Error: nil source reader or connection in forwardOutput")
		return
	}

	// Use buffer size optimized for streaming performance
	buffer := make([]byte, 4*1024)

	for {
		select {
		case <-ctx.Done():
			// Context was canceled, exit gracefully
			log.Printf("Stopping output forwarding due to context cancellation")
			return
		default:
			// Read data directly into the buffer
			n, err := src.Read(buffer)

			// If we read any data, send it immediately
			if n > 0 {
				// Create a copy of the buffer to avoid data races if the buffer is reused
				// before the data is sent through the connection
				data := make([]byte, n)
				copy(data, buffer[:n])

				// Log the output to server logs if debug is enabled
				if debug {
					log.Printf("[DEBUG] [%s] %s", streamType, string(data))
				}

				if err := protocol.WriteMessage(conn, msgType, data); err != nil {
					log.Printf("Error writing output message: %v", err)
					return
				}
			}

			// Check for errors or EOF
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading output: %v", err)
				}
				return
			}
		}
	}
}
