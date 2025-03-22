package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/steelbrain/ffmpeg-over-ip/pkg/common"
	"github.com/steelbrain/ffmpeg-over-ip/pkg/config"
	"github.com/steelbrain/ffmpeg-over-ip/pkg/protocol"
)

var (
	_ = flag.String("config", "", "Path to config file")
	_ = flag.Bool("debug-print-search-paths", false, "Print config search paths and exit")
)

func main() {
	// Custom flag parsing to avoid interference with ffmpeg args
	originalArgs := os.Args[1:]

	// Manually extract our flags
	var configPathValue string
	var debugSearchPathsValue bool
	ffmpegArgs := []string{}

	for i := 0; i < len(originalArgs); i++ {
		arg := originalArgs[i]

		if arg == "--config" || arg == "-config" {
			if i+1 < len(originalArgs) {
				configPathValue = originalArgs[i+1]
				i++ // Skip the next argument as it's the value
			}
		} else if strings.HasPrefix(arg, "--config=") {
			configPathValue = strings.TrimPrefix(arg, "--config=")
		} else if arg == "--debug-print-search-paths" || arg == "-debug-print-search-paths" {
			debugSearchPathsValue = true
		} else {
			// All other arguments are considered ffmpeg args
			ffmpegArgs = append(ffmpegArgs, arg)
		}
	}

	// Get config paths
	configPaths := config.GetClientConfigPaths()
	if configPathValue != "" {
		configPaths = []string{configPathValue}
	}

	// Debug print config paths if requested
	if debugSearchPathsValue {
		common.PrintConfigPaths(configPaths)
		return
	}

	// Check if we have ffmpeg args
	if len(ffmpegArgs) == 0 {
		fmt.Println("Usage: ffmpeg-over-ip-client [options] [ffmpeg args...]")
		fmt.Println("Options:")
		fmt.Println("  --config <path>                Path to config file")
		fmt.Println("  --debug-print-search-paths    Print config search paths and exit")
		fmt.Println("\nAll arguments after options are passed to ffmpeg.")
		return
	}

	// Load config
	cfg, configPath, err := config.LoadClientConfig(configPaths)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup logger
	logStr, err := config.ParseLogConfig(cfg.Log)
	if err != nil {
		log.Fatalf("Failed to parse log config: %v", err)
	}

	logDest, err := common.SetupLogger(logStr)
	if err != nil {
		log.Fatalf("Failed to setup logger: %v", err)
	}
	if logDest != nil && logDest != os.Stdout && logDest != os.Stderr {
		defer logDest.Close()
		log.SetOutput(logDest)
	}

	log.Printf("Loaded config from: %s", configPath)

	// Parse the address
	connInfo, err := protocol.ParseAddress(cfg.Address)
	if err != nil {
		log.Fatalf("Invalid address: %v", err)
	}

	// Create a context that can be canceled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to the server with timeout
	log.Printf("Connecting to server at %s (%s)...", cfg.Address, connInfo.Network)
	conn, err := protocol.ConnectWithTimeout(connInfo.Network, connInfo.Address, 5*time.Second)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Handle signals for graceful shutdown now that we have a connection
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		fmt.Println("Received interrupt signal, canceling...")
		// Send cancel message immediately to the server before calling cancel()
		// This ensures the server gets notified even if we haven't started reading messages yet
		protocol.WriteMessage(conn, protocol.MessageTypeCancel, nil)
		cancel()

		// Add a second signal handler to force exit if user presses Ctrl+C twice
		secondSignal := make(chan os.Signal, 1)
		signal.Notify(secondSignal, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-secondSignal
			fmt.Println("Received second interrupt signal, forcing exit...")
			os.Exit(130) // 130 is the standard exit code for SIGINT
		}()
	}()

	log.Printf("Connected to server, sending command: %v", ffmpegArgs)

	// Send the command
	if err := protocol.WriteCommandMessage(conn, cfg.AuthSecret, ffmpegArgs); err != nil {
		log.Fatalf("Failed to send command: %v", err)
	}

	// Setup a waitgroup to coordinate goroutines
	var wg sync.WaitGroup

	// Forward stdin to the server
	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardStdin(ctx, conn)
	}()

	// Process messages from the server
	exitCode, err := processServerMessages(ctx, conn)
	if err != nil {
		log.Printf("Error processing server messages: %v", err)
		os.Exit(1)
	}

	// Wait for stdin goroutine to finish
	wg.Wait()

	// Exit with the same code as the remote command
	os.Exit(exitCode)
}

