//go:build unix

package process

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// Accepting the loopback can fail once (EMFILE) without losing it: giving up
// left the listener open, so fio's later connect and hello were
// queued by the kernel and never accepted, and its requests hung.
func TestLoopbackSurvivesAcceptError(t *testing.T) {
	proc, port, token := startTokenChild(t)

	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &old); err != nil {
		t.Fatal(err)
	}
	lim := old
	lim.Cur = 256
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		t.Fatal(err)
	}
	var fillers []*os.File
	for {
		f, err := os.Open(os.DevNull)
		if err != nil {
			break
		}
		fillers = append(fillers, f)
	}
	// Free one descriptor for our own dial; the accept then hits EMFILE.
	fillers[len(fillers)-1].Close()
	fillers = fillers[:len(fillers)-1]
	c1 := dialLoopback(t, port)
	time.Sleep(200 * time.Millisecond)
	for _, f := range fillers {
		f.Close()
	}
	syscall.Setrlimit(syscall.RLIMIT_NOFILE, &old)
	c1.Close()

	// Now fio connects properly.
	c2 := dialLoopback(t, port)
	if _, err := c2.Write(LoopbackHello(token)); err != nil {
		t.Fatal(err)
	}
	got := make(chan bool, 1)
	go func() { got <- proc.Loopback() != nil }()
	select {
	case ok := <-got:
		if !ok {
			t.Fatalf("Loopback() returned nil while the child is alive and fio connected with the right token (listener still open: dial succeeded)")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Loopback blocked")
	}
}
