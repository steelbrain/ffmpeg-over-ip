#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== test-fallback: client falls back to local ffmpeg on dial failure ==="

# This test exercises the fallback path end-to-end. Unlike other integration
# tests it does NOT need the patched ffmpeg or a running server — the whole
# point is that the server is unreachable.

TMPDIR_TEST=$(mktemp -d)
trap 'rm -rf $TMPDIR_TEST' EXIT

# Build the client binary into the temp dir.
BIN_DIR="$TMPDIR_TEST/bin"
mkdir -p "$BIN_DIR"
go build -o "$BIN_DIR/ffmpeg-over-ip-client" ./cmd/client

# Stub ffmpeg + ffprobe that record their argv and a marker, then exit with a
# specific code so we can verify propagation.
STUB_DIR="$TMPDIR_TEST/stub-bin"
mkdir -p "$STUB_DIR"
cat > "$STUB_DIR/ffmpeg" << 'STUB'
#!/bin/sh
echo "STUB_FFMPEG argv: $*"
# Verify env was scrubbed: any FFMPEG_OVER_IP_* env var must not be set.
if env | grep -q '^FFMPEG_OVER_IP_'; then
    echo "STUB_FFMPEG: leaked env vars detected" >&2
    env | grep '^FFMPEG_OVER_IP_' >&2
    exit 99
fi
exit 42
STUB
chmod +x "$STUB_DIR/ffmpeg"
cat > "$STUB_DIR/ffprobe" << 'STUB'
#!/bin/sh
echo "STUB_FFPROBE argv: $*"
exit 7
STUB
chmod +x "$STUB_DIR/ffprobe"

# Pick a port nothing is listening on (use 1, which is reliably refused).
DEAD_ADDR="127.0.0.1:1"

FAILED=0

# --- Test 1: dial fails, fallback runs stub ffmpeg, args rewritten, env scrubbed, exit code propagated ---
echo ""
echo "--- Test 1: fallback to stub ffmpeg with rewrite ---"
cat > "$TMPDIR_TEST/client.jsonc" << CONF
{
  "address": "$DEAD_ADDR",
  "authSecret": "doesnt-matter",
  "fallbackToLocal": true,
  "fallbackRewrites": [["h264_nvenc", "h264_qsv"]],
  "log": "stderr"
}
CONF

EXIT_CODE=0
# Capture stdout and stderr separately so we can verify the invisible-proxy
# guarantee: our diagnostics must NOT mix into stdout (Jellyfin parses it).
# Set a FFMPEG_OVER_IP_* env var that the stub will check is NOT leaked.
FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=should-not-leak \
    FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    PATH="$STUB_DIR:$PATH" \
    "$BIN_DIR/ffmpeg-over-ip-client" -i input.mp4 -c:v h264_nvenc output.mp4 \
    >"$TMPDIR_TEST/t1.out" 2>"$TMPDIR_TEST/t1.err" || EXIT_CODE=$?
STDOUT=$(cat "$TMPDIR_TEST/t1.out")
STDERR=$(cat "$TMPDIR_TEST/t1.err")

if [ "$EXIT_CODE" -ne 42 ]; then
    echo "FAIL: expected exit 42 (stub's exit code), got $EXIT_CODE"
    echo "stdout: $STDOUT"
    echo "stderr: $STDERR"
    FAILED=1
elif ! echo "$STDOUT" | grep -q 'STUB_FFMPEG argv: -i input.mp4 -c:v h264_qsv output.mp4'; then
    echo "FAIL: stub did not see rewritten args (or stub output went to wrong stream)"
    echo "stdout: $STDOUT"
    echo "stderr: $STDERR"
    FAILED=1
elif echo "$STDOUT" | grep -qiE 'fallback|server unreachable'; then
    echo "FAIL: client diagnostic leaked into stdout (constraint 8: invisible proxy)"
    echo "stdout: $STDOUT"
    FAILED=1
elif ! echo "$STDERR" | grep -q 'falling back to local'; then
    echo "FAIL: expected 'falling back to local' diagnostic in stderr (log:stderr is configured)"
    echo "stderr: $STDERR"
    FAILED=1