// forwardStdin reads data from standard input and forwards it to the server connection
// It detects whether stdin is a pipe or terminal and handles context cancellation
func forwardStdin(ctx context.Context, conn net.Conn) {
	if conn == nil {
		log.Printf("Error: nil connection in forwardStdin")
		return
	}

	if ctx == nil {
		log.Printf("Error: nil context in forwardStdin")
		return
	}

	// Check if stdin is a pipe or terminal
	stat, err := os.Stdin.Stat()
	if err != nil {
		log.Printf("Error checking stdin: %v", err)
		// Try to notify the server we're not sending any stdin
		if err := protocol.WriteMessage(conn, protocol.MessageTypeStdinClose, nil); err != nil {
			log.Printf("Failed to send stdin close message: %v", err)
		}
		return
	}

	// If stdin is a terminal (not a pipe), don't forward anything
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		log.Printf("Stdin is a terminal, not forwarding")
		// Notify the server we're not sending any stdin
		if err := protocol.WriteMessage(conn, protocol.MessageTypeStdinClose, nil); err != nil {
			log.Printf("Failed to send stdin close message: %v", err)
		}
		return
	}

	log.Printf("Forwarding stdin to server")

	// Create a buffer for reading stdin - 4KB is a good balance for efficiency
	buffer := make([]byte, 4*1024)

	for {
		select {
		case <-ctx.Done():
			// Context canceled, stop reading
			log.Printf("Context canceled, stopping stdin forwarding")
			// Attempt to tell the server we're closing stdin
			_ = protocol.WriteMessage(conn, protocol.MessageTypeStdinClose, nil)
			return
		default:
			// Read from stdin
			n, err := os.Stdin.Read(buffer)
			if n > 0 {
				// Make a copy of the buffer to avoid potential data races
				data := make([]byte, n)
				copy(data, buffer[:n])

				// Send the data to the server
				if err := protocol.WriteMessage(conn, protocol.MessageTypeStdin, data); err != nil {
					log.Printf("Error sending stdin data: %v", err)
					return
				}
			}

			// Handle any read errors
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading from stdin: %v", err)
				} else {
					log.Printf("Reached end of stdin")
				}

				// Notify the server we're done with stdin
				if err := protocol.WriteMessage(conn, protocol.MessageTypeStdinClose, nil); err != nil {
					log.Printf("Failed to send stdin close message: %v", err)
				}
				return
			}
		}
	}
}

// processServerMessages handles incoming messages from the server
// It processes stdout/stderr output and captures the final exit code
// Returns the exit code and any error that occurred during processing
func processServerMessages(ctx context.Context, conn net.Conn) (int, error) {
	if conn == nil {
		return 1, fmt.Errorf("nil connection")
	}

	if ctx == nil {
		return 1, fmt.Errorf("nil context")
	}

	log.Printf("Processing server messages")

	for {
		select {
		case <-ctx.Done():
			// Context canceled, stop processing
			log.Printf("Context canceled, sending cancellation message to server")
			// Send cancellation message but ignore errors since we're already canceling
			_ = protocol.WriteMessage(conn, protocol.MessageTypeCancel, nil)
			return 1, fmt.Errorf("operation canceled by user or timeout")
		default:
			// Read a message from the server
			msg, err := protocol.ReadMessage(conn)
			if err != nil {
				if err == io.EOF {
					// Connection closed by server without sending exit code
					log.Printf("Connection closed by server without exit code")
					return 1, fmt.Errorf("connection closed by server unexpectedly")
				}
				return 1, fmt.Errorf("error reading server message: %w", err)
			}

			// Process the message based on its type
			switch msg.Type {
			case protocol.MessageTypeStdout:
				// Write to stdout
				if _, err := os.Stdout.Write(msg.Payload); err != nil {
					log.Printf("Failed to write to stdout: %v", err)
					return 1, fmt.Errorf("error writing to stdout: %w", err)
				}
			case protocol.MessageTypeStderr:
				// Write to stderr
				if _, err := os.Stderr.Write(msg.Payload); err != nil {
					log.Printf("Failed to write to stderr: %v", err)
					return 1, fmt.Errorf("error writing to stderr: %w", err)
				}
			case protocol.MessageTypeExitCode:
				// Return the exit code
				if len(msg.Payload) > 0 {
					exitCode := int(msg.Payload[0])
					log.Printf("Received exit code %d from server", exitCode)
					return exitCode, nil
				}
				return 0, nil
			case protocol.MessageTypeError:
				// Server reported an error
				return 1, fmt.Errorf("server error: %s", string(msg.Payload))
			default:
				log.Printf("Received unknown message type: %d", msg.Type)
			}
		}
	}
}
