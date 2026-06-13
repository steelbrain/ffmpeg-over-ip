#!/usr/bin/env bash
#
# benchmark-seek.sh — seek-heavy ("scrubbing") workload for ffmpeg-over-ip.
#
# The localhost benchmark measures a purely sequential stream copy, which the
# read-ahead/prefetch layer is tuned for. This benchmark instead stresses the
# case the prefetch layer can hurt: many seeks into one file on slow backing
# media, where a speculative prefetch issued just before a seek is wasted work
# that a seek must wait on.
#
# It runs N input-seek extractions at spaced timestamps and compares three
# configurations of the SAME patched binary:
#
#   local ffmpeg            direct, no tunnel (reference)
#   over-ip (optimized)     read-ahead + prefetch + range cache ON (this branch)
#   over-ip (no-cache)      FFOIP_READAHEAD_BYTES=0 / FFOIP_RANGE_CACHE_BYTES=0,
#                           which reverts fio to one-read-per-request (≈ main)
#
# Because both over-ip configurations pay identical process/connect overhead,
# their difference isolates the new caching layer. If "optimized" is not slower
# than "no-cache" on this workload, the seek/slow-media regression concern is
# empirically addressed.
#
# Environment:
#   BENCH_INPUT        path to a real media file (required for a meaningful run;
#                      a tiny synthetic file is generated if unset)
#   BENCH_SEEKS        number of seek extractions (default 40)
#   BENCH_CLIP         seconds copied at each seek point (default 0.5)
#   BENCH_LATENCY_MS   per-chunk latency injected on the client<->server link
#                      to emulate slow media (default 0 = none)
#   FFMPEG / FFPROBE   override binary paths
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

FFMPEG="${FFMPEG:-$ROOT/build/ffmpeg/bin/ffmpeg}"
FFPROBE="${FFPROBE:-$ROOT/build/ffmpeg/bin/ffprobe}"

BENCH_INPUT="${BENCH_INPUT:-}"
BENCH_SEEKS="${BENCH_SEEKS:-40}"
BENCH_CLIP="${BENCH_CLIP:-0.5}"
BENCH_LATENCY_MS="${BENCH_LATENCY_MS:-0}"

echo "=== benchmark-seek: seek-heavy workload, optimized vs no-cache over 127.0.0.1 ==="
echo ""

if [ ! -x "$FFMPEG" ]; then
	echo "Patched ffmpeg not found at $FFMPEG"
	echo "Run: bash scripts/build-ffmpeg.sh --minimal"
	exit 1
fi

TMPDIR_TEST=$(mktemp -d)
SERVER_PID=""
PROXY_PID=""

cleanup() {
	[ -n "$PROXY_PID" ] && { kill "$PROXY_PID" 2>/dev/null || true; wait "$PROXY_PID" 2>/dev/null || true; }
	[ -n "$SERVER_PID" ] && { kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; }
	rm -rf "$TMPDIR_TEST"
}
trap cleanup EXIT

BIN_DIR="$TMPDIR_TEST/bin"
mkdir -p "$BIN_DIR"

echo "Building server, client, latency proxy..."
go build -o "$BIN_DIR/ffmpeg-over-ip-server" ./cmd/server
go build -o "$BIN_DIR/ffmpeg-over-ip-client" ./cmd/client
go build -o "$BIN_DIR/latency-proxy" ./tests/benchmarks/latency-proxy
ln -sf "$FFMPEG" "$BIN_DIR/ffmpeg"
[ -x "$FFPROBE" ] && ln -sf "$FFPROBE" "$BIN_DIR/ffprobe"

SERVER_PORT=$((20000 + RANDOM % 10000))
PROXY_PORT=$((30000 + RANDOM % 10000))
SERVER_CONFIG="$TMPDIR_TEST/server.jsonc"
READY_CONFIG="$TMPDIR_TEST/client-ready.jsonc"
CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc"

cat > "$SERVER_CONFIG" << CONF
{ "address": "127.0.0.1:$SERVER_PORT", "authSecret": "benchmark-secret" }
CONF
cat > "$READY_CONFIG" << CONF
{ "address": "127.0.0.1:$SERVER_PORT", "authSecret": "benchmark-secret" }
CONF

# The client talks to the latency proxy when latency is requested, else direct.
CLIENT_PORT="$SERVER_PORT"
if [ "$BENCH_LATENCY_MS" != "0" ]; then
	CLIENT_PORT="$PROXY_PORT"
fi
cat > "$CLIENT_CONFIG" << CONF
{ "address": "127.0.0.1:$CLIENT_PORT", "authSecret": "benchmark-secret" }
CONF

echo "Starting server on 127.0.0.1:$SERVER_PORT..."
"$BIN_DIR/ffmpeg-over-ip-server" --config "$SERVER_CONFIG" &
SERVER_PID=$!
sleep 0.5
kill -0 "$SERVER_PID" 2>/dev/null || { echo "FAIL: server did not start"; exit 1; }

if ! FFMPEG_OVER_IP_CLIENT_CONFIG="$READY_CONFIG" \
	"$BIN_DIR/ffmpeg-over-ip-client" -version </dev/null >/dev/null 2>"$TMPDIR_TEST/ready.log"; then
	echo "FAIL: client could not reach server"; cat "$TMPDIR_TEST/ready.log"; exit 1
fi