else
    echo "PASS: fallback ran stub, rewrote h264_nvenc -> h264_qsv, scrubbed env, propagated exit 42, diagnostics stayed in stderr"
fi

# --- Test 2: ffprobe detection via argv[0] basename ---
echo ""
echo "--- Test 2: ffprobe basename routes to stub ffprobe ---"
ln -sf "$BIN_DIR/ffmpeg-over-ip-client" "$BIN_DIR/ffprobe"

EXIT_CODE=0
OUT=$(FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    PATH="$STUB_DIR:$PATH" \
    "$BIN_DIR/ffprobe" -show_streams input.mp4 2>&1) || EXIT_CODE=$?

if [ "$EXIT_CODE" -ne 7 ]; then
    echo "FAIL: expected exit 7 (stub ffprobe's exit code), got $EXIT_CODE"
    echo "output: $OUT"
    FAILED=1
elif ! echo "$OUT" | grep -q 'STUB_FFPROBE argv: -show_streams input.mp4'; then
    echo "FAIL: stub ffprobe did not run with expected args"
    echo "output: $OUT"
    FAILED=1
else
    echo "PASS: client invoked as 'ffprobe' fell back to stub ffprobe"
fi

# --- Test 3: fallbackToLocal=false → dial failure stays fatal (no regression) ---
echo ""
echo "--- Test 3: fallback disabled → fatal dial failure ---"
cat > "$TMPDIR_TEST/client-no-fallback.jsonc" << CONF
{
  "address": "$DEAD_ADDR",
  "authSecret": "x",
  "log": "stderr"
}
CONF

