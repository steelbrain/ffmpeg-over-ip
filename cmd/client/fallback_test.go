package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/steelbrain/ffmpeg-over-ip/internal/protocol"
)

// TestHelperProcess is the canonical Go-stdlib pattern for testing os/exec
// without depending on system commands. It only runs when invoked by another
// test via GO_WANT_HELPER_PROCESS=1; otherwise it's a no-op.
//
// Subcommands (passed after "--" in argv):
//   exit <N>            : exit with code N
//   check-env-stripped  : exit 0 if no FFMPEG_OVER_IP_* env var is set, else 99
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "helper: no subcommand")
		os.Exit(127)
	}
	switch args[0] {
	case "exit":
		if len(args) < 2 {
			os.Exit(127)
		}
		n, err := strconv.Atoi(args[1])
		if err != nil {
			os.Exit(127)
		}
		os.Exit(n)
	case "check-env-stripped":
		for _, kv := range os.Environ() {
			if strings.HasPrefix(kv, "FFMPEG_OVER_IP_") {
				fmt.Fprintf(os.Stderr, "helper: leaked env: %s\n", kv)
				os.Exit(99)
			}
		}
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "helper: unknown subcommand %q\n", args[0])
		os.Exit(127)
	}
}

// helperArgs returns the argv slice that will reinvoke this test binary as a
// helper process running TestHelperProcess with the given subcommand args.
//
// The caller MUST exec os.Args[0] (the running test binary) for this to work
// — the helper test lives in this same binary. `go test -c` followed by
// invoking the resulting binary from a different cwd is fine; what won't
// work is exec'ing some other binary and hoping it has TestHelperProcess.
func helperArgs(sub ...string) []string {
	return append([]string{"-test.run=^TestHelperProcess$", "--"}, sub...)
}

// withHelperEnv wraps a test that exec's the test binary as a child. It
// ensures GO_WANT_HELPER_PROCESS=1 is in the child's env (via t.Setenv on
// the parent, which execChild's scrubbing won't strip — it's not prefixed
// FFMPEG_OVER_IP_).
func withHelperEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
}

// makeExe writes a stub executable file at path. On Unix it's marked +x.
// Contents are arbitrary — these tests never execute the file, they only
// verify that findFallbackBinary picks the right path.
func makeExe(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestFindFallbackBinaryReturnsFirstMatch(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	first := filepath.Join(dir1, "ffmpeg")
	second := filepath.Join(dir2, "ffmpeg")
	makeExe(t, first)
	makeExe(t, second)

	pathEnv := dir1 + string(os.PathListSeparator) + dir2
	got, err := findFallbackBinary("ffmpeg", pathEnv, "/some/other/path", []string{""})
	if err != nil {
		t.Fatalf("findFallbackBinary: %v", err)
	}
	if got != first {
		t.Errorf("got %q, want %q (first PATH entry should win)", got, first)
	}
}

func TestFindFallbackBinarySkipsSelf(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	self := filepath.Join(dir1, "ffmpeg")
	other := filepath.Join(dir2, "ffmpeg")
	makeExe(t, self)
	makeExe(t, other)

	// Resolve self the same way the production code will.
	selfResolved, err := filepath.EvalSymlinks(self)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	pathEnv := dir1 + string(os.PathListSeparator) + dir2
	got, err := findFallbackBinary("ffmpeg", pathEnv, selfResolved, []string{""})
	if err != nil {
		t.Fatalf("findFallbackBinary: %v", err)
	}
	if got != other {
		t.Errorf("got %q, want %q (should have skipped self)", got, other)
	}
}

func TestFindFallbackBinarySkipsSelfWhenClientAndRealBinaryShareDir(t *testing.T) {
	// Docker-style case: the client is bind-mounted into the same directory
	// as the real ffmpeg, with two different basenames. PATH contains that
	// dir, and the lookup must skip the client and pick the real binary.
	if runtime.GOOS == "windows" {
		t.Skip("symlink-style self-equivalence test uses Unix layout")
	}
	dir := t.TempDir()

	// Pretend the client is installed here under its own name.
	clientBin := filepath.Join(dir, "ffmpeg-over-ip-client")
	makeExe(t, clientBin)
	// And exposed as `ffmpeg` via a symlink in the same dir — exact pattern
	// from docs/configuration.md ("ln -s ffmpeg-over-ip-client ffmpeg").
	clientAsFfmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.Symlink(clientBin, clientAsFfmpeg); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Resolve the client to what the production code will compare against.
	selfResolved, err := filepath.EvalSymlinks(clientBin)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	// With only this dir on PATH, the only candidate is the symlink to self
	// — should be skipped, leaving no match.
	if got, err := findFallbackBinary("ffmpeg", dir, selfResolved, []string{""}); err == nil {
		t.Errorf("got %q, want error (only candidate is symlink to self)", got)
	}

	// Now drop a real ffmpeg in the same dir under a non-conflicting name
	// (simulating ffmpeg being a separate file alongside the client).
	realFfmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	makeExe(t, realFfmpeg)
	pathEnv := dir + string(os.PathListSeparator) + filepath.Dir(realFfmpeg)
	got, err := findFallbackBinary("ffmpeg", pathEnv, selfResolved, []string{""})
	if err != nil {
		t.Fatalf("findFallbackBinary: %v", err)
	}
	if got != realFfmpeg {
		t.Errorf("got %q, want %q (should skip the symlink-to-self in dir1, pick real binary in dir2)", got, realFfmpeg)
	}
}

func TestFindFallbackBinarySkipsSelfThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires admin on Windows")
	}
	realDir := t.TempDir()
	linkDir := t.TempDir()

	realBin := filepath.Join(realDir, "ffmpeg-over-ip-client")
	makeExe(t, realBin)

	// Symlink it as "ffmpeg" in another PATH dir.
	linkBin := filepath.Join(linkDir, "ffmpeg")
	if err := os.Symlink(realBin, linkBin); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	selfResolved, err := filepath.EvalSymlinks(realBin)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	// Only the symlinked dir is on PATH — the symlink resolves to self, so we
	// should find nothing (not the symlink itself).
	pathEnv := linkDir
	if got, err := findFallbackBinary("ffmpeg", pathEnv, selfResolved, []string{""}); err == nil {
		t.Errorf("got %q, want error (symlink to self should be skipped)", got)
	}
}

func TestFindFallbackBinarySkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	// Create a subdirectory literally named "ffmpeg".
	if err := os.Mkdir(filepath.Join(dir, "ffmpeg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := findFallbackBinary("ffmpeg", dir, "/elsewhere", []string{""}); err == nil {
		t.Error("expected error when only candidate is a directory")
	}
}

func TestFindFallbackBinarySkipsNonExecutableOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores Unix +x bits")
	}
	dir := t.TempDir()
	notExec := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(notExec, []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := findFallbackBinary("ffmpeg", dir, "/elsewhere", []string{""}); err == nil {
		t.Error("expected error when only candidate is non-executable")
	}
}

func TestFindFallbackBinaryEmptyPath(t *testing.T) {
	if _, err := findFallbackBinary("ffmpeg", "", "/elsewhere", []string{""}); err == nil {
		t.Error("expected error for empty PATH")
	}
}

func TestFindFallbackBinaryNotFound(t *testing.T) {
	dir := t.TempDir() // empty
	if _, err := findFallbackBinary("ffmpeg", dir, "/elsewhere", []string{""}); err == nil {
		t.Error("expected error when no candidate exists")
	}
}

func TestFindFallbackBinarySkipsEmptyAndDotPathEntries(t *testing.T) {
	// Drop a binary in cwd, then put empty + "." + a real dir on PATH.
	// Empty and "." both mean cwd per POSIX and must be skipped.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(cwd)

	// Binary in cwd that should NOT be picked up.
	makeExe(t, filepath.Join(tmp, "ffmpeg"))

	// Real binary in a separate dir.
	realDir := t.TempDir()
	realBin := filepath.Join(realDir, "ffmpeg")
	makeExe(t, realBin)

	pathEnv := strings.Join([]string{"", ".", realDir}, string(os.PathListSeparator))
	got, err := findFallbackBinary("ffmpeg", pathEnv, "/elsewhere", []string{""})
	if err != nil {
		t.Fatalf("findFallbackBinary: %v", err)
	}
	if got != realBin {
		t.Errorf("got %q, want %q (empty and \".\" PATH entries must be skipped)", got, realBin)
	}
}

func TestFallbackExtensionsUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific")
	}
	got := fallbackExtensions()
	if len(got) != 1 || got[0] != "" {
		t.Errorf("got %v, want [\"\"]", got)
	}
}

func TestFallbackExtensionsWindowsIncludesBareName(t *testing.T) {
	// Test the function regardless of actual OS by inspecting its output:
	// on Windows the first entry must be "" (bare name) so MSYS shims work.
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific")
	}
	got := fallbackExtensions()
	if len(got) == 0 || got[0] != "" {
		t.Errorf("got %v, want first entry \"\"", got)
	}
}

