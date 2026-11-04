//go:build windows

package process

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestJobHelper is the "server" for TestChildDiesWithServer: it starts a
// child through Process and blocks until it is killed.
func TestJobHelper(t *testing.T) {
	if os.Getenv("FFOIP_JOB_HELPER") != "1" {
		t.Skip("helper for TestChildDiesWithServer")
	}
	p := NewProcess(os.Args[0], []string{"-test.run=^TestJobSleeper$"})
	if err := p.Start(context.Background()); err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
	fmt.Println(p.cmd.Process.Pid)
	select {}
}

// TestJobSleeper stands in for ffmpeg.
func TestJobSleeper(t *testing.T) {
	if os.Getenv("FFOIP_JOB_SLEEPER") != "1" {
		t.Skip("child for TestChildDiesWithServer")
	}
	time.Sleep(time.Hour)
}

// If the server dies without stopping its children (crash, taskkill /F),
// Windows must terminate ffmpeg too instead of leaving it running.
func TestChildDiesWithServer(t *testing.T) {
	server := exec.Command(os.Args[0], "-test.run=^TestJobHelper$")
	server.Env = append(os.Environ(), "FFOIP_JOB_HELPER=1", "FFOIP_JOB_SLEEPER=1")
	out, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		server.Process.Kill()
		t.Fatalf("reading child pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		server.Process.Kill()
		t.Fatalf("helper said %q", line)
	}

	child, err := syscall.OpenProcess(syscall.SYNCHRONIZE|processTerminate, false, uint32(pid))
	if err != nil {
		server.Process.Kill()
		t.Fatalf("OpenProcess(%d): %v", pid, err)
	}
	defer syscall.CloseHandle(child)
	if ev, _ := syscall.WaitForSingleObject(child, 0); ev == syscall.WAIT_OBJECT_0 {
		server.Process.Kill()
		t.Fatalf("child %d not running before the server was killed", pid)
	}

	server.Process.Kill() // TerminateProcess: no cleanup runs in the helper
	server.Wait()

	if ev, _ := syscall.WaitForSingleObject(child, 5000); ev != syscall.WAIT_OBJECT_0 {
		syscall.TerminateProcess(child, 1)
		t.Fatalf("child %d still running after its server was killed", pid)
	}
}
