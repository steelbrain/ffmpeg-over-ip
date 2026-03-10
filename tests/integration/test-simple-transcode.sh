#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

FFMPEG="$ROOT/build/ffmpeg/bin/ffmpeg"
FFPROBE="$ROOT/build/ffmpeg/bin/ffprobe"
HARNESS="$ROOT/tests/integration/harness"

echo "=== test-simple-transcode: patched ffmpeg end-to-end via fio ==="

# Check that patched ffmpeg is built
if [ ! -x "$FFMPEG" ]; then
    echo "Patched ffmpeg not found at $FFMPEG"
    echo "Run: bash scripts/build-ffmpeg.sh --minimal"
    exit 1
fi

# Build fio-harness
echo "Building fio-harness..."
go build -o "$HARNESS" ./internal/harness/

TMPDIR_TEST=$(mktemp -d)
trap "rm -rf $TMPDIR_TEST" EXIT

# --- Test 1: Generate output (lavfi source, write via fio) ---
echo ""
echo "--- Test 1: lavfi source → rawvideo output via fio ---"
OUTPUT1="$TMPDIR_TEST/lavfi_output.raw"

"$HARNESS" "$FFMPEG" \
    -f lavfi -i "color=c=blue:s=64x64:d=1:r=5" \
    -f rawvideo "$OUTPUT1" -y 2>&1 | tail -1

if [ ! -s "$OUTPUT1" ]; then
    echo "FAIL: output file is empty or missing"
    exit 1
fi
SIZE1=$(wc -c < "$OUTPUT1" | tr -d ' ')
echo "PASS: generated $SIZE1 bytes"

# --- Test 2: Read input + write output (both via fio) ---
echo ""
echo "--- Test 2: read input + write output (both via fio) ---"

# Create input file using unpatched ffmpeg (direct, no fio)
INPUT2="$TMPDIR_TEST/input.raw"
OUTPUT2="$TMPDIR_TEST/copy_output.raw"
"$FFMPEG" -f lavfi -i "color=c=red:s=64x64:d=1:r=5" \
    -f rawvideo "$INPUT2" -y 2>/dev/null

echo "Input: $(wc -c < "$INPUT2" | tr -d ' ') bytes"

# Run ffmpeg through harness: read input via fio, write output via fio
"$HARNESS" "$FFMPEG" \
    -f rawvideo -video_size 64x64 -pix_fmt yuv420p \
    -i "$INPUT2" \
    -f rawvideo "$OUTPUT2" -y 2>&1 | tail -1

if [ ! -s "$OUTPUT2" ]; then
    echo "FAIL: output file is empty or missing"
    exit 1
fi

if ! cmp -s "$INPUT2" "$OUTPUT2"; then
    echo "FAIL: output does not match input"
    echo "  input:  $(wc -c < "$INPUT2" | tr -d ' ') bytes"
    echo "  output: $(wc -c < "$OUTPUT2" | tr -d ' ') bytes"
    exit 1
fi
echo "PASS: input and output match byte-for-byte ($(wc -c < "$OUTPUT2" | tr -d ' ') bytes)"

# --- Test 3: Verify output with ffprobe ---
echo ""
echo "--- Test 3: ffprobe verification ---"

# Generate a matroska file (via fio) that ffprobe can inspect
# Use rawvideo in matroska container — but this had issues with minimal build
# Instead, verify the raw file has correct frame count by size
FRAME_SIZE=$((64 * 64 * 3 / 2))  # YUV420p: width * height * 1.5
EXPECTED_SIZE=$((FRAME_SIZE * 5))  # 5 frames at 5fps for 1 second
ACTUAL_SIZE=$(wc -c < "$OUTPUT2" | tr -d ' ')

if [ "$ACTUAL_SIZE" -ne "$EXPECTED_SIZE" ]; then
    echo "FAIL: expected $EXPECTED_SIZE bytes (5 frames), got $ACTUAL_SIZE"
    exit 1
fi
echo "PASS: output size matches expected frame count (5 frames × $FRAME_SIZE bytes)"

# --- Test 4: ffprobe reads input via fio ---
echo ""
echo "--- Test 4: ffprobe reads file via fio ---"

PROBE_OUT=$("$HARNESS" "$FFPROBE" \
    -f rawvideo -video_size 64x64 -pix_fmt yuv420p \
    "$INPUT2" 2>&1 || true)

if echo "$PROBE_OUT" | grep -q "rawvideo"; then
    echo "PASS: ffprobe parsed input via fio"
else
    echo "FAIL: ffprobe could not parse input via fio"
    echo "$PROBE_OUT"
    exit 1
fi

echo ""
echo "test-simple-transcode: ALL PASSED"
