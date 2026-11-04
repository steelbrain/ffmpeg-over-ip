//go:build linux

package process

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPdeathsigHelper is the "server" for TestChildDiesWithServer: it starts a
// child through Process and blocks until it is killed.
func TestPdeathsigHelper(t *testing.T) {
	if os.Getenv("FFOIP_PDEATHSIG_HELPER") != "1" {
		t.Skip("helper for TestChildDiesWithServer")
	}
	if os.Getenv("FFOIP_PDEATHSIG_MODE") == "tied" {
		// The client's fallback: StartTied with no exit channel.
		cmd := exec.Command("sleep", "3600")
		if err := StartTied(cmd, nil); err != nil {
			fmt.Println("error", err)
			os.Exit(1)
		}
		fmt.Println(cmd.Process.Pid)
		select {}
	}
	p := NewProcess("sleep", []string{"3600"})
	if err := p.Start(context.Background()); err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
	fmt.Println(p.cmd.Process.Pid)
	select {}
}

// processGone reports whether pid has exited. A killed child whose parent is
// gone may linger as a zombie until something reaps it; that counts as gone.
func processGone(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
	return len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X")
}

// If the server dies without stopping its children (SIGKILL, OOM killer,
// crash), the kernel must kill ffmpeg too instead of leaving it orphaned.
func TestChildDiesWithServer(t *testing.T) {
	testChildDiesWithParent(t, "process")
}

// The same for the client's local fallback ffmpeg (StartTied).
func TestTiedChildDiesWithParent(t *testing.T) {
	testChildDiesWithParent(t, "tied")
}

func testChildDiesWithParent(t *testing.T, mode string) {
	server := exec.Command(os.Args[0], "-test.run=^TestPdeathsigHelper$")
	server.Env = append(os.Environ(), "FFOIP_PDEATHSIG_HELPER=1", "FFOIP_PDEATHSIG_MODE="+mode)
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
	if processGone(pid) {
		t.Fatalf("child %d not running before the server was killed", pid)
	}

	server.Process.Kill()
	server.Wait()

	deadline := time.Now().Add(3 * time.Second)
	for !processGone(pid) {
		if time.Now().After(deadline) {
			syscall_kill(pid)
			t.Fatalf("child %d still running after its server was killed", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func syscall_kill(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}
