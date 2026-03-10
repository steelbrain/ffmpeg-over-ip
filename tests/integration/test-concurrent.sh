#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FFMPEG="$ROOT/build/ffmpeg/bin/ffmpeg"
HARNESS="$ROOT/tests/integration/harness"

echo "=== test-concurrent: two simultaneous transcodes via fio ==="

[ -x "$FFMPEG" ] || { echo "Patched ffmpeg not found. Run: bash scripts/build-ffmpeg.sh --minimal"; exit 1; }
go build -o "$HARNESS" ./internal/harness/

TMPDIR_TEST=$(mktemp -d)
trap "rm -rf $TMPDIR_TEST" EXIT

OUTPUT_A="$TMPDIR_TEST/output_a.raw"
OUTPUT_B="$TMPDIR_TEST/output_b.raw"

# Launch two concurrent transcodes with different parameters
"$HARNESS" "$FFMPEG" \
    -f lavfi -i "color=c=red:s=64x64:d=2:r=5" \
    -f rawvideo "$OUTPUT_A" -y 2>/dev/null &
PID_A=$!

"$HARNESS" "$FFMPEG" \
    -f lavfi -i "color=c=blue:s=32x32:d=2:r=10" \
    -f rawvideo "$OUTPUT_B" -y 2>/dev/null &
PID_B=$!

# Wait for both
FAIL=0
wait $PID_A || { echo "FAIL: transcode A exited with error"; FAIL=1; }
wait $PID_B || { echo "FAIL: transcode B exited with error"; FAIL=1; }

if [ "$FAIL" -ne 0 ]; then
    exit 1
fi

# Verify outputs exist and have correct sizes
# A: 64x64 yuv420p = 6144 bytes/frame, 2s at 5fps = 10 frames = 61440 bytes
SIZE_A=$(wc -c < "$OUTPUT_A" | tr -d ' ')
EXPECTED_A=$((64 * 64 * 3 / 2 * 10))
if [ "$SIZE_A" -ne "$EXPECTED_A" ]; then
    echo "FAIL: output A size $SIZE_A, expected $EXPECTED_A"
    exit 1
fi
echo "PASS: output A correct ($SIZE_A bytes, 10 frames at 64x64)"

# B: 32x32 yuv420p = 1536 bytes/frame, 2s at 10fps = 20 frames = 30720 bytes
SIZE_B=$(wc -c < "$OUTPUT_B" | tr -d ' ')
EXPECTED_B=$((32 * 32 * 3 / 2 * 20))
if [ "$SIZE_B" -ne "$EXPECTED_B" ]; then
    echo "FAIL: output B size $SIZE_B, expected $EXPECTED_B"
    exit 1
fi
echo "PASS: output B correct ($SIZE_B bytes, 20 frames at 32x32)"

# Verify no cross-contamination: files should be different sizes
if [ "$SIZE_A" -eq "$SIZE_B" ]; then
    echo "WARN: outputs are same size (may indicate cross-contamination)"
fi

echo ""
echo "test-concurrent: ALL PASSED"
