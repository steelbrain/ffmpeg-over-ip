#!/usr/bin/env bash
#
# test-fio-sanitize.sh — run the fio integration tests with the fio layer
# compiled under AddressSanitizer + UndefinedBehaviorSanitizer.
#
# The fio integration tests (test-fio-copy.sh, test-fio-ops.sh) compile fio.c
# into a standalone binary and drive it through a real socket tunnel against
# the Go harness. Building that binary with sanitizers exercises the real
# networked read-ahead / prefetch / range-cache / seek paths under ASan with
# zero ffmpeg noise, so leak detection (LSan, on Linux) is meaningful here.
#
# The fio unit tests already run under sanitizers via `make -C fio test`; this
# adds the end-to-end tunnel path that the unit tests' in-process mock cannot.
#
# Override sanitizer selection with ENABLE_ASAN=1 (force ASan even on Apple
# clang). On Apple clang / macOS, ASan is disabled by default because it can
# deadlock during init (see fio/Makefile for the gory details); UBSan still
# runs, and Linux CI provides the full ASan+UBSan+LSan coverage.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CC="${CC:-cc}"
UNAME_S="$(uname -s)"
IS_APPLE_CLANG=0
if "$CC" --version 2>&1 | grep -qi "Apple clang"; then IS_APPLE_CLANG=1; fi

if [ "${ENABLE_ASAN:-0}" = "1" ]; then
	SANITIZERS="address,undefined"
elif [ "$UNAME_S" = "Darwin" ] && [ "$IS_APPLE_CLANG" = "1" ]; then
	SANITIZERS="undefined"
	echo "note: ASan disabled on Apple clang/macOS (deadlocks on init); UBSan only."
	echo "      set ENABLE_ASAN=1 to force, or rely on Linux CI for ASan+LSan."
else
	SANITIZERS="address,undefined"
fi

# LSan ships with ASan on Linux but is unsupported on macOS; asking for it there
# (with abort_on_error) makes ASan abort during init, so gate it on platform.
DETECT_LEAKS=1
[ "$UNAME_S" = "Darwin" ] && DETECT_LEAKS=0

export FIO_EXTRA_CFLAGS="-fsanitize=${SANITIZERS} -fno-omit-frame-pointer -g"
export ASAN_OPTIONS="${ASAN_OPTIONS:-detect_leaks=${DETECT_LEAKS}:halt_on_error=1:abort_on_error=1}"
export UBSAN_OPTIONS="${UBSAN_OPTIONS:-halt_on_error=1:abort_on_error=1:print_stacktrace=1}"

echo "=== fio integration tests under sanitizers (${SANITIZERS}) ==="
echo "  FIO_EXTRA_CFLAGS=$FIO_EXTRA_CFLAGS"
echo ""

FAILED=0
for test in test-fio-copy.sh test-fio-ops.sh; do
	echo "========================================"
	echo "Running: $test"
	echo "========================================"
	log="$(mktemp)"
	if bash "$ROOT/tests/integration/$test" 2>&1 | tee "$log"; then
		# A sanitizer abort can still leave a passing-looking script if the
		# error is swallowed, so scan the output explicitly.
		if grep -qiE 'AddressSanitizer|UndefinedBehaviorSanitizer|runtime error:|heap-use-after-free|heap-buffer-overflow|LeakSanitizer|detected memory leaks' "$log"; then
			echo "FAILED (sanitizer diagnostic): $test"
			FAILED=1
		else
			echo "PASSED: $test"
		fi
	else
		echo "FAILED: $test"
		FAILED=1
	fi
	rm -f "$log"
	echo ""
done

if [ "$FAILED" -ne 0 ]; then
	echo "fio sanitizer integration: FAILURES detected"
	exit 1
fi
echo "fio sanitizer integration: all clean"
