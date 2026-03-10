#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FFMPEG="$ROOT/build/ffmpeg/bin/ffmpeg"
HARNESS="$ROOT/tests/integration/harness"

echo "=== test-errors: error handling via fio ==="

[ -x "$FFMPEG" ] || { echo "Patched ffmpeg not found. Run: bash scripts/build-ffmpeg.sh --minimal"; exit 1; }
go build -o "$HARNESS" ./internal/harness/

TMPDIR_TEST=$(mktemp -d)
trap "rm -rf $TMPDIR_TEST" EXIT

FAILED=0

# --- Test 1: Missing input file ---
echo "--- Test 1: Missing input file ---"
EXIT_CODE=0
"$HARNESS" "$FFMPEG" \
    -f rawvideo -video_size 64x64 -pix_fmt yuv420p \
    -i "$TMPDIR_TEST/nonexistent.raw" \
    -f null /dev/null -y 2>/dev/null || EXIT_CODE=$?

if [ "$EXIT_CODE" -eq 0 ]; then
    echo "FAIL: ffmpeg should have exited with error for missing input"
    FAILED=1
else
    echo "PASS: ffmpeg exited with code $EXIT_CODE for missing input"
fi

# --- Test 2: Unwritable output directory ---
echo ""
echo "--- Test 2: Unwritable output path ---"
EXIT_CODE=0
"$HARNESS" "$FFMPEG" \
    -f lavfi -i "color=c=red:s=64x64:d=1:r=5" \
    -f rawvideo "/nonexistent/deep/path/output.raw" -y 2>/dev/null || EXIT_CODE=$?

if [ "$EXIT_CODE" -eq 0 ]; then
    echo "FAIL: ffmpeg should have exited with error for unwritable path"
    FAILED=1
else
    echo "PASS: ffmpeg exited with code $EXIT_CODE for unwritable path"
fi

echo ""
if [ "$FAILED" -ne 0 ]; then
    echo "test-errors: SOME TESTS FAILED"
    exit 1
fi

echo "test-errors: ALL PASSED"
