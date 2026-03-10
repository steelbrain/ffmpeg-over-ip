#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== test-fio-copy: end-to-end file copy via fio ==="

# Build fio_copy
echo "Building fio_copy..."
cd "$ROOT/tests/fio-bins"
cc -Wall -Wextra -Werror -std=c11 -DFIO_TESTING -D_DEFAULT_SOURCE -D_DARWIN_C_SOURCE \
    -o fio_copy -I../../fio ../../fio/fio.c fio_copy.c -lpthread

# Build fio-harness
echo "Building fio-harness..."
cd "$ROOT"
go build -o "$ROOT/tests/integration/harness" \
    ./internal/harness/

# Create temp directory for test files
TMPDIR_TEST=$(mktemp -d)
trap "rm -rf $TMPDIR_TEST" EXIT

# Create a test input file with known content
echo "Creating test input file..."
INPUT="$TMPDIR_TEST/input.bin"
OUTPUT="$TMPDIR_TEST/output.bin"

# Generate 1MB of pseudo-random data (deterministic via seq)
dd if=/dev/urandom of="$INPUT" bs=1024 count=1024 2>/dev/null

echo "Running fio_copy through harness..."
"$ROOT/tests/integration/harness" \
    "$ROOT/tests/fio-bins/fio_copy" "$INPUT" "$OUTPUT"

# Verify output matches input
echo "Verifying output..."
if ! cmp -s "$INPUT" "$OUTPUT"; then
    echo "FAIL: output file does not match input"
    echo "  input size:  $(wc -c < "$INPUT")"
    echo "  output size: $(wc -c < "$OUTPUT")"
    exit 1
fi

SIZE=$(wc -c < "$INPUT" | tr -d ' ')
echo "PASS: copied $SIZE bytes, files match byte-for-byte"

# Test 2: Small text file
echo ""
echo "--- Test 2: small text file ---"
TEXT_INPUT="$TMPDIR_TEST/hello.txt"
TEXT_OUTPUT="$TMPDIR_TEST/hello_copy.txt"
echo "Hello, fio!" > "$TEXT_INPUT"

"$ROOT/tests/integration/harness" \
    "$ROOT/tests/fio-bins/fio_copy" "$TEXT_INPUT" "$TEXT_OUTPUT"

if ! cmp -s "$TEXT_INPUT" "$TEXT_OUTPUT"; then
    echo "FAIL: text file copy mismatch"
    exit 1
fi
echo "PASS: text file copied correctly"

# Test 3: Empty file
echo ""
echo "--- Test 3: empty file ---"
EMPTY_INPUT="$TMPDIR_TEST/empty"
EMPTY_OUTPUT="$TMPDIR_TEST/empty_copy"
touch "$EMPTY_INPUT"

"$ROOT/tests/integration/harness" \
    "$ROOT/tests/fio-bins/fio_copy" "$EMPTY_INPUT" "$EMPTY_OUTPUT"

if [ ! -f "$EMPTY_OUTPUT" ]; then
    echo "FAIL: empty file was not created"
    exit 1
fi
if [ -s "$EMPTY_OUTPUT" ]; then
    echo "FAIL: empty file copy is not empty"
    exit 1
fi
echo "PASS: empty file copied correctly"

echo ""
echo "test-fio-copy: ALL PASSED"
