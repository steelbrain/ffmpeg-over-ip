// fio-harness launches a C binary through the process manager and handles
// file I/O requests using the filehandler. Used for integration testing.
//
// Usage: fio-harness <c-binary> [args...]
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/steelbrain/ffmpeg-over-ip/internal/filehandler"
	"github.com/steelbrain/ffmpeg-over-ip/internal/process"
	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: fio-harness <binary> [args...]\n")
		os.Exit(1)
	}

	binary := os.Args[1]
	args := os.Args[2:]

	proc := process.NewProcess(binary, args)
	if err := proc.Start(context.Background()); err != nil {
		log.Fatalf("failed to start process: %v", err)
	}

	// Pipe stdout/stderr to terminal
	go io.Copy(os.Stdout, proc.Stdout())
	go io.Copy(os.Stderr, proc.Stderr())

	// Wait for loopback connection from fio
	loopback := proc.Loopback()
	if loopback != nil {
		defer loopback.Close()

		handler := filehandler.NewHandler()
		defer handler.CloseAll()

		// Read fio requests from loopback, dispatch to handler, write responses back
		for {
			msg, err := protocol.ReadMessageFrom(loopback)
			if err != nil {
				if err == io.EOF {
					break
				}
				break
			}

			if protocol.IsFileIORequest(msg.Type) {
				respType, respPayload, err := handler.HandleMessage(msg.Type, msg.Payload)
				if err != nil {
					log.Printf("handler error for type 0x%02x: %v", msg.Type, err)
					continue
				}
				if err := protocol.WriteMessageTo(loopback, respType, respPayload); err != nil {
					break
				}
			}
		}
	}

	exitCode, _ := proc.Wait()
	os.Exit(exitCode)
}