func TestScrubFFmpegOverIPEnv(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=hunter2",
		"FFMPEG_OVER_IP_CLIENT_ADDRESS=10.0.0.1:5050",
		"HOME=/home/user",
		"FFMPEG_OVER_IP_CLIENT_FALLBACK_TO_LOCAL=true",
		// Edge: literal prefix only (no suffix), and a stripped key with empty value.
		// Both should still be removed since they start with FFMPEG_OVER_IP_.
		"FFMPEG_OVER_IP_=stripped",
		"FFMPEG_OVER_IP_X=",
		// Edge: keys that merely contain the prefix in the middle/end must NOT be stripped.
		"X_FFMPEG_OVER_IP_Y=keep-me",
		"MY_FFMPEG_OVER_IP=keep-me-too",
	}
	got := scrubFFmpegOverIPEnv(in)
	want := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"X_FFMPEG_OVER_IP_Y=keep-me",
		"MY_FFMPEG_OVER_IP=keep-me-too",
	}

	if len(got) != len(want) {
		t.Fatalf("got %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for _, kv := range got {
		if strings.HasPrefix(kv, "FFMPEG_OVER_IP_") {
			t.Errorf("FFMPEG_OVER_IP_* leaked into scrubbed env: %q", kv)
		}
	}
}

func TestRunLocalFallbackPicksFFmpegForFFmpegProgram(t *testing.T) {
	var lookedUp string
	var execedBin string
	var execedArgs []string

	deps := fallbackDeps{
		lookup: func(name string) (string, error) {
			lookedUp = name
			return "/fake/ffmpeg", nil
		},
		exec: func(binPath string, args []string) int {
			execedBin = binPath
			execedArgs = args
			return 0
		},
	}

	code := runLocalFallback(deps, protocol.ProgramFFmpeg, []string{"-i", "in.mp4"}, nil, false, errors.New("boom"), "1.2.3.4:5050")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if lookedUp != "ffmpeg" {
		t.Errorf("looked up %q, want %q", lookedUp, "ffmpeg")
	}
	if execedBin != "/fake/ffmpeg" {
		t.Errorf("execed %q, want %q", execedBin, "/fake/ffmpeg")
	}
	if len(execedArgs) != 2 || execedArgs[0] != "-i" || execedArgs[1] != "in.mp4" {
		t.Errorf("execed args = %v, want [-i in.mp4]", execedArgs)
	}
}

func TestRunLocalFallbackPicksFFprobeForFFprobeProgram(t *testing.T) {
	var lookedUp string
	deps := fallbackDeps{
		lookup: func(name string) (string, error) {
			lookedUp = name
			return "/fake/ffprobe", nil
		},
		exec: func(string, []string) int { return 0 },
	}

	runLocalFallback(deps, protocol.ProgramFFprobe, []string{"input.mp4"}, nil, false, errors.New("boom"), "1.2.3.4:5050")
	if lookedUp != "ffprobe" {
		t.Errorf("looked up %q, want %q", lookedUp, "ffprobe")
	}
}

func TestRunLocalFallbackAppliesRewritesBeforeExec(t *testing.T) {
	var execedArgs []string
	deps := fallbackDeps{
		lookup: func(string) (string, error) { return "/fake/ffmpeg", nil },
		exec: func(_ string, args []string) int {
			execedArgs = args
			return 0
		},
	}

	rewrites := [][2]string{
		{"h264_nvenc", "h264_qsv"},
		{"hevc_nvenc", "hevc_qsv"},
	}
	args := []string{"-c:v", "h264_nvenc", "-c:a", "aac", "-c:v", "hevc_nvenc"}
	want := []string{"-c:v", "h264_qsv", "-c:a", "aac", "-c:v", "hevc_qsv"}

	runLocalFallback(deps, protocol.ProgramFFmpeg, args, rewrites, false, errors.New("boom"), "addr")

	if len(execedArgs) != len(want) {
		t.Fatalf("execed args length = %d, want %d", len(execedArgs), len(want))
	}
	for i := range want {
		if execedArgs[i] != want[i] {
			t.Errorf("execed[%d] = %q, want %q", i, execedArgs[i], want[i])
		}
	}
}

func TestRunLocalFallbackReturns1AndDoesNotExecOnLookupFailure(t *testing.T) {
	execed := false
	deps := fallbackDeps{
		lookup: func(string) (string, error) {
			return "", errors.New("nothing on PATH")
		},
		exec: func(string, []string) int {
			execed = true
			return 0
		},
	}

	code := runLocalFallback(deps, protocol.ProgramFFmpeg, []string{"-version"}, nil, false, errors.New("dial failed"), "addr")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if execed {
		t.Error("exec was called despite lookup failure")
	}
}

