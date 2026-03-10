#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
FFMPEG="$ROOT/build/ffmpeg/bin/ffmpeg"
HARNESS="$ROOT/tests/integration/harness"

echo "=== test-hls-transcode: HLS segment writes and playlist rename via fio ==="

[ -x "$FFMPEG" ] || { echo "Patched ffmpeg not found. Run: bash scripts/build-ffmpeg.sh --minimal"; exit 1; }
go build -o "$HARNESS" ./internal/harness/

TMPDIR_TEST=$(mktemp -d)
trap "rm -rf $TMPDIR_TEST" EXIT

# Generate HLS output via fio: creates .ts segments + .m3u8 playlist
"$HARNESS" "$FFMPEG" \
    -f lavfi -i "color=c=green:s=64x64:d=3:r=5" \
    -c:v rawvideo -f hls \
    -hls_time 1 -hls_segment_filename "$TMPDIR_TEST/seg_%03d.ts" \
    "$TMPDIR_TEST/playlist.m3u8" -y 2>&1 | tail -2

# Verify playlist exists
if [ ! -f "$TMPDIR_TEST/playlist.m3u8" ]; then
    echo "FAIL: playlist.m3u8 not created"
    exit 1
fi
echo "PASS: playlist.m3u8 created"

# Verify segments exist
SEG_COUNT=$(ls "$TMPDIR_TEST"/seg_*.ts 2>/dev/null | wc -l | tr -d ' ')
if [ "$SEG_COUNT" -eq 0 ]; then
    echo "FAIL: no .ts segments created"
    exit 1
fi
echo "PASS: $SEG_COUNT .ts segments created"

# Verify playlist references the segments
if ! grep -q '\.ts' "$TMPDIR_TEST/playlist.m3u8"; then
    echo "FAIL: playlist doesn't reference .ts segments"
    exit 1
fi
echo "PASS: playlist references segments"

# Verify playlist has required HLS tags
if ! grep -q '#EXTM3U' "$TMPDIR_TEST/playlist.m3u8"; then
    echo "FAIL: playlist missing #EXTM3U tag"
    exit 1
fi
if ! grep -q '#EXT-X-ENDLIST' "$TMPDIR_TEST/playlist.m3u8"; then
    echo "FAIL: playlist missing #EXT-X-ENDLIST tag"
    exit 1
fi
echo "PASS: playlist has valid HLS structure"

echo ""
echo "test-hls-transcode: ALL PASSED"
