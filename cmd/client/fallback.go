package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
	"github.com/steelbrain/ffmpeg-over-ip/internal/rewrite"
)

// All fallback diagnostics go through the log package so they honor the
// configured log destination. We act as an invisible proxy for ffmpeg —
// writing our own messages to stdout/stderr would corrupt the output that
// consumers (Jellyfin, etc.) parse from the child process.

// fallbackDeps holds the pluggable side-effects of runLocalFallback so the
// orchestrator can be unit-tested without touching the real PATH or exec'ing
// a real binary.
type fallbackDeps struct {
	lookup func(name string) (string, error)
	exec   func(binPath string, args []string) int
}

// runLocalFallback resolves the local ffmpeg/ffprobe via deps.lookup, applies
// fallbackRewrites, and runs it via deps.exec. Returns the exit code to
// propagate. Pure — all side effects live in deps.
//
// When debug is true, original and rewritten args are logged (mirrors the
// server's cfg.Debug behavior — useful when fallbackRewrites mangle argv and
// the local ffmpeg fails).
func runLocalFallback(deps fallbackDeps, program uint8, args []string, fallbackRewrites [][2]string, debug bool, dialErr error, addr string) int {
	binName := "ffmpeg"
	if program == protocol.ProgramFFprobe {
		binName = "ffprobe"
	}

	binPath, err := deps.lookup(binName)
	if err != nil {
		log.Printf("fallback: cannot connect to %s (%v) and no local %s on PATH: %v",
			addr, dialErr, binName, err)
		return 1
	}

	log.Printf("server unreachable (%v), falling back to local %s", dialErr, binPath)
	rewritten := rewrite.Apply(args, fallbackRewrites)
	if debug {
		log.Printf("[debug] original args: %v", args)
		log.Printf("[debug] rewritten args: %v", rewritten)
	}
	return deps.exec(binPath, rewritten)
}

// realFallbackDeps wires runLocalFallback up to the real environment: PATH
// lookup via findFallbackBinary, exec via execChild. Returns an error if we
// can't even resolve our own executable (we can't safely do PATH lookup
// without the self-skip reference).
func realFallbackDeps() (fallbackDeps, error) {
	self, err := resolveSelf()
	if err != nil {
		return fallbackDeps{}, fmt.Errorf("resolve own executable: %w", err)
	}
	return fallbackDeps{
		lookup: func(name string) (string, error) {
			return findFallbackBinary(name, os.Getenv("PATH"), self, fallbackExtensions())
		},
		exec: execChild,
	}, nil
}

// findFallbackBinary searches `pathEnv` (a PATH-style list, separator = OS)
// for an executable named `name`, skipping any candidate whose resolved path
// matches `selfResolved`. The self-skip prevents the client from re-execing
// itself when it's installed on PATH under the name "ffmpeg" / "ffprobe".
//
// On Unix, `extensions` is `[""]`. On Windows it's `[""]` followed by entries
// of %PATHEXT% so both bare names (MSYS-style shims) and `.exe` candidates are
// tried.
//
// Literal "" and "." PATH entries are skipped (POSIX-empty-means-cwd is a
// foot-gun that almost no one means literally). Other spellings of cwd are
// honored — we trust the user's PATH and rely on the self-skip as our only
// hard guarantee.
func findFallbackBinary(name, pathEnv, selfResolved string, extensions []string) (string, error) {
	if pathEnv == "" {
		return "", fmt.Errorf("PATH is empty")
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || dir == "." {
			continue
		}
		for _, ext := range extensions {
			cand := filepath.Join(dir, name+ext)
			info, err := os.Stat(cand)
			if err != nil || info.IsDir() {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
				continue
			}
			candResolved, err := filepath.EvalSymlinks(cand)
			if err != nil {
				candResolved = cand
			}
			if pathsEqual(candResolved, selfResolved) {
				continue
			}
			return cand, nil
		}
	}
	return "", fmt.Errorf("no %q found in PATH (excluding self)", name)
}

// pathsEqual compares two paths case-insensitively on Windows, exact elsewhere.
func pathsEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// resolveSelf returns the resolved (symlinks-followed) path of the running
// executable, used to exclude ourselves from PATH lookups.
func resolveSelf() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// fallbackExtensions returns the list of executable extensions to try when
// looking up a binary on the current OS's PATH. The bare name (no extension)
// is always tried first so MSYS / Git-Bash shims named simply "ffmpeg" still
// resolve on Windows. PATHEXT entries are lowercased so resolved candidate
// paths read as `ffmpeg.exe` instead of `ffmpeg.EXE` in logs.
func fallbackExtensions() []string {
	if runtime.GOOS != "windows" {
		return []string{""}
	}
	pathext := os.Getenv("PATHEXT")
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	exts := []string{""}
	for _, e := range strings.Split(pathext, ";") {
		exts = append(exts, strings.ToLower(e))
	}
	return exts
}

// execChild runs binPath with args as a child process, wiring stdio to the
// parent and forwarding SIGINT/SIGTERM to the child so ffmpeg can shut down
// cleanly (write trailers, etc.) on Ctrl-C. Returns the child's exit code.
//
// FFMPEG_OVER_IP_* env vars are stripped from the child's environment so the
// auth secret doesn't leak into the local ffmpeg's /proc/<pid>/environ.
func execChild(binPath string, args []string) int {
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = scrubFFmpegOverIPEnv(os.Environ())

	// Signal-forwarding ordering matters here:
	//   1. signal.Notify BEFORE cmd.Start — a Ctrl-C during process startup
	//      would otherwise hit the default handler and kill the parent,
	//      orphaning the child once it spawns.
	//   2. close(done) BEFORE the deferred signal.Stop runs — the goroutine
	//      exits via the done branch, then signal.Stop tears down the
	//      registration. (signal.Stop here is the deferred call below.)
	// A signal arriving between cmd.Wait returning and close(done) may be
	// forwarded to an already-reaped PID; Process.Signal returns ESRCH and
	// we discard it (the swallowed error is intentional).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if err := cmd.Start(); err != nil {
		log.Printf("fallback: failed to start %s: %v", binPath, err)
		return 1
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigCh:
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()

	err := cmd.Wait()
	close(done)
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			log.Printf("fallback: %v", err)
		}
		return exitCodeFromError(err)
	}
	return 0
}

// scrubFFmpegOverIPEnv returns env with FFMPEG_OVER_IP_* entries removed so
// the auth secret and other config don't leak into the child ffmpeg process.
func scrubFFmpegOverIPEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "FFMPEG_OVER_IP_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
