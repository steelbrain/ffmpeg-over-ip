package process

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// readAsync starts reading from r in a goroutine and returns a function
// that blocks until the read completes, returning the data and error.
// This avoids a race where cmd.Wait() closes pipes before ReadAll starts.
func readAsync(r io.Reader) func() ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(r)
		ch <- result{data, err}
	}()
	return func() ([]byte, error) {
		r := <-ch
		return r.data, r.err
	}
}

func TestStartAndWait(t *testing.T) {
	proc := NewProcess("echo", []string{"hello"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestStdoutPipe(t *testing.T) {
	proc := NewProcess("echo", []string{"hello world"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	getStdout := readAsync(proc.Stdout())

	out, err := getStdout()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello world\n" {
		t.Fatalf("stdout = %q, want %q", string(out), "hello world\n")
	}

	code, _ := proc.Wait()
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestStderrPipe(t *testing.T) {
	proc := NewProcess("sh", []string{"-c", "echo error >&2"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	getStderr := readAsync(proc.Stderr())

	out, err := getStderr()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "error\n" {
		t.Fatalf("stderr = %q, want %q", string(out), "error\n")
	}

	proc.Wait()
}

func TestStdinPipe(t *testing.T) {
	proc := NewProcess("cat", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	getStdout := readAsync(proc.Stdout())

	proc.Stdin().Write([]byte("input data"))
	proc.Stdin().Close()

	out, _ := getStdout()
	if string(out) != "input data" {
		t.Fatalf("stdout = %q, want %q", string(out), "input data")
	}

	code, _ := proc.Wait()
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestExitCodeNonZero(t *testing.T) {
	proc := NewProcess("sh", []string{"-c", "exit 42"})
	proc.Start(context.Background())

	code, _ := proc.Wait()
	if code != 42 {
		t.Fatalf("exit code = %d, want 42", code)
	}
}

func TestFFOIPPortSet(t *testing.T) {
	proc := NewProcess("sh", []string{"-c", "echo $FFOIP_PORT"})
	proc.Start(context.Background())

	getStdout := readAsync(proc.Stdout())

	out, _ := getStdout()
	port := string(bytes.TrimSpace(out))
	if port == "" || port == "0" {
		t.Fatal("FFOIP_PORT was not set or is zero")
	}

	proc.Wait()
}

func TestLoopbackNilWhenNoConnect(t *testing.T) {
	// echo doesn't connect to the loopback
	proc := NewProcess("echo", []string{"hi"})
	proc.Start(context.Background())

	conn := proc.Loopback()
	if conn != nil {
		t.Fatal("expected nil loopback when child doesn't connect")
		conn.Close()
	}

	proc.Wait()
}

func TestProcessNotFound(t *testing.T) {
	proc := NewProcess("/nonexistent/binary", nil)
	err := proc.Start(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
}

func TestTerminate(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	proc := NewProcess("sleep", []string{"3600"})
	proc.Start(context.Background())

	time.Sleep(100 * time.Millisecond)
	proc.Terminate()

	code, _ := proc.Wait()
	// SIGTERM = 15, exit code = 128+15 = 143
	if code != 143 {
		t.Errorf("exit code = %d, want 143 (128+SIGTERM)", code)
	}
}

func TestSignalNil(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	proc := NewProcess("sleep", []string{"3600"})
	proc.Start(context.Background())

	time.Sleep(100 * time.Millisecond)

	// Signal(nil) should return an error (os.Process.Signal(nil) errors on Unix)
	err := proc.Signal(nil)
	if err == nil {
		t.Error("Signal(nil) should return an error")
	}

	proc.Terminate()
	proc.Wait()
}

func TestMultipleStdout(t *testing.T) {
	proc := NewProcess("sh", []string{"-c", "for i in $(seq 1 50); do echo line$i; done"})
	proc.Start(context.Background())

	getStdout := readAsync(proc.Stdout())

	out, _ := getStdout()
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	if len(lines) != 50 {
		t.Fatalf("expected 50 lines, got %d", len(lines))
	}

	proc.Wait()
}

func TestStdoutAndStderr(t *testing.T) {
	proc := NewProcess("sh", []string{"-c", "echo out; echo err >&2"})
	proc.Start(context.Background())

	getStdout := readAsync(proc.Stdout())
	getStderr := readAsync(proc.Stderr())

	stdout, _ := getStdout()
	stderr, _ := getStderr()

	if string(bytes.TrimSpace(stdout)) != "out" {
		t.Fatalf("stdout = %q, want %q", string(stdout), "out\n")
	}
	if string(bytes.TrimSpace(stderr)) != "err" {
		t.Fatalf("stderr = %q, want %q", string(stderr), "err\n")
	}

	proc.Wait()
}

func TestCloseLoopbackBeforeStart(t *testing.T) {
	proc := NewProcess("echo", []string{"hello"})
	// CloseLoopback on a never-started process should not panic
	proc.CloseLoopback()
}

func TestCloseLoopbackAfterStart(t *testing.T) {
	proc := NewProcess("echo", []string{"hello"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// CloseLoopback after process has exited should not panic
	proc.CloseLoopback()
}

func TestTerminateBeforeStart(t *testing.T) {
	proc := NewProcess("echo", []string{"hello"})
	// Terminate on a never-started process (cmd is nil) should not panic
	proc.Terminate()
}

func TestTerminateAfterExit(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	proc := NewProcess("echo", []string{"hello"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// Terminate after the process has already exited should not panic
	proc.Terminate()
}

func TestSignalNotStarted(t *testing.T) {
	proc := NewProcess("echo", []string{"hello"})
	err := proc.Signal(syscall.SIGTERM)
	if err == nil {
		t.Fatal("expected error when signaling a process that was never started")
	}
}

func TestSignalSIGUSR1(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

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

func TestWaitMultipleCalls(t *testing.T) {
	proc := NewProcess("echo", []string{"hi"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	code1, err1 := proc.Wait()
	if err1 != nil {
		t.Fatalf("first Wait failed: %v", err1)
	}

	code2, err2 := proc.Wait()
	if err2 != nil {
		t.Fatalf("second Wait failed: %v", err2)
	}

	if code1 != code2 {
		t.Fatalf("Wait returned different exit codes: %d vs %d", code1, code2)
	}
	if code1 != 0 {
		t.Fatalf("exit code = %d, want 0", code1)
	}
}

func TestExitCode128PlusSignal(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	proc := NewProcess("sleep", []string{"3600"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Send SIGKILL directly via cmd.Process.Kill()
	if err := proc.cmd.Process.Kill(); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	code, _ := proc.Wait()
	// SIGKILL = 9, exit code should be 128+9 = 137
	if code != 137 {
		t.Fatalf("exit code = %d, want 137 (128+SIGKILL)", code)
	}
}

func TestExitCodeSignal(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	proc := NewProcess("sleep", []string{"3600"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	proc.Terminate()

	code, _ := proc.Wait()
	// SIGTERM = 15, exit code should be 128+15 = 143
	if code != 143 {
		t.Fatalf("exit code = %d, want 143 (128+SIGTERM)", code)
	}
}

func TestStdinCloseBeforeWrite(t *testing.T) {
	proc := NewProcess("cat", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	getStdout := readAsync(proc.Stdout())

	// Close stdin immediately without writing anything
	proc.Stdin().Close()

	out, err := getStdout()
	if err != nil {
		t.Fatalf("ReadAll stdout failed: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("stdout = %q, want empty", string(out))
	}

	code, _ := proc.Wait()
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestLargeStdinStdout(t *testing.T) {
	proc := NewProcess("cat", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Generate 100KB of data
	data := make([]byte, 100*1024)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}

	getStdout := readAsync(proc.Stdout())

	// Write stdin — this unblocks cat, which writes stdout, then exits
	if _, err := proc.Stdin().Write(data); err != nil {
		t.Fatalf("stdin write failed: %v", err)
	}
	proc.Stdin().Close()

	// Collect stdout
	out, err := getStdout()
	if err != nil {
		t.Fatalf("ReadAll stdout failed: %v", err)
	}

	if !bytes.Equal(out, data) {
		t.Fatalf("round-trip mismatch: wrote %d bytes, got %d bytes", len(data), len(out))
	}

	code, _ := proc.Wait()
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestFFOIPPortIsNumeric(t *testing.T) {
	proc := NewProcess("sh", []string{"-c", "echo $FFOIP_PORT"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	getStdout := readAsync(proc.Stdout())

	out, err := getStdout()
	if err != nil {
		t.Fatalf("ReadAll stdout failed: %v", err)
	}

	portStr := strings.TrimSpace(string(out))
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("FFOIP_PORT %q is not a valid integer: %v", portStr, err)
	}
	if port <= 0 || port >= 65536 {
		t.Fatalf("FFOIP_PORT = %d, want 1..65535", port)
	}

	proc.Wait()
}

func TestProcessEnvInherited(t *testing.T) {
	os.Setenv("MY_TEST_VAR", "ffoip_test_value")
	defer os.Unsetenv("MY_TEST_VAR")

	proc := NewProcess("sh", []string{"-c", "echo $MY_TEST_VAR"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	getStdout := readAsync(proc.Stdout())

	out, err := getStdout()
	if err != nil {
		t.Fatalf("ReadAll stdout failed: %v", err)
	}

	val := strings.TrimSpace(string(out))
	if val != "ffoip_test_value" {
		t.Fatalf("MY_TEST_VAR = %q, want %q", val, "ffoip_test_value")
	}

	code, _ := proc.Wait()
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestCloseLoopbackMultipleCalls(t *testing.T) {
	proc := NewProcess("echo", []string{"hello"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// Calling CloseLoopback multiple times should never panic
	proc.CloseLoopback()
	proc.CloseLoopback()
	proc.CloseLoopback()
}

func TestTerminateMultipleCalls(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	proc := NewProcess("sleep", []string{"3600"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Calling Terminate twice in a row should not panic
	proc.Terminate()
	proc.Terminate()

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	// Process should have exited (SIGTERM = 128+15 = 143)
	if code != 143 {
		t.Errorf("exit code = %d, want 143 (128+SIGTERM)", code)
	}
}

func TestSignalAfterExit(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("signal tests only on unix")
	}

	proc := NewProcess("echo", []string{"hello"})
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// Signaling an already-exited process should return an error
	err = proc.Signal(syscall.SIGTERM)
	if err == nil {
		t.Fatal("expected error when signaling an already-exited process")
	}
}

func TestProcessEmptyArgs(t *testing.T) {
	proc := NewProcess("echo", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	getStdout := readAsync(proc.Stdout())

	out, err := getStdout()
	if err != nil {
		t.Fatalf("ReadAll stdout failed: %v", err)
	}
	// "echo" with no args prints a single newline
	if string(out) != "\n" {
		t.Fatalf("stdout = %q, want %q", string(out), "\n")
	}

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestExitCode1(t *testing.T) {
	proc := NewProcess("false", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestExitCode0(t *testing.T) {
	proc := NewProcess("true", nil)
	if err := proc.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}
