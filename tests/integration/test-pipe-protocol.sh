#!/usr/bin/env bash
set -euo pipefail

# Regression test for https://github.com/steelbrain/ffmpeg-over-ip/issues/8
#
# ffmpeg's pipe: protocol (pipe:0 / pipe:1 / pipe:2) is broken under
# ffmpeg-over-ip: the patched ffmpeg's file.c stores fd 0/1/2 directly in
# c->fd, then file_read / file_write call fio_read / fio_write with those
# raw stdio fds. In tunneled mode the fio layer rejects any fd below
# FIO_VFD_BASE (10000) with EBADF, surfacing as "Bad file descriptor".
#
# The Go side already forwards client stdin -> server -> ffmpeg.Stdin and
# ffmpeg.Stdout -> server -> client stdout, so once fio passes through stdio
# fds, pipe: just works end-to-end.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

FFMPEG="$ROOT/build/ffmpeg/bin/ffmpeg"

echo "=== test-pipe-protocol: ffmpeg pipe: protocol over ffmpeg-over-ip ==="

[ -x "$FFMPEG" ] || { echo "Patched ffmpeg not found. Run: bash scripts/build-ffmpeg.sh --minimal"; exit 1; }

TMPDIR_TEST=$(mktemp -d)
trap 'kill $SERVER_PID 2>/dev/null; wait $SERVER_PID 2>/dev/null; rm -rf $TMPDIR_TEST' EXIT

echo "Building server and client..."
BIN_DIR="$TMPDIR_TEST/bin"
mkdir -p "$BIN_DIR"
go build -o "$BIN_DIR/ffmpeg-over-ip-server" ./cmd/server
go build -o "$BIN_DIR/ffmpeg-over-ip-client" ./cmd/client
ln -sf "$FFMPEG" "$BIN_DIR/ffmpeg"
ln -sf "$ROOT/build/ffmpeg/bin/ffprobe" "$BIN_DIR/ffprobe"

PORT=$((20000 + RANDOM % 10000))

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

"$BIN_DIR/ffmpeg-over-ip-server" --config "$TMPDIR_TEST/server.jsonc" &
SERVER_PID=$!
sleep 0.5

if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "FAIL: server did not start"
    exit 1
fi
echo "Server running on port $PORT (PID $SERVER_PID)"

# Reference input: 5 frames of YUV420p 64x64 = 5 * 64*64*3/2 = 30720 bytes
INPUT="$TMPDIR_TEST/input.raw"
"$FFMPEG" -f lavfi -i "color=c=green:s=64x64:d=1:r=5" \
    -f rawvideo "$INPUT" -y 2>/dev/null
INPUT_SIZE=$(wc -c < "$INPUT" | tr -d ' ')
echo "Reference input: $INPUT_SIZE bytes"

# --- Test 1: pipe:1 (write output to client stdout) ---
echo ""
echo "--- Test 1: file input -> pipe:1 (client stdout) ---"
OUTPUT1="$TMPDIR_TEST/pipe1_output.raw"

set +e
FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    "$BIN_DIR/ffmpeg-over-ip-client" \
    -hide_banner -v error \
    -f rawvideo -video_size 64x64 -pix_fmt yuv420p \
    -i "$INPUT" \
    -f rawvideo pipe:1 \
    > "$OUTPUT1" 2> "$TMPDIR_TEST/pipe1.err"
EXIT1=$?
set -e

if [ "$EXIT1" -ne 0 ]; then
    echo "FAIL: client exited with $EXIT1"
    echo "--- stderr ---"
    cat "$TMPDIR_TEST/pipe1.err"
    exit 1
fi

if ! cmp -s "$INPUT" "$OUTPUT1"; then
    echo "FAIL: pipe:1 output does not match input"
    echo "  input:  $(wc -c < "$INPUT" | tr -d ' ') bytes"
    echo "  output: $(wc -c < "$OUTPUT1" | tr -d ' ') bytes"
    echo "--- stderr ---"
    cat "$TMPDIR_TEST/pipe1.err"
    exit 1
fi
echo "PASS: pipe:1 output matches input byte-for-byte"

# --- Test 2: pipe:0 (read input from client stdin) ---
echo ""
echo "--- Test 2: pipe:0 (client stdin) -> file output ---"
OUTPUT2="$TMPDIR_TEST/pipe0_output.raw"

set +e
FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    "$BIN_DIR/ffmpeg-over-ip-client" \
    -hide_banner -v error \
    -f rawvideo -video_size 64x64 -pix_fmt yuv420p \
    -i pipe:0 \
    -f rawvideo "$OUTPUT2" -y \
    < "$INPUT" 2> "$TMPDIR_TEST/pipe0.err"
EXIT2=$?
set -e

if [ "$EXIT2" -ne 0 ]; then
    echo "FAIL: client exited with $EXIT2"
    echo "--- stderr ---"
    cat "$TMPDIR_TEST/pipe0.err"
    exit 1
fi

if ! cmp -s "$INPUT" "$OUTPUT2"; then
    echo "FAIL: pipe:0 output does not match input"
    echo "  input:  $(wc -c < "$INPUT" | tr -d ' ') bytes"
    echo "  output: $(wc -c < "$OUTPUT2" | tr -d ' ') bytes"
    echo "--- stderr ---"
    cat "$TMPDIR_TEST/pipe0.err"
    exit 1
fi
echo "PASS: pipe:0 input transcoded byte-for-byte"

# --- Test 3: pipe:0 -> pipe:1 (full passthrough via stdio) ---
echo ""
echo "--- Test 3: pipe:0 -> pipe:1 (stdin -> stdout passthrough) ---"
OUTPUT3="$TMPDIR_TEST/pipe_both_output.raw"

set +e
FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    "$BIN_DIR/ffmpeg-over-ip-client" \
    -hide_banner -v error \
    -f rawvideo -video_size 64x64 -pix_fmt yuv420p \
    -i pipe:0 \
    -f rawvideo pipe:1 \
    < "$INPUT" > "$OUTPUT3" 2> "$TMPDIR_TEST/pipe_both.err"
EXIT3=$?
set -e

if [ "$EXIT3" -ne 0 ]; then
    echo "FAIL: client exited with $EXIT3"
    echo "--- stderr ---"
    cat "$TMPDIR_TEST/pipe_both.err"
    exit 1
fi

if ! cmp -s "$INPUT" "$OUTPUT3"; then
    echo "FAIL: pipe:0 -> pipe:1 output does not match input"
    echo "  input:  $(wc -c < "$INPUT" | tr -d ' ') bytes"
    echo "  output: $(wc -c < "$OUTPUT3" | tr -d ' ') bytes"
    echo "--- stderr ---"
    cat "$TMPDIR_TEST/pipe_both.err"
    exit 1
fi
echo "PASS: pipe:0 -> pipe:1 passthrough matches byte-for-byte"

echo ""
echo "test-pipe-protocol: ALL PASSED"
