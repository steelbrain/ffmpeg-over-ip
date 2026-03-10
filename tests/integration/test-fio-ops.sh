#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== test-fio-ops: remaining fio operations end-to-end ==="

# Build fio_ops
echo "Building fio_ops..."
cd "$ROOT/tests/fio-bins"
cc -Wall -Wextra -Werror -std=c11 -DFIO_TESTING -D_DEFAULT_SOURCE -D_DARWIN_C_SOURCE \
    -o fio_ops -I../../fio ../../fio/fio.c fio_ops.c -lpthread

# Build fio-harness
echo "Building fio-harness..."
cd "$ROOT"
go build -o "$ROOT/tests/integration/harness" \
    ./internal/harness/

# Create temp directory for test working area
TMPDIR_TEST=$(mktemp -d)
trap "rm -rf $TMPDIR_TEST" EXIT

echo "Running fio_ops through harness (workdir=$TMPDIR_TEST)..."
echo ""

"$ROOT/tests/integration/harness" \
    "$ROOT/tests/fio-bins/fio_ops" "$TMPDIR_TEST"

echo ""
echo "test-fio-ops: ALL PASSED"
