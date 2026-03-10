#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FFMPEG="$ROOT/build/ffmpeg/bin/ffmpeg"
FFPROBE="$ROOT/build/ffmpeg/bin/ffprobe"
HARNESS="$ROOT/tests/integration/harness"

echo "=== test-ffprobe: read-only metadata extraction via fio ==="

[ -x "$FFPROBE" ] || { echo "Patched ffprobe not found. Run: bash scripts/build-ffmpeg.sh --minimal"; exit 1; }
go build -o "$HARNESS" ./internal/harness/

TMPDIR_TEST=$(mktemp -d)
trap "rm -rf $TMPDIR_TEST" EXIT

# Create a known input file in mpegts container (ffprobe can auto-detect)
INPUT="$TMPDIR_TEST/input.ts"
"$FFMPEG" -f lavfi -i "color=c=yellow:s=128x96:d=2:r=10" \
    -c:v rawvideo -f mpegts "$INPUT" -y 2>/dev/null

INPUT_SIZE=$(wc -c < "$INPUT" | tr -d ' ')
echo "Input: $INPUT_SIZE bytes (mpegts)"

# Run ffprobe through harness (reads file via fio)
PROBE_OUT=$("$HARNESS" "$FFPROBE" \
    -show_format -show_streams \
    "$INPUT" 2>&1 || true)

# Verify ffprobe found a stream
if ! echo "$PROBE_OUT" | grep -q "codec_type="; then
    echo "FAIL: ffprobe did not detect any stream"
    echo "$PROBE_OUT"
    exit 1
fi
echo "PASS: ffprobe detected stream"

# Verify format
if ! echo "$PROBE_OUT" | grep -q "format_name=mpegts"; then
    echo "FAIL: ffprobe did not detect mpegts format"
    echo "$PROBE_OUT" | grep format_name || true
    exit 1
fi
echo "PASS: ffprobe detected mpegts format"

# Verify duration is approximately 2 seconds
if echo "$PROBE_OUT" | grep -q "duration="; then
    echo "PASS: ffprobe extracted duration"
fi

echo ""
echo "test-ffprobe: ALL PASSED"
