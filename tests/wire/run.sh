#!/usr/bin/env bash
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Build C test binary if needed
make -C "$ROOT/fio" fio_test || exit 1

# Start Go server, capture port from first line of stdout
PORT_FILE=$(mktemp)
go run "$ROOT/tests/wire/" > "$PORT_FILE" &
GO_PID=$!
trap "kill $GO_PID 2>/dev/null; rm -f $PORT_FILE" EXIT

# Wait for port to appear
PORT=""
for i in $(seq 1 30); do
    PORT=$(head -1 "$PORT_FILE" 2>/dev/null || true)
    if [ -n "$PORT" ]; then break; fi
    sleep 0.1
done

if [ -z "$PORT" ]; then
    echo "FAIL: Go server did not print port"
    exit 1
fi

# Run C wire test
C_EXIT=0
"$ROOT/fio/fio_test" --wire-test "$PORT" || C_EXIT=$?

# Wait for Go to finish
GO_EXIT=0
wait $GO_PID || GO_EXIT=$?

if [ $C_EXIT -ne 0 ] || [ $GO_EXIT -ne 0 ]; then
    echo "FAIL: C exit=$C_EXIT, Go exit=$GO_EXIT"
    exit 1
fi

echo "wire-test: PASS"
