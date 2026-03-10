#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

FFMPEG="$ROOT/build/ffmpeg/bin/ffmpeg"

echo "=== test-client-server: server + client over TCP with patched ffmpeg ==="

[ -x "$FFMPEG" ] || { echo "Patched ffmpeg not found. Run: bash scripts/build-ffmpeg.sh --minimal"; exit 1; }

# Create temp dir for test artifacts
TMPDIR_TEST=$(mktemp -d)
trap 'kill $SERVER_PID 2>/dev/null; wait $SERVER_PID 2>/dev/null; rm -rf $TMPDIR_TEST' EXIT

# Build server and client into temp dir alongside ffmpeg/ffprobe symlinks
echo "Building server and client..."
BIN_DIR="$TMPDIR_TEST/bin"
mkdir -p "$BIN_DIR"
go build -o "$BIN_DIR/ffmpeg-over-ip-server" ./cmd/server
go build -o "$BIN_DIR/ffmpeg-over-ip-client" ./cmd/client
ln -sf "$FFMPEG" "$BIN_DIR/ffmpeg"
ln -sf "$ROOT/build/ffmpeg/bin/ffprobe" "$BIN_DIR/ffprobe"

# Pick a random port
PORT=$((20000 + RANDOM % 10000))

# Write configs
cat > "$TMPDIR_TEST/server.jsonc" << CONF
{
  "address": "127.0.0.1:$PORT",
  "authSecret": "test-secret-key"
}
CONF

cat > "$TMPDIR_TEST/client.jsonc" << CONF
{
  "address": "127.0.0.1:$PORT",
  "authSecret": "test-secret-key"
}
CONF

# Start server
"$BIN_DIR/ffmpeg-over-ip-server" --config "$TMPDIR_TEST/server.jsonc" &
SERVER_PID=$!
sleep 0.5

# Verify server is running
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "FAIL: server did not start"
    exit 1
fi
echo "Server running on port $PORT (PID $SERVER_PID)"

# --- Test 1: lavfi source → rawvideo output ---
echo ""
echo "--- Test 1: transcode via fio (lavfi → rawvideo) ---"
OUTPUT1="$TMPDIR_TEST/output.raw"

FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    "$BIN_DIR/ffmpeg-over-ip-client" \
    -f lavfi -i "color=c=blue:s=64x64:d=1:r=5" \
    -f rawvideo "$OUTPUT1" -y 2>&1 | tail -1

if [ ! -s "$OUTPUT1" ]; then
    echo "FAIL: output file missing or empty"
    exit 1
fi
SIZE1=$(wc -c < "$OUTPUT1" | tr -d ' ')
EXPECTED=$((64 * 64 * 3 / 2 * 5))  # YUV420p, 5 frames
if [ "$SIZE1" -ne "$EXPECTED" ]; then
    echo "FAIL: output size $SIZE1, expected $EXPECTED"
    exit 1
fi
echo "PASS: output $SIZE1 bytes (5 frames)"

# --- Test 2: read input + write output (both via fio) ---
echo ""
echo "--- Test 2: read + write (both via fio) ---"
INPUT2="$TMPDIR_TEST/input.raw"
OUTPUT2="$TMPDIR_TEST/copy.raw"

# Create input file directly
"$FFMPEG" -f lavfi -i "color=c=red:s=64x64:d=1:r=5" -f rawvideo "$INPUT2" -y 2>/dev/null

FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    "$BIN_DIR/ffmpeg-over-ip-client" \
    -f rawvideo -video_size 64x64 -pix_fmt yuv420p \
    -i "$INPUT2" \
    -f rawvideo "$OUTPUT2" -y 2>&1 | tail -1

if ! cmp -s "$INPUT2" "$OUTPUT2"; then
    echo "FAIL: output does not match input"
    exit 1
fi
echo "PASS: input and output match byte-for-byte"

# --- Test 3: non-zero exit code ---
echo ""
echo "--- Test 3: non-zero exit code forwarded ---"
EXIT_CODE=0
FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    "$BIN_DIR/ffmpeg-over-ip-client" \
    -f rawvideo -video_size 64x64 -pix_fmt yuv420p \
    -i "$TMPDIR_TEST/nonexistent.raw" \
    -f null /dev/null -y 2>/dev/null || EXIT_CODE=$?

if [ "$EXIT_CODE" -eq 0 ]; then
    echo "FAIL: expected non-zero exit code for missing input"
    exit 1
fi
echo "PASS: client exited with code $EXIT_CODE"

# --- Test 4: auth failure ---
echo ""
echo "--- Test 4: auth failure ---"
cat > "$TMPDIR_TEST/bad-client.jsonc" << CONF
{
  "address": "127.0.0.1:$PORT",
  "authSecret": "wrong-secret"
}
CONF

EXIT_CODE=0
FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/bad-client.jsonc" \
    "$BIN_DIR/ffmpeg-over-ip-client" -version 2>/dev/null || EXIT_CODE=$?

if [ "$EXIT_CODE" -eq 0 ]; then
    echo "FAIL: expected failure with wrong auth secret"
    exit 1
fi
echo "PASS: auth failure rejected (exit code $EXIT_CODE)"

echo ""
echo "test-client-server: ALL PASSED"