if [ "$BENCH_LATENCY_MS" != "0" ]; then
	echo "Starting latency proxy on 127.0.0.1:$PROXY_PORT (${BENCH_LATENCY_MS}ms/chunk/direction)..."
	"$BIN_DIR/latency-proxy" \
		--listen "127.0.0.1:$PROXY_PORT" \
		--target "127.0.0.1:$SERVER_PORT" \
		--latency "${BENCH_LATENCY_MS}ms" >"$TMPDIR_TEST/proxy.log" 2>&1 &
	PROXY_PID=$!
	sleep 0.3
	kill -0 "$PROXY_PID" 2>/dev/null || { echo "FAIL: latency proxy did not start"; cat "$TMPDIR_TEST/proxy.log"; exit 1; }
fi

if [ -n "$BENCH_INPUT" ]; then
	INPUT="$BENCH_INPUT"
	[ -f "$INPUT" ] || { echo "BENCH_INPUT does not exist: $INPUT"; exit 1; }
else
	INPUT="$TMPDIR_TEST/input.mkv"
	echo "No BENCH_INPUT set; generating a short synthetic file (use a real file for meaningful numbers)..."
	"$FFMPEG" -hide_banner -loglevel error -nostdin \
		-f lavfi -i "testsrc2=size=320x180:rate=30" -t 20 \
		-c:v rawvideo -f matroska "$INPUT" -y
fi

INPUT_SIZE=$(wc -c < "$INPUT" | tr -d '[:space:]')
INPUT_MIB=$(awk -v b="$INPUT_SIZE" 'BEGIN { printf "%.2f", b / 1048576 }')

# Determine the media duration so we can spread seeks across the whole file.
DURATION=$("$FFPROBE" -v error -show_entries format=duration -of default=nw=1:nk=1 "$INPUT" 2>/dev/null | head -1 || true)
case "$DURATION" in ''|*[!0-9.]*) DURATION=20 ;; esac
echo ""
echo "Input:    $INPUT ($INPUT_MIB MiB, ~${DURATION}s)"
echo "Workload: $BENCH_SEEKS input-seek extractions, ${BENCH_CLIP}s each, -c copy"
[ "$BENCH_LATENCY_MS" != "0" ] && echo "Latency:  ${BENCH_LATENCY_MS}ms/chunk/direction injected on client<->server link"
echo ""

# Build the list of seek timestamps, evenly spaced and avoiding the very ends.
mapfile -t SEEK_TIMES < <(awk -v n="$BENCH_SEEKS" -v d="$DURATION" 'BEGIN {
	lo = d * 0.02; hi = d * 0.95; if (hi <= lo) { lo = 0; hi = d }
	for (i = 0; i < n; i++) printf "%.3f\n", lo + (hi - lo) * (i / n)
}')

# Run all seek extractions through one launcher, time the whole batch.
# $1 label, $2 launcher-cmd... ; reads SEEK_TIMES.
run_scrub() {
	local label="$1"; shift
	local time_file="$TMPDIR_TEST/${label// /_}.time"
	local log_file="$TMPDIR_TEST/${label// /_}.log"
	: > "$log_file"

	printf "%-26s" "$label"
	if ! { TIMEFORMAT='%3R'; time (
		for t in "${SEEK_TIMES[@]}"; do
			"$@" -hide_banner -nostdin -ss "$t" -i "$INPUT" -t "$BENCH_CLIP" \
				-c copy -f null - </dev/null >>"$log_file" 2>&1 || {
					echo "extraction failed at t=$t" >>"$log_file"; exit 1; }
		done
	); } 2>"$time_file"; then
		echo "FAILED"; tail -20 "$log_file"; exit 1
	fi

	local secs; secs=$(awk 'NF { print $1; exit }' "$time_file")
	local per; per=$(awk -v s="$secs" -v n="$BENCH_SEEKS" 'BEGIN { if (n>0) printf "%.1f", s*1000/n; else print "inf" }')
	printf "%9ss  %8s ms/seek\n" "$secs" "$per"
	LAST_SECS="$secs"
}

echo "Results ($BENCH_SEEKS seeks):"
run_scrub "local ffmpeg" "$FFMPEG"
LOCAL_SECS="$LAST_SECS"

run_scrub "over-ip (optimized)" \
	env FFMPEG_OVER_IP_CLIENT_CONFIG="$CLIENT_CONFIG" "$BIN_DIR/ffmpeg-over-ip-client"
OPT_SECS="$LAST_SECS"

run_scrub "over-ip (no-cache)" \
	env FFMPEG_OVER_IP_CLIENT_CONFIG="$CLIENT_CONFIG" \
	FFOIP_READAHEAD_BYTES=0 FFOIP_RANGE_CACHE_BYTES=0 "$BIN_DIR/ffmpeg-over-ip-client"
NOCACHE_SECS="$LAST_SECS"

echo ""
OPT_VS_NOCACHE=$(awk -v o="$OPT_SECS" -v n="$NOCACHE_SECS" 'BEGIN { if (n>0) printf "%.2f", o/n; else print "inf" }')
OPT_VS_LOCAL=$(awk -v o="$OPT_SECS" -v l="$LOCAL_SECS" 'BEGIN { if (l>0) printf "%.2f", o/l; else print "inf" }')
echo "optimized / no-cache wall time: ${OPT_VS_NOCACHE}x   (<=1.00 means the cache layer does not regress seeks)"
echo "optimized / local    wall time: ${OPT_VS_LOCAL}x"
echo ""
echo "Tune with: BENCH_SEEKS, BENCH_CLIP, BENCH_LATENCY_MS, BENCH_INPUT"
