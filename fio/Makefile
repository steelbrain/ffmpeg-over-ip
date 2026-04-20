CC ?= cc
CFLAGS = -Wall -Wextra -Werror -std=c11 -DFIO_TESTING -D_DEFAULT_SOURCE -D_DARWIN_C_SOURCE
LDFLAGS = -lpthread

# ASan on Apple clang deadlocks during init on macOS 26: its malloc zone hook
# re-enters AsanInitFromRtl from dyld_shared_cache_iterate_text_swift while the
# init mutex is already held, so the process spins on sched_yield forever.
# Drop ASan on that combo and keep UBSan; other hosts (Linux CI, non-Apple
# clang) get both. Override with ENABLE_ASAN=1 to force ASan regardless.
UNAME_S := $(shell uname -s)
IS_APPLE_CLANG := $(shell $(CC) --version 2>&1 | grep -qi "Apple clang" && echo 1)
ifeq ($(ENABLE_ASAN),1)
  SANITIZERS := address,undefined
  SANITIZE_LABEL := ASan + UBSan
else ifeq ($(UNAME_S)-$(IS_APPLE_CLANG),Darwin-1)
  SANITIZERS := undefined
  SANITIZE_LABEL := UBSan (ASan disabled on Apple clang / macOS — set ENABLE_ASAN=1 to override)
else
  SANITIZERS := address,undefined
  SANITIZE_LABEL := ASan + UBSan
endif
SANITIZE_FLAGS = -fsanitize=$(SANITIZERS) -fno-omit-frame-pointer -g

# Detect clang for static analysis (--analyze is clang-only)
IS_CLANG := $(shell $(CC) --version 2>&1 | grep -qi clang && echo 1)

.PHONY: test wire-test clean

test: fio_test fio_test_sanitize
	@echo "=== unit tests ==="
	./fio_test
	@echo ""
	@echo "=== unit tests ($(SANITIZE_LABEL)) ==="
	./fio_test_sanitize
ifeq ($(IS_CLANG),1)
	@echo ""
	@echo "=== static analysis ==="
	$(CC) --analyze $(CFLAGS) fio.c fio_test.c
	@echo "static analysis clean"
else
	@echo ""
	@echo "=== static analysis skipped (clang not detected) ==="
endif

fio_test: fio.c fio_test.c fio.h
	$(CC) $(CFLAGS) -o $@ fio.c fio_test.c $(LDFLAGS)

fio_test_sanitize: fio.c fio_test.c fio.h
	$(CC) $(CFLAGS) $(SANITIZE_FLAGS) -o $@ fio.c fio_test.c $(LDFLAGS)

wire-test: fio_test
	bash ../tests/wire/run.sh

clean:
	rm -f fio_test fio_test_sanitize *.plist
