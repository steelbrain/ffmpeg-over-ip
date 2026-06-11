#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

FFMPEG="${FFMPEG:-$ROOT/build/ffmpeg/bin/ffmpeg}"
FFPROBE="${FFPROBE:-$ROOT/build/ffmpeg/bin/ffprobe}"

BENCH_INPUT="${BENCH_INPUT:-}"
BENCH_DURATION="${BENCH_DURATION:-5}"
BENCH_RATE="${BENCH_RATE:-30}"
BENCH_SIZE="${BENCH_SIZE:-320x180}"
BENCH_PROTOCOL_STATS="${BENCH_PROTOCOL_STATS:-0}"

echo "=== benchmark-localhost: local ffmpeg vs ffmpeg-over-ip over 127.0.0.1 ==="
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
	if [ -n "$PROXY_PID" ]; then
		kill "$PROXY_PID" 2>/dev/null || true
		wait "$PROXY_PID" 2>/dev/null || true
	fi
	if [ -n "$SERVER_PID" ]; then
		kill "$SERVER_PID" 2>/dev/null || true
		wait "$SERVER_PID" 2>/dev/null || true
	fi
	rm -rf "$TMPDIR_TEST"
}
trap cleanup EXIT

BIN_DIR="$TMPDIR_TEST/bin"
mkdir -p "$BIN_DIR"

echo "Building server and client..."
go build -o "$BIN_DIR/ffmpeg-over-ip-server" ./cmd/server
go build -o "$BIN_DIR/ffmpeg-over-ip-client" ./cmd/client
if [ "$BENCH_PROTOCOL_STATS" = "1" ]; then
	go build -o "$BIN_DIR/protocol-stats-proxy" ./tests/benchmarks/protocol-stats-proxy
fi
ln -sf "$FFMPEG" "$BIN_DIR/ffmpeg"
if [ -x "$FFPROBE" ]; then
	ln -sf "$FFPROBE" "$BIN_DIR/ffprobe"
fi

SERVER_PORT=$((20000 + RANDOM % 10000))
PROXY_PORT=$((30000 + RANDOM % 10000))
CLIENT_PORT="$SERVER_PORT"
SERVER_CONFIG="$TMPDIR_TEST/server.jsonc"
CLIENT_CONFIG="$TMPDIR_TEST/client.jsonc"
READY_CLIENT_CONFIG="$TMPDIR_TEST/client-ready.jsonc"
STATS_FILE="$TMPDIR_TEST/protocol-stats.env"

if [ "$BENCH_PROTOCOL_STATS" = "1" ]; then
	CLIENT_PORT="$PROXY_PORT"
fi

cat > "$SERVER_CONFIG" << CONF
{
  "address": "127.0.0.1:$SERVER_PORT",
  "authSecret": "benchmark-secret"
}
CONF

cat > "$CLIENT_CONFIG" << CONF
{
  "address": "127.0.0.1:$CLIENT_PORT",
  "authSecret": "benchmark-secret"
}
CONF

cat > "$READY_CLIENT_CONFIG" << CONF
{
  "address": "127.0.0.1:$SERVER_PORT",
  "authSecret": "benchmark-secret"
}
CONF

echo "Starting server on 127.0.0.1:$SERVER_PORT..."
"$BIN_DIR/ffmpeg-over-ip-server" --config "$SERVER_CONFIG" &
SERVER_PID=$!
sleep 0.5

if ! kill -0 "$SERVER_PID" 2>/dev/null; then
	echo "FAIL: server did not start"
	exit 1
fi

if ! FFMPEG_OVER_IP_CLIENT_CONFIG="$READY_CLIENT_CONFIG" \
	"$BIN_DIR/ffmpeg-over-ip-client" -version </dev/null >/dev/null 2>"$TMPDIR_TEST/client-ready.log"; then
	echo "FAIL: client could not run ffmpeg -version through the server"
	cat "$TMPDIR_TEST/client-ready.log"
	exit 1
fi

