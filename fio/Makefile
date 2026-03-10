CC ?= cc
CFLAGS = -Wall -Wextra -Werror -std=c11 -DFIO_TESTING -D_DEFAULT_SOURCE -D_DARWIN_C_SOURCE
LDFLAGS = -lpthread

SANITIZE_FLAGS = -fsanitize=address,undefined -fno-omit-frame-pointer -g

# Detect clang for static analysis (--analyze is clang-only)
IS_CLANG := $(shell $(CC) --version 2>&1 | grep -qi clang && echo 1)

.PHONY: test wire-test clean

test: fio_test fio_test_sanitize
	@echo "=== unit tests ==="
	./fio_test
	@echo ""
	@echo "=== unit tests (ASan + UBSan) ==="
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
