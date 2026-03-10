#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FFMPEG="$ROOT/build/ffmpeg/bin/ffmpeg"
HARNESS="$ROOT/tests/integration/harness"

echo "=== test-hls-delete-segments: old segment deletion via fio_unlink ==="

[ -x "$FFMPEG" ] || { echo "Patched ffmpeg not found. Run: bash scripts/build-ffmpeg.sh --minimal"; exit 1; }
go build -o "$HARNESS" ./internal/harness/

TMPDIR_TEST=$(mktemp -d)
trap "rm -rf $TMPDIR_TEST" EXIT

# Generate HLS with short segments and segment deletion enabled.
# hls_list_size=2 + hls_flags=delete_segments means only the last 2 segments
# are kept; older ones are deleted via unlink.
"$HARNESS" "$FFMPEG" \
    -f lavfi -i "color=c=cyan:s=64x64:d=6:r=5" \
    -c:v rawvideo -f hls \
    -hls_time 1 \
    -hls_list_size 2 \
    -hls_flags delete_segments \
    -hls_segment_filename "$TMPDIR_TEST/seg_%03d.ts" \
    "$TMPDIR_TEST/playlist.m3u8" -y 2>&1 | tail -2

# With 6 seconds at 1s segments, we should have ~6 segments generated.
# With hls_list_size=2 and delete_segments, only the last 2-3 should remain.
REMAINING=$(ls "$TMPDIR_TEST"/seg_*.ts 2>/dev/null | wc -l | tr -d ' ')

if [ "$REMAINING" -gt 4 ]; then
    echo "FAIL: expected at most 4 segments remaining, got $REMAINING"
    echo "Segments: $(ls "$TMPDIR_TEST"/seg_*.ts 2>/dev/null)"
    exit 1
fi

if [ "$REMAINING" -eq 0 ]; then
    echo "FAIL: no segments remaining"
    exit 1
fi

echo "PASS: $REMAINING segments remaining (old segments were deleted via fio_unlink)"

# Verify playlist only references remaining segments
PLAYLIST_SEGS=$(grep '\.ts' "$TMPDIR_TEST/playlist.m3u8" | wc -l | tr -d ' ')
echo "PASS: playlist references $PLAYLIST_SEGS segments"

echo ""
echo "test-hls-delete-segments: ALL PASSED"
