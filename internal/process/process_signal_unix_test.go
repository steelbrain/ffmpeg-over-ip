//go:build !windows

package process

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// TestSignalSIGUSR1 lives in a Unix-only file because syscall.SIGUSR1 is not
// defined on Windows (it would be a compile error). The rest of the package's
// signal tests use SIGTERM/SIGINT, which exist on Windows even though the
// suite as a whole skips there for shell-utility reasons (see TestMain).
func TestSignalSIGUSR1(t *testing.T) {
	proc := NewProcess("sleep", []string{"3600"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := proc.Signal(syscall.SIGUSR1); err != nil {
		t.Fatalf("Signal(SIGUSR1) returned error: %v", err)
	}

	proc.Terminate()
	proc.Wait()
}