if [ "$BENCH_PROTOCOL_STATS" = "1" ]; then
	echo "Starting protocol stats proxy on 127.0.0.1:$PROXY_PORT..."
	"$BIN_DIR/protocol-stats-proxy" \
		--listen "127.0.0.1:$PROXY_PORT" \
		--target "127.0.0.1:$SERVER_PORT" \
		--stats "$STATS_FILE" \
		>"$TMPDIR_TEST/proxy.log" 2>&1 &
	PROXY_PID=$!
	sleep 0.2

	if ! kill -0 "$PROXY_PID" 2>/dev/null; then
		echo "FAIL: protocol stats proxy did not start"
		cat "$TMPDIR_TEST/proxy.log"
		exit 1
	fi
fi

if [ -n "$BENCH_INPUT" ]; then
	INPUT="$BENCH_INPUT"
	if [ ! -f "$INPUT" ]; then
		echo "BENCH_INPUT does not exist: $INPUT"
		exit 1
	fi
else
	INPUT="$TMPDIR_TEST/input.mkv"
	echo "Generating default input: $BENCH_SIZE, ${BENCH_RATE}fps, ${BENCH_DURATION}s rawvideo in Matroska..."
	"$FFMPEG" -hide_banner -loglevel error -nostdin \
		-f lavfi -i "testsrc2=size=$BENCH_SIZE:rate=$BENCH_RATE" \
		-t "$BENCH_DURATION" \
		-c:v rawvideo -f matroska "$INPUT" -y
fi

INPUT_SIZE=$(wc -c < "$INPUT" | tr -d '[:space:]')
INPUT_MIB=$(awk -v bytes="$INPUT_SIZE" 'BEGIN { printf "%.2f", bytes / 1048576 }')
WORKLOAD_ARGS=(-hide_banner -nostdin -i "$INPUT" -c:v copy -c:a copy -f null -)

extract_ffmpeg_speed() {
	tr '\r' '\n' < "$1" |
		awk 'match($0, /speed=[[:space:]]*[^[:space:]]+/) { speed = substr($0, RSTART + 6, RLENGTH - 6) } END { print speed }'
}

stat_value() {
	awk -F= -v key="$1" '$1 == key { print $2; found = 1; exit } END { if (!found) print 0 }' "$STATS_FILE"
}

LAST_SECONDS=""
run_timed() {
	local label="$1"
	local log_file="$2"
	local time_file="$3"
	shift 3

	printf "%-18s" "$label"
	if ! { TIMEFORMAT='%3R'; time "$@" </dev/null >/dev/null 2>"$log_file"; } 2>"$time_file"; then
		echo "FAILED"
		echo "Command failed. Last log lines:"
		tail -40 "$log_file"
		if [ -f "$TMPDIR_TEST/proxy.log" ]; then
			echo ""
			echo "Proxy log:"
			tail -40 "$TMPDIR_TEST/proxy.log"
		fi
		exit 1
	fi

	LAST_SECONDS=$(awk 'NF { print $1; exit }' "$time_file")
	local mib_per_sec
	mib_per_sec=$(awk -v mib="$INPUT_MIB" -v seconds="$LAST_SECONDS" 'BEGIN { if (seconds > 0) printf "%.2f", mib / seconds; else printf "inf" }')
	local speed
	speed=$(extract_ffmpeg_speed "$log_file")

	if [ -n "$speed" ]; then
		printf "%8ss  %10s MiB/s  ffmpeg speed=%s\n" "$LAST_SECONDS" "$mib_per_sec" "$speed"
	else
		printf "%8ss  %10s MiB/s\n" "$LAST_SECONDS" "$mib_per_sec"
	fi
}

echo ""
echo "Input: $INPUT ($INPUT_MIB MiB)"
echo "Workload: ffmpeg ${WORKLOAD_ARGS[*]}"
echo ""
echo "Results:"
run_timed "local ffmpeg" "$TMPDIR_TEST/local.log" "$TMPDIR_TEST/local.time" \
	"$FFMPEG" "${WORKLOAD_ARGS[@]}"
LOCAL_SECONDS="$LAST_SECONDS"

run_timed "ffmpeg-over-ip" "$TMPDIR_TEST/over-ip.log" "$TMPDIR_TEST/over-ip.time" \
	env FFMPEG_OVER_IP_CLIENT_CONFIG="$CLIENT_CONFIG" \
	"$BIN_DIR/ffmpeg-over-ip-client" "${WORKLOAD_ARGS[@]}"
OVER_IP_SECONDS="$LAST_SECONDS"
if [ "$BENCH_PROTOCOL_STATS" = "1" ]; then
	sleep 0.1