func TestRunLocalFallbackDebugLogsOriginalAndRewritten(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	deps := fallbackDeps{
		lookup: func(string) (string, error) { return "/fake/ffmpeg", nil },
		exec:   func(string, []string) int { return 0 },
	}
	rewrites := [][2]string{{"h264_nvenc", "h264_qsv"}}
	args := []string{"-c:v", "h264_nvenc"}

	runLocalFallback(deps, protocol.ProgramFFmpeg, args, rewrites, true, errors.New("boom"), "addr")

	out := buf.String()
	if !strings.Contains(out, "[debug] original args:") || !strings.Contains(out, "h264_nvenc") {
		t.Errorf("missing original-args debug line in: %s", out)
	}
	if !strings.Contains(out, "[debug] rewritten args:") || !strings.Contains(out, "h264_qsv") {
		t.Errorf("missing rewritten-args debug line in: %s", out)
	}
}

func TestRunLocalFallbackNoDebugOutputWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	deps := fallbackDeps{
		lookup: func(string) (string, error) { return "/fake/ffmpeg", nil },
		exec:   func(string, []string) int { return 0 },
	}
	runLocalFallback(deps, protocol.ProgramFFmpeg, []string{"-i", "x"}, nil, false, errors.New("boom"), "addr")

	if strings.Contains(buf.String(), "[debug]") {
		t.Errorf("unexpected [debug] log line when debug=false: %s", buf.String())
	}
}

func TestRunLocalFallbackPropagatesChildExitCode(t *testing.T) {
	deps := fallbackDeps{
		lookup: func(string) (string, error) { return "/fake/ffmpeg", nil },
		exec:   func(string, []string) int { return 42 },
	}
	code := runLocalFallback(deps, protocol.ProgramFFmpeg, nil, nil, false, errors.New("boom"), "addr")
	if code != 42 {
		t.Errorf("exit code = %d, want 42 (child's exit code)", code)
	}
}

func TestExecChildPropagatesZeroExit(t *testing.T) {
	withHelperEnv(t)
	code := execChild(os.Args[0], helperArgs("exit", "0"))
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestExecChildPropagatesNonZeroExit(t *testing.T) {
	withHelperEnv(t)
	code := execChild(os.Args[0], helperArgs("exit", "42"))
	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
}

func TestExecChildScrubsFFmpegOverIPEnvFromChild(t *testing.T) {
	withHelperEnv(t)
	// Set a FFMPEG_OVER_IP_* var in the parent's env. execChild must strip
	// it before invoking the child, so the helper's check passes (exit 0).
	t.Setenv("FFMPEG_OVER_IP_CLIENT_AUTH_SECRET", "should-not-leak")
	code := execChild(os.Args[0], helperArgs("check-env-stripped"))
	if code == 99 {
		t.Error("FFMPEG_OVER_IP_CLIENT_AUTH_SECRET leaked into child env")
	} else if code != 0 {
		t.Errorf("helper exit code = %d, want 0 (no leak)", code)
	}
}

func TestExecChildReturns1OnStartError(t *testing.T) {
	// Hermetic nonexistent path — t.TempDir is freshly created and we never
	// write `no-such-bin` into it, so cmd.Start is guaranteed to fail.
	missing := filepath.Join(t.TempDir(), "no-such-bin")
	code := execChild(missing, []string{"-version"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (start failure)", code)
	}
}

func TestRealFallbackDepsWiresLookupAndExec(t *testing.T) {
	// Smoke-test the production wiring: realFallbackDeps must succeed in a
	// normal test environment (os.Executable resolvable) and return both
	// callable funcs. We don't invoke exec — that path is covered by
	// TestExecChild* — just verify we got real funcs.
	deps, err := realFallbackDeps()
	if err != nil {
		t.Fatalf("realFallbackDeps: %v", err)
	}
	if deps.lookup == nil {
		t.Error("deps.lookup is nil")
	}
	if deps.exec == nil {
		t.Error("deps.exec is nil")
	}

	// The lookup closure must consult $PATH at call time, not capture it
	// once. Set PATH to an empty dir; lookup should return an error.
	t.Setenv("PATH", t.TempDir())
	if _, err := deps.lookup("ffmpeg"); err == nil {
		t.Error("lookup with empty-dir PATH should fail")
	}
}

func TestResolveSelfReturnsAbsolute(t *testing.T) {
	got, err := resolveSelf()
	if err != nil {
		t.Fatalf("resolveSelf: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("got %q, want an absolute path", got)
	}
}