EXIT_CODE=0
OUT=$(FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client-no-fallback.jsonc" \
    PATH="$STUB_DIR:$PATH" \
    "$BIN_DIR/ffmpeg-over-ip-client" -version 2>&1) || EXIT_CODE=$?

if [ "$EXIT_CODE" -eq 0 ]; then
    echo "FAIL: expected non-zero exit when fallback disabled and server unreachable"
    FAILED=1
elif echo "$OUT" | grep -q 'STUB_FFMPEG'; then
    echo "FAIL: stub ran even though fallback was disabled"
    echo "output: $OUT"
    FAILED=1
else
    echo "PASS: fallback-off → exit $EXIT_CODE, stub not invoked"
fi

# --- Test 4: fallback enabled but no binary on PATH → exit 1 ---
echo ""
echo "--- Test 4: fallback enabled, no binary on PATH → exit 1 ---"
EMPTY_PATH_DIR="$TMPDIR_TEST/empty-bin"
mkdir -p "$EMPTY_PATH_DIR"

EXIT_CODE=0
FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    PATH="$EMPTY_PATH_DIR" \
    "$BIN_DIR/ffmpeg-over-ip-client" -version \
    >"$TMPDIR_TEST/t4.out" 2>"$TMPDIR_TEST/t4.err" || EXIT_CODE=$?
STDOUT=$(cat "$TMPDIR_TEST/t4.out")
STDERR=$(cat "$TMPDIR_TEST/t4.err")

if [ "$EXIT_CODE" -ne 1 ]; then
    echo "FAIL: expected exit 1 (no binary on PATH), got $EXIT_CODE"
    echo "stdout: $STDOUT"
    echo "stderr: $STDERR"
    FAILED=1
elif [ -n "$STDOUT" ]; then
    echo "FAIL: stdout should be empty (constraint 8: invisible proxy)"
    echo "stdout: $STDOUT"
    FAILED=1
elif ! echo "$STDERR" | grep -q 'no local ffmpeg on PATH'; then
    echo "FAIL: expected 'no local ffmpeg on PATH' diagnostic in stderr"
    echo "stderr: $STDERR"
    FAILED=1
else
    echo "PASS: no fallback binary on PATH → exit 1, diagnostic in stderr, stdout clean"
fi

# --- Test 5: client installed as 'ffmpeg' on PATH → self-skip prevents recursion ---
echo ""
echo "--- Test 5: self-skip when client is the only 'ffmpeg' on PATH ---"
SELF_DIR="$TMPDIR_TEST/self-bin"
mkdir -p "$SELF_DIR"
ln -sf "$BIN_DIR/ffmpeg-over-ip-client" "$SELF_DIR/ffmpeg"

EXIT_CODE=0
FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc" \
    PATH="$SELF_DIR" \
    "$SELF_DIR/ffmpeg" -version \
    >"$TMPDIR_TEST/t5.out" 2>"$TMPDIR_TEST/t5.err" || EXIT_CODE=$?
STDOUT=$(cat "$TMPDIR_TEST/t5.out")
STDERR=$(cat "$TMPDIR_TEST/t5.err")

# Should exit 1 (no usable ffmpeg found — self was correctly skipped, no other
# candidate on PATH). The crucial check: client did NOT recurse.
if [ "$EXIT_CODE" -ne 1 ]; then
    echo "FAIL: expected exit 1 when only candidate is self, got $EXIT_CODE"
    echo "stdout: $STDOUT"
    echo "stderr: $STDERR"
    FAILED=1
elif [ -n "$STDOUT" ]; then
    echo "FAIL: stdout should be empty (constraint 8: invisible proxy)"
    echo "stdout: $STDOUT"
    FAILED=1
elif echo "$STDERR" | grep -q 'no "ffmpeg" found in PATH (excluding self)'; then
    echo "PASS: self-skip prevented recursion, diagnostic in stderr, stdout clean"
else
    echo "FAIL: unexpected output, expected self-exclusion message"
    echo "stderr: $STDERR"
    FAILED=1
fi

# --- Test 6: debug:true logs original and rewritten args ---
echo ""
echo "--- Test 6: debug=true logs [debug] original/rewritten args ---"
cat > "$TMPDIR_TEST/client-debug.jsonc" << CONF
{
  "address": "$DEAD_ADDR",
  "authSecret": "x",
  "fallbackToLocal": true,
  "fallbackRewrites": [["h264_nvenc", "h264_qsv"]],
  "debug": true,
  "log": "stderr"
}
CONF

EXIT_CODE=0
FFMPEG_OVER_IP_CLIENT_CONFIG="$TMPDIR_TEST/client-debug.jsonc" \
    PATH="$STUB_DIR:$PATH" \
    "$BIN_DIR/ffmpeg-over-ip-client" -i input.mp4 -c:v h264_nvenc output.mp4 \
    >"$TMPDIR_TEST/t6.out" 2>"$TMPDIR_TEST/t6.err" || EXIT_CODE=$?
STDOUT=$(cat "$TMPDIR_TEST/t6.out")
STDERR=$(cat "$TMPDIR_TEST/t6.err")

if [ "$EXIT_CODE" -ne 42 ]; then
    echo "FAIL: expected exit 42 (stub's exit code), got $EXIT_CODE"
    echo "stderr: $STDERR"
    FAILED=1
elif ! echo "$STDERR" | grep -q '\[debug\] original args:.*h264_nvenc'; then
    echo "FAIL: expected '[debug] original args:' line containing h264_nvenc in stderr"
    echo "stderr: $STDERR"
    FAILED=1
elif ! echo "$STDERR" | grep -q '\[debug\] rewritten args:.*h264_qsv'; then
    echo "FAIL: expected '[debug] rewritten args:' line containing h264_qsv in stderr"
    echo "stderr: $STDERR"
    FAILED=1
elif echo "$STDOUT" | grep -q '\[debug\]'; then
    echo "FAIL: [debug] line leaked into stdout (constraint 8: invisible proxy)"
    echo "stdout: $STDOUT"
    FAILED=1
else
    echo "PASS: debug=true logged original/rewritten args to configured log sink"
fi

echo ""
if [ "$FAILED" -ne 0 ]; then
    echo "test-fallback: SOME TESTS FAILED"
    exit 1
fi
echo "test-fallback: ALL PASSED"