fi

RATIO=$(awk -v remote="$OVER_IP_SECONDS" -v local="$LOCAL_SECONDS" 'BEGIN { if (local > 0) printf "%.2f", remote / local; else printf "inf" }')

echo ""
echo "ffmpeg-over-ip wall time / local wall time: ${RATIO}x"
if [ "$BENCH_PROTOCOL_STATS" = "1" ] && [ -s "$STATS_FILE" ]; then
	READ_REQUESTS=$(stat_value read_requests)
	READ_REQUEST_BYTES=$(stat_value read_request_bytes)
	READ_REQUEST_MIN=$(stat_value read_request_min)
	READ_REQUEST_MAX=$(stat_value read_request_max)
	READ_REQUEST_32K=$(stat_value read_request_32768)
	READ_REQUEST_LT_32K=$(stat_value read_request_lt_32768)
	READ_REQUEST_GT_32K=$(stat_value read_request_gt_32768)
	READ_RESPONSES=$(stat_value read_responses)
	READ_RESPONSE_BYTES=$(stat_value read_response_bytes)
	READ_UNIQUE_BYTES=$(stat_value read_unique_bytes)
	READ_REDUNDANT_BYTES=$(stat_value read_redundant_bytes)
	READ_RESPONSE_ZERO=$(stat_value read_response_zero)
	SEEK_REQUESTS=$(stat_value seek_requests)
	FSTAT_REQUESTS=$(stat_value fstat_requests)
	IO_ERRORS=$(stat_value io_errors)

	AVG_REQUEST_BYTES=$(awk -v bytes="$READ_REQUEST_BYTES" -v count="$READ_REQUESTS" 'BEGIN { if (count > 0) printf "%.0f", bytes / count; else printf "0" }')
	AVG_RESPONSE_BYTES=$(awk -v bytes="$READ_RESPONSE_BYTES" -v count="$READ_RESPONSES" 'BEGIN { if (count > 0) printf "%.0f", bytes / count; else printf "0" }')
	READS_PER_SEC=$(awk -v count="$READ_REQUESTS" -v seconds="$OVER_IP_SECONDS" 'BEGIN { if (seconds > 0) printf "%.0f", count / seconds; else printf "0" }')
	RESPONSE_MIB=$(awk -v bytes="$READ_RESPONSE_BYTES" 'BEGIN { printf "%.2f", bytes / 1048576 }')
	UNIQUE_MIB=$(awk -v bytes="$READ_UNIQUE_BYTES" 'BEGIN { printf "%.2f", bytes / 1048576 }')
	REDUNDANT_MIB=$(awk -v bytes="$READ_REDUNDANT_BYTES" 'BEGIN { printf "%.2f", bytes / 1048576 }')

	echo ""
	echo "Protocol read stats:"
	echo "  read requests:      $READ_REQUESTS (${READS_PER_SEC}/sec)"
	echo "  requested bytes:    $READ_REQUEST_BYTES total, $AVG_REQUEST_BYTES avg, min=$READ_REQUEST_MIN, max=$READ_REQUEST_MAX"
	echo "  32 KiB requests:    $READ_REQUEST_32K exact, $READ_REQUEST_LT_32K smaller, $READ_REQUEST_GT_32K larger"
	echo "  read responses:     $READ_RESPONSES, $RESPONSE_MIB MiB returned, $AVG_RESPONSE_BYTES avg, $READ_RESPONSE_ZERO empty"
	echo "  unique/redundant:   $UNIQUE_MIB MiB unique, $REDUNDANT_MIB MiB redundant"
	echo "  seeks/fstats/errors: $SEEK_REQUESTS seeks, $FSTAT_REQUESTS fstats, $IO_ERRORS I/O errors"
else
	echo ""
	echo "Protocol read stats: disabled. Re-run with BENCH_PROTOCOL_STATS=1 to count protocol reads."
fi
echo ""
echo "To run against a real media file from issue #23:"
echo "  BENCH_INPUT=/path/to/input.mp4 bash tests/benchmarks/benchmark-localhost.sh"
echo "To include protocol read counters:"
echo "  BENCH_PROTOCOL_STATS=1 BENCH_INPUT=/path/to/input.mp4 bash tests/benchmarks/benchmark-localhost.sh"
