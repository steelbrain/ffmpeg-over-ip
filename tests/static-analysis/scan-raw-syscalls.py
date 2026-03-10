#!/usr/bin/env python3
"""
scan-raw-syscalls.py — Static analysis for raw file syscalls in patched ffmpeg.

Two-level scan:
1. STRICT: Files we patch (file.c, hlsenc.c, utils.c) must have zero raw file
   syscalls unless marked with '// fio-safe'.
2. AUDIT: All other libavformat files are checked against a file-level allowlist.
   If a new file appears with raw syscalls, it may need patching.

Usage:
    python3 tests/static-analysis/scan-raw-syscalls.py [path-to-patched-ffmpeg-source]
"""

import os
import re
import sys
from pathlib import Path

# Raw POSIX file I/O function names we care about
SYSCALL_NAMES = {
    "open", "openat", "read", "write", "close",
    "unlink", "rename", "mkdir", "rmdir",
    "fstat", "lseek", "ftruncate", "access", "stat",
}

# Regex: bare syscall name preceded by a non-identifier char, followed by '('
# This avoids matching fio_read, av_read, ff_mkdir_p, etc.
SYSCALL_RE = re.compile(
    r'(?<![_a-zA-Z0-9])(' + '|'.join(SYSCALL_NAMES) + r')\s*\('
)

# Prefixes that indicate a wrapper, not a raw syscall
SAFE_PREFIXES = {"fio_", "av_", "ff_", "avio_", "url_", "avpriv_"}

# Files we've patched — these get strict checking
PATCHED_FILES = {"file.c", "hlsenc.c", "utils.c"}

# Files that are part of the fio layer itself
FIO_FILES = {"fio.c", "fio.h"}


def is_raw_syscall_line(line: str) -> bool:
    """Check if a line contains a raw file syscall that should be tunneled."""
    stripped = line.strip()

    # Skip fio-safe marked lines
    if "// fio-safe" in stripped:
        return False

    # Skip comments
    if stripped.startswith("//") or stripped.startswith("*"):
        return False

    # Find syscall matches
    for match in SYSCALL_RE.finditer(stripped):
        name = match.group(1)
        # Check the context before the match to see if it's a wrapper call
        start = match.start()
        prefix_text = stripped[:start]

        # Skip if preceded by -> (method call on struct, e.g., handler->read)
        if prefix_text.rstrip().endswith("->") or prefix_text.rstrip().endswith("."):
            continue

        # Skip if it's a prefixed wrapper
        if any(prefix_text.rstrip().endswith(p.rstrip("_")) for p in SAFE_PREFIXES):
            continue

        # Skip common false positives: function pointer field names, macro args
        # e.g., .read = file_read, GET_UTF16(..., read(pb), ...)
        if name in ("read", "write", "close") and ("GET_UTF16" in stripped or "extra_func" in stripped or "dynamic_handler" in stripped):
            continue

        # Skip lines that are function declarations/definitions (contain the
        # syscall name as part of a larger identifier like file_read, pipe_open)
        # by checking if the match is part of a _name pattern
        before = stripped[:match.start()]
        if before and before[-1] == '_':
            continue

        return True

    return False


def scan_file(filepath: Path) -> list[tuple[int, str]]:
    """Scan a single C file for raw syscall lines. Returns [(lineno, line), ...]."""
    hits = []
    try:
        with open(filepath) as f:
            for lineno, line in enumerate(f, 1):
                if is_raw_syscall_line(line):
                    hits.append((lineno, line.rstrip()))
    except (OSError, UnicodeDecodeError):
        pass
    return hits


def main():
    root = Path(__file__).resolve().parent.parent.parent
    if len(sys.argv) > 1:
        ffmpeg_src = Path(sys.argv[1])
    else:
        version = (root / "FFMPEG_VERSION").read_text().strip()
        ffmpeg_src = root / "build" / f"ffmpeg-{version}"
    allowlist_path = root / "tests" / "static-analysis" / "known-raw-syscall-files.txt"
    libavformat = ffmpeg_src / "libavformat"

    if not libavformat.is_dir():
        print(f"ERROR: {libavformat} not found")
        print("Run: bash scripts/build-ffmpeg.sh --minimal")
        sys.exit(1)

    print("=== Scanning for raw file syscalls in libavformat ===")
    print()

    failed = False

    # --- Level 1: Strict scan of patched files ---
    print("--- Level 1: Strict scan of patched files ---")
    for name in sorted(PATCHED_FILES):
        filepath = libavformat / name
        if not filepath.exists():
            print(f"  SKIP: {name} not found")
            continue

        hits = scan_file(filepath)
        if hits:
            print(f"  FAIL: {name} has raw syscalls:")
            for lineno, line in hits:
                print(f"    {lineno}: {line}")
            failed = True
        else:
            print(f"  OK: {name}")

    print()

    # --- Level 2: Audit scan of all other files ---
    print("--- Level 2: Audit scan of all libavformat files ---")

    files_with_syscalls = set()
    for filepath in sorted(libavformat.glob("*.c")):
        name = filepath.name
        if name in FIO_FILES or name in PATCHED_FILES:
            continue

        hits = scan_file(filepath)
        if hits:
            files_with_syscalls.add(name)

    # Load or create allowlist
    if allowlist_path.exists():
        allowed = set(allowlist_path.read_text().strip().splitlines())
    else:
        print("  Allowlist not found. Creating from current scan...")
        allowlist_path.write_text("\n".join(sorted(files_with_syscalls)) + "\n")
        print(f"  Created {allowlist_path} with {len(files_with_syscalls)} files")
        print()
        print("  Review and commit the allowlist.")
        sys.exit(0)

    new_files = files_with_syscalls - allowed
    removed_files = allowed - files_with_syscalls

    if not new_files and not removed_files:
        print(f"  OK: File list matches allowlist ({len(allowed)} files)")
    else:
        if new_files:
            print("  NEW files with raw syscalls (investigate — may need patching):")
            for f in sorted(new_files):
                print(f"    + {f}")
            failed = True
        if removed_files:
            print("  REMOVED files (safe to remove from allowlist):")
            for f in sorted(removed_files):
                print(f"    - {f}")

    print()

    if failed:
        print("FAIL: Raw syscall scan found issues.")
        sys.exit(1)

    print("PASS: All checks passed.")


if __name__ == "__main__":
    main()
