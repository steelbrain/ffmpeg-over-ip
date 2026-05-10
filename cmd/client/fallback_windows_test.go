//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// On Windows, findFallbackBinary must try each %PATHEXT% extension to locate
// an executable. Verify that an `ffmpeg.exe` on PATH is found when the lookup
// uses the real fallbackExtensions() list.
func TestFindFallbackBinaryFindsDotExeOnWindows(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ffmpeg.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatalf("write ffmpeg.exe: %v", err)
	}

	got, err := findFallbackBinary("ffmpeg", dir, "C:\\not\\self.exe", fallbackExtensions())
	if err != nil {
		t.Fatalf("findFallbackBinary: %v", err)
	}
	if got != exe {
		t.Errorf("got %q, want %q", got, exe)
	}
}

// pathsEqual on Windows must be case-insensitive — NTFS canonicalizes path
// case, so a candidate resolved as `ffmpeg.EXE` should match a self path
// recorded as `ffmpeg.exe`.
func TestPathsEqualCaseInsensitiveOnWindows(t *testing.T) {
	if !pathsEqual("C:\\Users\\Foo\\Bin\\ffmpeg.EXE", "c:\\users\\foo\\bin\\ffmpeg.exe") {
		t.Error("expected case-insensitive match on Windows")
	}
	if pathsEqual("C:\\a\\b", "C:\\a\\c") {
		t.Error("different paths should not match")
	}
}
