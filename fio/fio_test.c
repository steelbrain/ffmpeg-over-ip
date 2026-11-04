#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/stat.h>
#include <errno.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <sys/resource.h>
#include <sys/time.h>

/* Pull in the encoding/decoding helpers made visible by -DFIO_TESTING */
#include "fio.h"

/* Declarations for FIO_TESTING-exposed functions from fio.c */
extern int encode_open_req(uint8_t *buf, uint32_t cap,
                           uint16_t req_id, uint16_t file_id,
                           uint32_t flags, uint16_t mode, const char *path);
extern int decode_open_req(const uint8_t *buf, uint32_t len,
                           uint16_t *req_id, uint16_t *file_id,
                           uint32_t *flags, uint16_t *mode,
                           char *path, uint32_t path_cap);

extern int encode_read_req(uint8_t *buf, uint32_t cap,
                           uint16_t req_id, uint16_t file_id, uint32_t nbytes);
extern int decode_read_req(const uint8_t *buf, uint32_t len,
                           uint16_t *req_id, uint16_t *file_id, uint32_t *nbytes);

extern int encode_write_req(uint8_t *buf, uint32_t cap,
                            uint16_t req_id, uint16_t file_id,
                            const uint8_t *data, uint32_t data_len);
extern int decode_write_req(const uint8_t *buf, uint32_t len,
                            uint16_t *req_id, uint16_t *file_id,
                            const uint8_t **data, uint32_t *data_len);

extern int encode_seek_req(uint8_t *buf, uint32_t cap,
                           uint16_t req_id, uint16_t file_id,
                           int64_t offset, uint8_t whence);
extern int decode_seek_req(const uint8_t *buf, uint32_t len,
                           uint16_t *req_id, uint16_t *file_id,
                           int64_t *offset, uint8_t *whence);

extern int encode_close_req(uint8_t *buf, uint32_t cap,
                            uint16_t req_id, uint16_t file_id);
extern int decode_close_req(const uint8_t *buf, uint32_t len,
                            uint16_t *req_id, uint16_t *file_id);

extern int encode_fstat_req(uint8_t *buf, uint32_t cap,
                            uint16_t req_id, uint16_t file_id);
extern int decode_fstat_req(const uint8_t *buf, uint32_t len,
                            uint16_t *req_id, uint16_t *file_id);

extern int encode_ftruncate_req(uint8_t *buf, uint32_t cap,
                                uint16_t req_id, uint16_t file_id, int64_t length);
extern int decode_ftruncate_req(const uint8_t *buf, uint32_t len,
                                uint16_t *req_id, uint16_t *file_id, int64_t *length);

extern int encode_unlink_req(uint8_t *buf, uint32_t cap,
                             uint16_t req_id, const char *path);
extern int decode_unlink_req(const uint8_t *buf, uint32_t len,
                             uint16_t *req_id, char *path, uint32_t path_cap);

extern int encode_rename_req(uint8_t *buf, uint32_t cap,
                             uint16_t req_id, const char *old_path, const char *new_path);
extern int decode_rename_req(const uint8_t *buf, uint32_t len,
                             uint16_t *req_id,
                             char *old_path, uint32_t old_cap,
                             char *new_path, uint32_t new_cap);

extern int encode_mkdir_req(uint8_t *buf, uint32_t cap,
                            uint16_t req_id, uint16_t mode, const char *path);
extern int decode_mkdir_req(const uint8_t *buf, uint32_t len,
                            uint16_t *req_id, uint16_t *mode,
                            char *path, uint32_t path_cap);

extern int encode_open_ok(uint8_t *buf, uint32_t cap,
                          uint16_t req_id, int64_t file_size, uint32_t mode);
extern int decode_open_ok(const uint8_t *buf, uint32_t len,
                          uint16_t *req_id, int64_t *file_size, uint32_t *mode);

extern int encode_read_ok(uint8_t *buf, uint32_t cap,
                          uint16_t req_id, const uint8_t *data, uint32_t data_len);
extern int decode_read_ok(const uint8_t *buf, uint32_t len,
                          uint16_t *req_id, const uint8_t **data, uint32_t *data_len);

extern int encode_write_ok(uint8_t *buf, uint32_t cap,
                           uint16_t req_id, uint32_t bytes_written);
extern int decode_write_ok(const uint8_t *buf, uint32_t len,
                           uint16_t *req_id, uint32_t *bytes_written);

extern int encode_seek_ok(uint8_t *buf, uint32_t cap,
                          uint16_t req_id, int64_t offset);
extern int decode_seek_ok(const uint8_t *buf, uint32_t len,
                          uint16_t *req_id, int64_t *offset);

extern int encode_reqid_resp(uint8_t *buf, uint32_t cap, uint16_t req_id);
extern int decode_reqid_resp(const uint8_t *buf, uint32_t len, uint16_t *req_id);

extern int encode_fstat_ok(uint8_t *buf, uint32_t cap,
                           uint16_t req_id, int64_t file_size, uint32_t mode);
extern int decode_fstat_ok(const uint8_t *buf, uint32_t len,
                           uint16_t *req_id, int64_t *file_size, uint32_t *mode);

extern int encode_io_error(uint8_t *buf, uint32_t cap,
                           uint16_t req_id, int32_t err);
extern int decode_io_error(const uint8_t *buf, uint32_t len,
                           uint16_t *req_id, int32_t *err);

/* Test-only tunnel injection hooks (FIO_TESTING) */
extern int fio_test_set_tunnel(int sock_fd, uint32_t read_ahead_bytes,
                               int read_ahead_explicit, uint32_t range_cache_max_bytes);
extern void fio_test_teardown(void);
extern int fio_test_prefetch_in_flight(int fd);
extern int fio_test_prefetch_req_id(int fd);
extern void fio_test_set_next_req_id(uint16_t id);
extern void fio_test_set_next_file_id(uint16_t id);
extern int fio_test_pending_used(void);
extern int fio_test_tunnel_nodelay(void);
extern uint64_t fio_test_range_cache_total(void);
extern int fio_test_max_pending(void);
extern int fio_test_max_files(void);
extern uint32_t fio_test_max_io_bytes(void);
extern int fio_test_init_from_env(int *dead);

extern unsigned int mode_from_wire(uint32_t wire);
extern int encode_path_req(uint8_t *buf, uint32_t cap, uint16_t req_id, const char *path);
extern int encode_stat_ok(uint8_t *buf, uint32_t cap, uint16_t req_id,
                          int64_t size, uint32_t mode, int64_t mtime, uint8_t access_bits);
extern int decode_stat_ok(const uint8_t *buf, uint32_t len, uint16_t *req_id,
                          int64_t *size, uint32_t *mode, int64_t *mtime, uint8_t *access_bits);
extern int encode_listdir_ok_header(uint8_t *buf, uint32_t cap, uint16_t req_id, uint32_t count);
extern int encode_listdir_entry(uint8_t *buf, uint32_t cap, const char *name,
                                uint32_t mode, int64_t size, int64_t mtime);
extern int decode_listdir_header(const uint8_t *buf, uint32_t len, uint16_t *req_id, uint32_t *count);
extern int decode_listdir_entry(const uint8_t *buf, uint32_t len, uint32_t *pos,
                                const uint8_t **name, uint16_t *name_len, uint32_t *mode,
                                int64_t *size, int64_t *mtime);

#include <pthread.h>

/* Big-endian helpers for envelope tests */
static inline void test_put_u32(uint8_t *buf, uint32_t v) {
    buf[0] = (uint8_t)(v >> 24); buf[1] = (uint8_t)(v >> 16);
    buf[2] = (uint8_t)(v >> 8);  buf[3] = (uint8_t)(v);
}
static inline uint32_t test_get_u32(const uint8_t *buf) {
    return ((uint32_t)buf[0] << 24) | ((uint32_t)buf[1] << 16) |
           ((uint32_t)buf[2] << 8)  | (uint32_t)buf[3];
}

/* Test framework */
static int tests_run = 0;
static int tests_passed = 0;

#define TEST(name) static int test_##name(void)
#define RUN(name) do { \
    tests_run++; \
    printf("  %-60s ", #name); \
    if (test_##name() == 0) { tests_passed++; printf("PASS\n"); } \
    else { printf("FAIL\n"); } \
} while(0)
#define ASSERT(cond) do { if (!(cond)) { printf("ASSERT FAILED: %s (line %d) ", #cond, __LINE__); return 1; } } while(0)
#define ASSERT_EQ(a, b) do { if ((a) != (b)) { printf("ASSERT_EQ FAILED: %s=%lld != %s=%lld (line %d) ", #a, (long long)(a), #b, (long long)(b), __LINE__); return 1; } } while(0)
#define ASSERT_STR_EQ(a, b) do { if (strcmp((a), (b)) != 0) { printf("ASSERT_STR_EQ FAILED: \"%s\" != \"%s\" (line %d) ", (a), (b), __LINE__); return 1; } } while(0)
#define ASSERT_MEM_EQ(a, b, n) do { if (memcmp((a), (b), (n)) != 0) { printf("ASSERT_MEM_EQ FAILED (line %d) ", __LINE__); return 1; } } while(0)

/* ======================================================================
 * 1. Encode/decode round-trips
 * ====================================================================== */

TEST(open_req_roundtrip) {
    uint8_t buf[256];
    int n = encode_open_req(buf, sizeof(buf), 1, 42, 0x0241, 0x01B6, "/tmp/test/output.ts");
    ASSERT(n > 0);

    uint16_t req_id, file_id, mode;
    uint32_t flags;
    char path[256];
    ASSERT(decode_open_req(buf, (uint32_t)n, &req_id, &file_id, &flags, &mode, path, sizeof(path)) == 0);
    ASSERT_EQ(req_id, 1);
    ASSERT_EQ(file_id, 42);
    ASSERT_EQ(flags, 0x0241);
    ASSERT_EQ(mode, 0x01B6);
    ASSERT_STR_EQ(path, "/tmp/test/output.ts");
    return 0;
}

TEST(read_req_roundtrip) {
    uint8_t buf[16];
    encode_read_req(buf, sizeof(buf), 5, 1, 65536);

    uint16_t req_id, file_id;
    uint32_t nbytes;
    ASSERT(decode_read_req(buf, 8, &req_id, &file_id, &nbytes) == 0);
    ASSERT_EQ(req_id, 5);
    ASSERT_EQ(file_id, 1);
    ASSERT_EQ(nbytes, 65536);
    return 0;
}

TEST(write_req_roundtrip) {
    uint8_t buf[256];
    const uint8_t data[] = "hello video data";
    int n = encode_write_req(buf, sizeof(buf), 10, 2, data, (uint32_t)strlen((const char *)data));
    ASSERT(n > 0);

    uint16_t req_id, file_id;
    const uint8_t *dec_data;
    uint32_t dec_len;
    ASSERT(decode_write_req(buf, (uint32_t)n, &req_id, &file_id, &dec_data, &dec_len) == 0);
    ASSERT_EQ(req_id, 10);
    ASSERT_EQ(file_id, 2);
    ASSERT_EQ(dec_len, (uint32_t)strlen((const char *)data));
    ASSERT_MEM_EQ(dec_data, data, dec_len);
    return 0;
}

TEST(seek_req_roundtrip) {
    uint8_t buf[16];
    encode_seek_req(buf, sizeof(buf), 7, 1, -1024, 2);

    uint16_t req_id, file_id;
    int64_t offset;
    uint8_t whence;
    ASSERT(decode_seek_req(buf, 13, &req_id, &file_id, &offset, &whence) == 0);
    ASSERT_EQ(req_id, 7);
    ASSERT_EQ(file_id, 1);
    ASSERT_EQ(offset, -1024);
    ASSERT_EQ(whence, 2);
    return 0;
}

TEST(close_req_roundtrip) {
    uint8_t buf[8];
    encode_close_req(buf, sizeof(buf), 100, 50);

    uint16_t req_id, file_id;
    ASSERT(decode_close_req(buf, 4, &req_id, &file_id) == 0);
    ASSERT_EQ(req_id, 100);
    ASSERT_EQ(file_id, 50);
    return 0;
}

TEST(fstat_req_roundtrip) {
    uint8_t buf[8];
    encode_fstat_req(buf, sizeof(buf), 3, 7);

    uint16_t req_id, file_id;
    ASSERT(decode_fstat_req(buf, 4, &req_id, &file_id) == 0);
    ASSERT_EQ(req_id, 3);
    ASSERT_EQ(file_id, 7);
    return 0;
}

TEST(ftruncate_req_roundtrip) {
    uint8_t buf[16];
    encode_ftruncate_req(buf, sizeof(buf), 20, 5, 1048576);

    uint16_t req_id, file_id;
    int64_t length;
    ASSERT(decode_ftruncate_req(buf, 12, &req_id, &file_id, &length) == 0);
    ASSERT_EQ(req_id, 20);
    ASSERT_EQ(file_id, 5);
    ASSERT_EQ(length, 1048576);
    return 0;
}

TEST(unlink_req_roundtrip) {
    uint8_t buf[256];
    int n = encode_unlink_req(buf, sizeof(buf), 15, "/tmp/old-segment.ts");
    ASSERT(n > 0);

    uint16_t req_id;
    char path[256];
    ASSERT(decode_unlink_req(buf, (uint32_t)n, &req_id, path, sizeof(path)) == 0);
    ASSERT_EQ(req_id, 15);
    ASSERT_STR_EQ(path, "/tmp/old-segment.ts");
    return 0;
}

TEST(rename_req_roundtrip) {
    uint8_t buf[256];
    int n = encode_rename_req(buf, sizeof(buf), 30,
                              "/tmp/stream.m3u8.tmp", "/tmp/stream.m3u8");
    ASSERT(n > 0);

    uint16_t req_id;
    char old_path[256], new_path[256];
    ASSERT(decode_rename_req(buf, (uint32_t)n, &req_id,
                             old_path, sizeof(old_path),
                             new_path, sizeof(new_path)) == 0);
    ASSERT_EQ(req_id, 30);
    ASSERT_STR_EQ(old_path, "/tmp/stream.m3u8.tmp");
    ASSERT_STR_EQ(new_path, "/tmp/stream.m3u8");
    return 0;
}

TEST(mkdir_req_roundtrip) {
    uint8_t buf[256];
    int n = encode_mkdir_req(buf, sizeof(buf), 8, 0755, "/tmp/2024/01/15");
    ASSERT(n > 0);

    uint16_t req_id, mode;
    char path[256];
    ASSERT(decode_mkdir_req(buf, (uint32_t)n, &req_id, &mode, path, sizeof(path)) == 0);
    ASSERT_EQ(req_id, 8);
    ASSERT_EQ(mode, 0755);
    ASSERT_STR_EQ(path, "/tmp/2024/01/15");
    return 0;
}

/* Response round-trips */

TEST(open_ok_roundtrip) {
    uint8_t buf[16];
    ASSERT(encode_open_ok(buf, sizeof(buf), 1, 524288000, 0010640) == 14);

    uint16_t req_id;
    int64_t file_size;
    uint32_t mode;
    ASSERT(decode_open_ok(buf, 14, &req_id, &file_size, &mode) == 0);
    ASSERT_EQ(req_id, 1);
    ASSERT_EQ(file_size, 524288000);
    ASSERT_EQ(mode, 0010640);
    ASSERT(decode_open_ok(buf, 10, &req_id, &file_size, &mode) == -1); /* no mode */
    return 0;
}

TEST(read_ok_roundtrip) {
    uint8_t buf[128];
    uint8_t data[64];
    for (int i = 0; i < 64; i++) data[i] = (uint8_t)(i % 256);
    int n = encode_read_ok(buf, sizeof(buf), 5, data, 64);
    ASSERT(n == 66);

    uint16_t req_id;
    const uint8_t *dec_data;
    uint32_t dec_len;
    ASSERT(decode_read_ok(buf, (uint32_t)n, &req_id, &dec_data, &dec_len) == 0);
    ASSERT_EQ(req_id, 5);
    ASSERT_EQ(dec_len, 64);
    ASSERT_MEM_EQ(dec_data, data, 64);
    return 0;
}

TEST(read_ok_eof) {
    uint8_t buf[8];
    int n = encode_read_ok(buf, sizeof(buf), 5, NULL, 0);
    ASSERT(n == 2);

    uint16_t req_id;
    const uint8_t *dec_data;
    uint32_t dec_len;
    ASSERT(decode_read_ok(buf, (uint32_t)n, &req_id, &dec_data, &dec_len) == 0);
    ASSERT_EQ(req_id, 5);
    ASSERT_EQ(dec_len, 0);
    return 0;
}

TEST(write_ok_roundtrip) {
    uint8_t buf[8];
    encode_write_ok(buf, sizeof(buf), 10, 65536);

    uint16_t req_id;
    uint32_t written;
    ASSERT(decode_write_ok(buf, 6, &req_id, &written) == 0);
    ASSERT_EQ(req_id, 10);
    ASSERT_EQ(written, 65536);
    return 0;
}

TEST(seek_ok_roundtrip) {
    uint8_t buf[16];
    encode_seek_ok(buf, sizeof(buf), 7, 1048576);

    uint16_t req_id;
    int64_t offset;
    ASSERT(decode_seek_ok(buf, 10, &req_id, &offset) == 0);
    ASSERT_EQ(req_id, 7);
    ASSERT_EQ(offset, 1048576);
    return 0;
}

TEST(reqid_resp_roundtrip) {
    uint8_t buf[4];
    encode_reqid_resp(buf, sizeof(buf), 42);

    uint16_t req_id;
    ASSERT(decode_reqid_resp(buf, 2, &req_id) == 0);
    ASSERT_EQ(req_id, 42);
    return 0;
}

TEST(fstat_ok_roundtrip) {
    uint8_t buf[16];
    encode_fstat_ok(buf, sizeof(buf), 3, 999999, 0100644);

    uint16_t req_id;
    int64_t file_size;
    uint32_t mode;
    ASSERT(decode_fstat_ok(buf, 14, &req_id, &file_size, &mode) == 0);
    ASSERT_EQ(req_id, 3);
    ASSERT_EQ(file_size, 999999);
    ASSERT_EQ(mode, 0100644);
    return 0;
}

TEST(io_error_roundtrip) {
    uint8_t buf[8];
    encode_io_error(buf, sizeof(buf), 1, 2); /* ENOENT */

    uint16_t req_id;
    int32_t err;
    ASSERT(decode_io_error(buf, 6, &req_id, &err) == 0);
    ASSERT_EQ(req_id, 1);
    ASSERT_EQ(err, 2);
    return 0;
}

/* ======================================================================
 * 2. Byte layout verification (must match Go test vectors)
 * ====================================================================== */

TEST(open_req_byte_layout) {
    /* Go test: req_id=0x0102, file_id=0x0304, flags=0x00000241, mode=0x01B6, path="AB" */
    uint8_t buf[64];
    int n = encode_open_req(buf, sizeof(buf), 0x0102, 0x0304, 0x00000241, 0x01B6, "AB");
    ASSERT_EQ(n, 12);

    const uint8_t expected[] = {
        0x01, 0x02,             /* request ID */
        0x03, 0x04,             /* file ID */
        0x00, 0x00, 0x02, 0x41, /* flags */
        0x01, 0xB6,             /* mode */
        'A', 'B'                /* path */
    };
    ASSERT_MEM_EQ(buf, expected, 12);
    return 0;
}

TEST(rename_req_byte_layout) {
    /* Go test: req_id=0x0001, old="AB", new="CD" */
    uint8_t buf[64];
    int n = encode_rename_req(buf, sizeof(buf), 0x0001, "AB", "CD");
    ASSERT_EQ(n, 8);

    const uint8_t expected[] = {
        0x00, 0x01, /* request ID */
        0x00, 0x02, /* old path length = 2 */
        'A', 'B',   /* old path */
        'C', 'D'    /* new path */
    };
    ASSERT_MEM_EQ(buf, expected, 8);
    return 0;
}

TEST(envelope_byte_layout) {
    /* Go test: type=0x42, payload={0xDE, 0xAD}
     * Expected: {42 00 00 00 02 DE AD} */
    uint8_t envelope[16];
    uint8_t payload[] = {0xDE, 0xAD};

    /* Manual envelope construction (matching Go WriteMessageTo) */
    envelope[0] = 0x42;
    test_put_u32(envelope + 1, 2);
    memcpy(envelope + 5, payload, 2);

    ASSERT_EQ(envelope[0], 0x42);
    ASSERT_EQ(test_get_u32(envelope + 1), 2);
    ASSERT_EQ(envelope[5], 0xDE);
    ASSERT_EQ(envelope[6], 0xAD);

    /* Verify total is 7 bytes: {42 00 00 00 02 DE AD} */
    const uint8_t expected[] = {0x42, 0x00, 0x00, 0x00, 0x02, 0xDE, 0xAD};
    ASSERT_MEM_EQ(envelope, expected, 7);
    return 0;
}

/* ======================================================================
 * 3. Short payload rejection
 * ====================================================================== */

TEST(short_payload_open_req) {
    uint16_t a; uint32_t b; char c[8];
    ASSERT(decode_open_req(NULL, 0, &a, &a, &b, &a, c, 8) == -1);
    uint8_t one = 0;
    ASSERT(decode_open_req(&one, 1, &a, &a, &b, &a, c, 8) == -1);
    return 0;
}

TEST(short_payload_read_req) {
    uint16_t a; uint32_t b;
    ASSERT(decode_read_req(NULL, 0, &a, &a, &b) == -1);
    uint8_t one = 0;
    ASSERT(decode_read_req(&one, 1, &a, &a, &b) == -1);
    return 0;
}

TEST(short_payload_write_req) {
    uint16_t a; const uint8_t *d; uint32_t dl;
    ASSERT(decode_write_req(NULL, 0, &a, &a, &d, &dl) == -1);
    uint8_t one = 0;
    ASSERT(decode_write_req(&one, 1, &a, &a, &d, &dl) == -1);
    return 0;
}

TEST(short_payload_seek_req) {
    uint16_t a; int64_t b; uint8_t c;
    ASSERT(decode_seek_req(NULL, 0, &a, &a, &b, &c) == -1);
    uint8_t one = 0;
    ASSERT(decode_seek_req(&one, 1, &a, &a, &b, &c) == -1);
    return 0;
}

TEST(short_payload_close_req) {
    uint16_t a;
    ASSERT(decode_close_req(NULL, 0, &a, &a) == -1);
    uint8_t one = 0;
    ASSERT(decode_close_req(&one, 1, &a, &a) == -1);
    return 0;
}

TEST(short_payload_fstat_req) {
    uint16_t a;
    ASSERT(decode_fstat_req(NULL, 0, &a, &a) == -1);
    uint8_t one = 0;
    ASSERT(decode_fstat_req(&one, 1, &a, &a) == -1);
    return 0;
}

TEST(short_payload_ftruncate_req) {
    uint16_t a; int64_t b;
    ASSERT(decode_ftruncate_req(NULL, 0, &a, &a, &b) == -1);
    uint8_t one = 0;
    ASSERT(decode_ftruncate_req(&one, 1, &a, &a, &b) == -1);
    return 0;
}

TEST(short_payload_unlink_req) {
    uint16_t a; char p[8];
    ASSERT(decode_unlink_req(NULL, 0, &a, p, 8) == -1);
    uint8_t one = 0;
    ASSERT(decode_unlink_req(&one, 1, &a, p, 8) == -1);
    return 0;
}

TEST(short_payload_rename_req) {
    uint16_t a; char o[8], n[8];
    ASSERT(decode_rename_req(NULL, 0, &a, o, 8, n, 8) == -1);
    uint8_t one = 0;
    ASSERT(decode_rename_req(&one, 1, &a, o, 8, n, 8) == -1);
    return 0;
}

TEST(short_payload_mkdir_req) {
    uint16_t a; char p[8];
    ASSERT(decode_mkdir_req(NULL, 0, &a, &a, p, 8) == -1);
    uint8_t one = 0;
    ASSERT(decode_mkdir_req(&one, 1, &a, &a, p, 8) == -1);
    return 0;
}

TEST(short_payload_open_ok) {
    uint16_t a; int64_t b; uint32_t m;
    ASSERT(decode_open_ok(NULL, 0, &a, &b, &m) == -1);
    uint8_t one = 0;
    ASSERT(decode_open_ok(&one, 1, &a, &b, &m) == -1);
    return 0;
}

TEST(short_payload_read_ok) {
    uint16_t a; const uint8_t *d; uint32_t dl;
    ASSERT(decode_read_ok(NULL, 0, &a, &d, &dl) == -1);
    uint8_t one = 0;
    ASSERT(decode_read_ok(&one, 1, &a, &d, &dl) == -1);
    return 0;
}

TEST(short_payload_write_ok) {
    uint16_t a; uint32_t b;
    ASSERT(decode_write_ok(NULL, 0, &a, &b) == -1);
    uint8_t one = 0;
    ASSERT(decode_write_ok(&one, 1, &a, &b) == -1);
    return 0;
}

TEST(short_payload_seek_ok) {
    uint16_t a; int64_t b;
    ASSERT(decode_seek_ok(NULL, 0, &a, &b) == -1);
    uint8_t one = 0;
    ASSERT(decode_seek_ok(&one, 1, &a, &b) == -1);
    return 0;
}

TEST(short_payload_reqid_resp) {
    uint16_t a;
    ASSERT(decode_reqid_resp(NULL, 0, &a) == -1);
    uint8_t one = 0;
    ASSERT(decode_reqid_resp(&one, 1, &a) == -1);
    return 0;
}

TEST(short_payload_fstat_ok) {
    uint16_t a; int64_t b; uint32_t c;
    ASSERT(decode_fstat_ok(NULL, 0, &a, &b, &c) == -1);
    uint8_t one = 0;
    ASSERT(decode_fstat_ok(&one, 1, &a, &b, &c) == -1);
    return 0;
}

TEST(short_payload_io_error) {
    uint16_t a; int32_t b;
    ASSERT(decode_io_error(NULL, 0, &a, &b) == -1);
    uint8_t one = 0;
    ASSERT(decode_io_error(&one, 1, &a, &b) == -1);
    return 0;
}

/* ======================================================================
 * 4. Signed int64 round-trip
 * ====================================================================== */

TEST(seek_negative_offset) {
    uint8_t buf[16];
    encode_seek_req(buf, sizeof(buf), 1, 1, -9999999999LL, 1);

    uint16_t req_id, file_id;
    int64_t offset;
    uint8_t whence;
    ASSERT(decode_seek_req(buf, 13, &req_id, &file_id, &offset, &whence) == 0);
    ASSERT_EQ(offset, -9999999999LL);
    return 0;
}

TEST(ftruncate_negative_length) {
    /* While negative truncate lengths are unusual, the encoding must survive */
    uint8_t buf[16];
    encode_ftruncate_req(buf, sizeof(buf), 1, 1, -42);

    uint16_t req_id, file_id;
    int64_t length;
    ASSERT(decode_ftruncate_req(buf, 12, &req_id, &file_id, &length) == 0);
    ASSERT_EQ(length, -42);
    return 0;
}

/* ======================================================================
 * 5. Flag translation
 * ====================================================================== */

/* flags_to_wire is static in fio.c; we test it indirectly via encode_open_req
 * by checking that the expected wire value appears in the encoded output. */

TEST(flag_translation_wronly_creat_trunc) {
    /* Platform O_WRONLY|O_CREAT|O_TRUNC must become wire 0x0241 */
    uint8_t buf[64];
    /* We test the wire-level by encoding with known wire values directly */
    uint32_t wire = 0x0001 | 0x0040 | 0x0200; /* FIO_O_WRONLY | FIO_O_CREAT | FIO_O_TRUNC */
    ASSERT_EQ(wire, 0x0241);

    int n = encode_open_req(buf, sizeof(buf), 1, 1, wire, 0666, "/test");
    ASSERT(n > 0);

    /* Verify bytes 4-7 are 0x00000241 */
    ASSERT_EQ(buf[4], 0x00);
    ASSERT_EQ(buf[5], 0x00);
    ASSERT_EQ(buf[6], 0x02);
    ASSERT_EQ(buf[7], 0x41);
    return 0;
}

/* ======================================================================
 * 6. Passthrough mode (FFOIP_PORT unset)
 * ====================================================================== */

TEST(passthrough_file_ops) {
    /* This test uses the fio_* functions in passthrough mode.
     * FFOIP_PORT must be unset for this to work. */
    unsetenv("FFOIP_PORT");

    char tmppath[] = "/tmp/fio_test_XXXXXX";
    int fd = mkstemp(tmppath);
    ASSERT(fd >= 0);
    close(fd); /* close the mkstemp fd, we'll reopen with fio */

    /* Open for writing */
    fd = fio_open(tmppath, O_WRONLY | O_TRUNC, 0644);
    ASSERT(fd >= 0);

    /* Write */
    const char *msg = "hello fio";
    ssize_t nw = fio_write(fd, msg, strlen(msg));
    ASSERT_EQ(nw, (ssize_t)strlen(msg));

    /* Close */
    ASSERT_EQ(fio_close(fd), 0);

    /* Reopen for reading */
    fd = fio_open(tmppath, O_RDONLY, 0);
    ASSERT(fd >= 0);

    /* Read */
    char rbuf[64];
    ssize_t nr = fio_read(fd, rbuf, sizeof(rbuf));
    ASSERT_EQ(nr, (ssize_t)strlen(msg));
    ASSERT_MEM_EQ(rbuf, msg, (size_t)nr);

    /* Seek to beginning */
    off_t pos = fio_lseek(fd, 0, SEEK_SET);
    ASSERT_EQ(pos, 0);

    /* Fstat */
    struct stat st;
    ASSERT_EQ(fio_fstat(fd, &st), 0);
    ASSERT_EQ(st.st_size, (off_t)strlen(msg));

    /* Close */
    ASSERT_EQ(fio_close(fd), 0);

    /* Unlink */
    ASSERT_EQ(fio_unlink(tmppath), 0);

    /* Verify unlink worked */
    ASSERT(access(tmppath, F_OK) != 0);

    return 0;
}

TEST(passthrough_mkdir_rename) {
    unsetenv("FFOIP_PORT");

    char dir1[] = "/tmp/fio_test_dir1_XXXXXX";
    char *result = mkdtemp(dir1);
    ASSERT(result != NULL);

    /* Remove it so we can test fio_mkdir */
    rmdir(dir1);

    ASSERT_EQ(fio_mkdir(dir1, 0755), 0);

    struct stat st;
    ASSERT(stat(dir1, &st) == 0);
    ASSERT(S_ISDIR(st.st_mode));

    /* Create a file in the directory */
    char filepath[512];
    snprintf(filepath, sizeof(filepath), "%s/test.txt", dir1);
    int fd = fio_open(filepath, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    ASSERT(fd >= 0);
    fio_write(fd, "x", 1);
    fio_close(fd);

    /* Rename */
    char filepath2[512];
    snprintf(filepath2, sizeof(filepath2), "%s/test2.txt", dir1);
    ASSERT_EQ(fio_rename(filepath, filepath2), 0);

    /* Verify rename worked */
    ASSERT(access(filepath, F_OK) != 0);
    ASSERT(access(filepath2, F_OK) == 0);

    /* Cleanup */
    unlink(filepath2);
    rmdir(dir1);
    return 0;
}

TEST(passthrough_ftruncate) {
    unsetenv("FFOIP_PORT");

    char tmppath[] = "/tmp/fio_trunc_XXXXXX";
    int fd = mkstemp(tmppath);
    ASSERT(fd >= 0);
    close(fd);

    fd = fio_open(tmppath, O_WRONLY, 0);
    ASSERT(fd >= 0);
    fio_write(fd, "hello world", 11);

    ASSERT_EQ(fio_ftruncate(fd, 5), 0);
    fio_close(fd);

    /* Verify size */
    struct stat st;
    stat(tmppath, &st);
    ASSERT_EQ(st.st_size, 5);

    unlink(tmppath);
    return 0;
}

/* ======================================================================
 * 7. Virtual FD allocation
 * ====================================================================== */

/* Virtual fds are tested in tunnel mode below (tunnel_real_fds_pass_through).
 * Here: in passthrough mode fio_open returns a real kernel fd. */

TEST(vfd_constants) {
    char tmppath[] = "/tmp/fio_test_vfd_XXXXXX";
    int tmp = mkstemp(tmppath);
    ASSERT(tmp >= 0);
    close(tmp);
    int fd = fio_open(tmppath, O_RDONLY, 0);
    ASSERT(fd >= 0 && fd < 0x40000000);
    ASSERT(fio_close(fd) == 0);
    unlink(tmppath);
    return 0;
}

/* ======================================================================
 * 8. UTF-8 path round-trip
 * ====================================================================== */

TEST(utf8_path_roundtrip) {
    const char *path = "/media/\xe6\x98\xa0\xe7\x94\xbb/\xe3\x83\x86\xe3\x82\xb9\xe3\x83\x88.mkv";
    uint8_t buf[256];
    int n = encode_open_req(buf, sizeof(buf), 1, 1, 0, 0, path);
    ASSERT(n > 0);

    uint16_t req_id, file_id, mode;
    uint32_t flags;
    char dec_path[256];
    ASSERT(decode_open_req(buf, (uint32_t)n, &req_id, &file_id, &flags, &mode,
                           dec_path, sizeof(dec_path)) == 0);
    ASSERT_STR_EQ(dec_path, path);
    return 0;
}

/* ======================================================================
 * 9. Buffer capacity / boundary tests
 * ====================================================================== */

TEST(encode_open_req_tiny_buffer) {
    /* Buffer too small to hold the encoded open request */
    uint8_t buf[4]; /* needs at least 10 + path len */
    int n = encode_open_req(buf, sizeof(buf), 1, 1, 0, 0, "/test");
    ASSERT(n < 0); /* should fail */
    return 0;
}

TEST(encode_unlink_req_tiny_buffer) {
    uint8_t buf[2]; /* needs at least 2 + path len */
    int n = encode_unlink_req(buf, sizeof(buf), 1, "/some/path");
    ASSERT(n < 0);
    return 0;
}

TEST(encode_rename_req_tiny_buffer) {
    uint8_t buf[2]; /* needs at least 4 + oldLen + newLen */
    int n = encode_rename_req(buf, sizeof(buf), 1, "/old", "/new");
    ASSERT(n < 0);
    return 0;
}

TEST(encode_mkdir_req_tiny_buffer) {
    uint8_t buf[2]; /* needs at least 4 + path len */
    int n = encode_mkdir_req(buf, sizeof(buf), 1, 0755, "/dir");
    ASSERT(n < 0);
    return 0;
}

TEST(encode_write_req_tiny_buffer) {
    uint8_t buf[2]; /* needs at least 4 + data len */
    uint8_t data[] = "hello";
    int n = encode_write_req(buf, sizeof(buf), 1, 1, data, 5);
    ASSERT(n < 0);
    return 0;
}

TEST(encode_read_ok_tiny_buffer) {
    uint8_t buf[1]; /* needs at least 2 + data len */
    uint8_t data[] = "hi";
    int n = encode_read_ok(buf, sizeof(buf), 1, data, 2);
    ASSERT(n < 0);
    return 0;
}

TEST(zero_length_write_data) {
    /* Encode a write request with zero-length data */
    uint8_t buf[64];
    int n = encode_write_req(buf, sizeof(buf), 42, 7, NULL, 0);
    ASSERT(n > 0);

    uint16_t req_id, file_id;
    const uint8_t *data;
    uint32_t data_len;
    ASSERT(decode_write_req(buf, (uint32_t)n, &req_id, &file_id, &data, &data_len) == 0);
    ASSERT_EQ(req_id, 42);
    ASSERT_EQ(file_id, 7);
    ASSERT_EQ(data_len, 0);
    return 0;
}

TEST(max_uint16_ids) {
    /* Encode/decode with max uint16 values for request/file IDs */
    uint8_t buf[64];
    int n = encode_open_req(buf, sizeof(buf), 0xFFFF, 0xFFFF, 0, 0, "/x");
    ASSERT(n > 0);

    uint16_t req_id, file_id, mode;
    uint32_t flags;
    char path[64];
    ASSERT(decode_open_req(buf, (uint32_t)n, &req_id, &file_id, &flags, &mode, path, sizeof(path)) == 0);
    ASSERT_EQ(req_id, 0xFFFF);
    ASSERT_EQ(file_id, 0xFFFF);
    return 0;
}

TEST(empty_path_unlink) {
    uint8_t buf[64];
    int n = encode_unlink_req(buf, sizeof(buf), 1, "");
    ASSERT(n > 0);

    uint16_t req_id;
    char path[64];
    ASSERT(decode_unlink_req(buf, (uint32_t)n, &req_id, path, sizeof(path)) == 0);
    ASSERT_EQ(req_id, 1);
    ASSERT_STR_EQ(path, "");
    return 0;
}

TEST(empty_paths_rename) {
    uint8_t buf[64];
    int n = encode_rename_req(buf, sizeof(buf), 1, "", "");
    ASSERT(n > 0);

    uint16_t req_id;
    char old_path[64], new_path[64];
    ASSERT(decode_rename_req(buf, (uint32_t)n, &req_id,
                             old_path, sizeof(old_path),
                             new_path, sizeof(new_path)) == 0);
    ASSERT_STR_EQ(old_path, "");
    ASSERT_STR_EQ(new_path, "");
    return 0;
}

TEST(open_ok_negative_file_size) {
    uint8_t buf[16];
    encode_open_ok(buf, sizeof(buf), 1, -1, 0100644);

    uint16_t req_id;
    int64_t file_size;
    uint32_t mode;
    ASSERT(decode_open_ok(buf, 14, &req_id, &file_size, &mode) == 0);
    ASSERT_EQ(req_id, 1);
    ASSERT_EQ(file_size, -1);
    return 0;
}

TEST(io_error_all_errno_values) {
    /* Test all errno values defined in the protocol */
    int32_t errnos[] = {1, 2, 5, 13, 17, 20, 21, 22, 28, 30, 34};
    int count = (int)(sizeof(errnos) / sizeof(errnos[0]));

    for (int i = 0; i < count; i++) {
        uint8_t buf[16];
        encode_io_error(buf, sizeof(buf), (uint16_t)i, errnos[i]);

        uint16_t req_id;
        int32_t errn;
        ASSERT(decode_io_error(buf, 6, &req_id, &errn) == 0);
        ASSERT_EQ(req_id, (uint16_t)i);
        ASSERT_EQ(errn, errnos[i]);
    }
    return 0;
}

TEST(long_path_roundtrip) {
    /* Test with a path close to the limit */
    char path[1024];
    memset(path, 'a', sizeof(path) - 1);
    path[0] = '/';
    path[sizeof(path) - 1] = '\0';

    uint8_t buf[2048];
    int n = encode_open_req(buf, sizeof(buf), 1, 1, 0, 0, path);
    ASSERT(n > 0);

    uint16_t req_id, file_id, mode;
    uint32_t flags;
    char dec_path[1024];
    ASSERT(decode_open_req(buf, (uint32_t)n, &req_id, &file_id, &flags, &mode,
                           dec_path, sizeof(dec_path)) == 0);
    ASSERT_STR_EQ(dec_path, path);
    return 0;
}

TEST(read_ok_empty_data) {
    /* EOF response: zero bytes of data */
    uint8_t buf[16];
    int n = encode_read_ok(buf, sizeof(buf), 5, NULL, 0);
    ASSERT(n > 0);

    uint16_t req_id;
    const uint8_t *data;
    uint32_t data_len;
    ASSERT(decode_read_ok(buf, (uint32_t)n, &req_id, &data, &data_len) == 0);
    ASSERT_EQ(req_id, 5);
    ASSERT_EQ(data_len, 0);
    return 0;
}

TEST(seek_all_whence_values) {
    /* SEEK_SET=0, SEEK_CUR=1, SEEK_END=2 */
    for (uint8_t w = 0; w <= 2; w++) {
        uint8_t buf[16];
        encode_seek_req(buf, sizeof(buf), 1, 1, 100, w);

        uint16_t req_id, file_id;
        int64_t offset;
        uint8_t whence;
        ASSERT(decode_seek_req(buf, 13, &req_id, &file_id, &offset, &whence) == 0);
        ASSERT_EQ(whence, w);
        ASSERT_EQ(offset, 100);
    }
    return 0;
}

TEST(fstat_ok_large_mode) {
    uint8_t buf[16];
    encode_fstat_ok(buf, sizeof(buf), 1, 999999999LL, 0xFFFFFFFF);

    uint16_t req_id;
    int64_t file_size;
    uint32_t mode;
    ASSERT(decode_fstat_ok(buf, 14, &req_id, &file_size, &mode) == 0);
    ASSERT_EQ(file_size, 999999999LL);
    ASSERT_EQ(mode, 0xFFFFFFFF);
    return 0;
}

TEST(passthrough_seek_end) {
    /* Test SEEK_END in passthrough mode */
    unsetenv("FFOIP_PORT");

    char tmppath[] = "/tmp/fio_seekend_XXXXXX";
    int fd = mkstemp(tmppath);
    ASSERT(fd >= 0);
    write(fd, "0123456789", 10);
    close(fd);

    fd = fio_open(tmppath, O_RDONLY, 0);
    ASSERT(fd >= 0);

    /* Seek to end-3 */
    off_t pos = fio_lseek(fd, -3, SEEK_END);
    ASSERT_EQ(pos, 7);

    /* Read remaining bytes */
    char rbuf[4];
    ssize_t nr = fio_read(fd, rbuf, 3);
    ASSERT_EQ(nr, 3);
    ASSERT_MEM_EQ(rbuf, "789", 3);

    fio_close(fd);
    unlink(tmppath);
    return 0;
}

TEST(passthrough_read_write_large) {
    /* Test with larger data in passthrough mode */
    unsetenv("FFOIP_PORT");

    char tmppath[] = "/tmp/fio_large_XXXXXX";
    int fd = mkstemp(tmppath);
    ASSERT(fd >= 0);
    close(fd);

    fd = fio_open(tmppath, O_WRONLY | O_TRUNC, 0644);
    ASSERT(fd >= 0);

    /* Write 64KB of patterned data */
    uint8_t wbuf[65536];
    for (int i = 0; i < 65536; i++) wbuf[i] = (uint8_t)(i % 251);

    ssize_t total_written = 0;
    while (total_written < 65536) {
        ssize_t nw = fio_write(fd, wbuf + total_written, 65536 - (size_t)total_written);
        ASSERT(nw > 0);
        total_written += nw;
    }
    fio_close(fd);

    /* Read back and verify */
    fd = fio_open(tmppath, O_RDONLY, 0);
    ASSERT(fd >= 0);

    uint8_t rbuf[65536];
    ssize_t total_read = 0;
    while (total_read < 65536) {
        ssize_t nr = fio_read(fd, rbuf + total_read, 65536 - (size_t)total_read);
        ASSERT(nr > 0);
        total_read += nr;
    }
    ASSERT_MEM_EQ(rbuf, wbuf, 65536);

    fio_close(fd);
    unlink(tmppath);
    return 0;
}

TEST(passthrough_fstat_permissions) {
    unsetenv("FFOIP_PORT");

    char tmppath[] = "/tmp/fio_perm_XXXXXX";
    int fd = mkstemp(tmppath);
    ASSERT(fd >= 0);
    close(fd);

    /* Open and write some data */
    fd = fio_open(tmppath, O_WRONLY, 0);
    ASSERT(fd >= 0);
    fio_write(fd, "hello", 5);

    /* fstat should reflect correct size */
    struct stat st;
    ASSERT_EQ(fio_fstat(fd, &st), 0);
    ASSERT_EQ(st.st_size, 5);

    fio_close(fd);
    unlink(tmppath);
    return 0;
}

/* ======================================================================
 * Wire-test helpers (TCP communication with Go server)
 * ====================================================================== */

/* Message type constants (must match fio.c / Go protocol constants) */
#define WIRE_MSG_OPEN        0x20
#define WIRE_MSG_READ        0x21
#define WIRE_MSG_WRITE       0x22
#define WIRE_MSG_SEEK        0x23
#define WIRE_MSG_CLOSE       0x24
#define WIRE_MSG_FSTAT       0x25
#define WIRE_MSG_FTRUNCATE   0x26
#define WIRE_MSG_UNLINK      0x27
#define WIRE_MSG_RENAME      0x28
#define WIRE_MSG_MKDIR       0x29
#define WIRE_MSG_STAT        0x2A
#define WIRE_MSG_LISTDIR     0x2B

#define WIRE_MSG_OPEN_OK        0x40
#define WIRE_MSG_READ_OK        0x41
#define WIRE_MSG_WRITE_OK       0x42
#define WIRE_MSG_SEEK_OK        0x43
#define WIRE_MSG_CLOSE_OK       0x44
#define WIRE_MSG_FSTAT_OK       0x45
#define WIRE_MSG_FTRUNCATE_OK   0x46
#define WIRE_MSG_UNLINK_OK      0x47
#define WIRE_MSG_RENAME_OK      0x48
#define WIRE_MSG_MKDIR_OK       0x49
#define WIRE_MSG_STAT_OK        0x4A
#define WIRE_MSG_LISTDIR_OK     0x4B
#define WIRE_MSG_IO_ERROR       0x4F

static int read_full(int fd, uint8_t *buf, size_t len) {
    size_t done = 0;
    while (done < len) {
        ssize_t n = read(fd, buf + done, len - done);
        if (n <= 0) return -1;
        done += (size_t)n;
    }
    return 0;
}

static int write_full(int fd, const uint8_t *buf, size_t len) {
    size_t done = 0;
    while (done < len) {
        ssize_t n = write(fd, buf + done, len - done);
        if (n <= 0) return -1;
        done += (size_t)n;
    }
    return 0;
}

static int send_envelope(int fd, uint8_t type, const uint8_t *payload, uint32_t len) {
    uint8_t header[5];
    header[0] = type;
    test_put_u32(header + 1, len);
    if (write_full(fd, header, 5) != 0) return -1;
    if (len > 0 && write_full(fd, payload, len) != 0) return -1;
    return 0;
}

static int recv_envelope(int fd, uint8_t *type, uint8_t *payload, uint32_t payload_cap, uint32_t *payload_len) {
    uint8_t header[5];
    if (read_full(fd, header, 5) != 0) return -1;
    *type = header[0];
    *payload_len = test_get_u32(header + 1);
    if (*payload_len > payload_cap) return -1;
    if (*payload_len > 0 && read_full(fd, payload, *payload_len) != 0) return -1;
    return 0;
}

/* Wire test: each test is a function returning 0=pass, nonzero=fail.
 * They take the socket fd and shared buffers. */
typedef struct {
    uint8_t buf[4096];
    uint8_t rbuf[4096];
    uint8_t rtype;
    uint32_t rlen;
} wire_ctx;

#define WFAIL do { return 1; } while(0)

/* ---- Phase 1: echo tests (C sends, Go echoes back) ---- */

static int wt_echo_open(int s, wire_ctx *c) {
    int n = encode_open_req(c->buf, sizeof(c->buf), 0x0102, 0x0304, 0x0241, 0x01B6, "/tmp/test.ts");
    if (send_envelope(s, WIRE_MSG_OPEN, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_OPEN) WFAIL;
    uint16_t req, fid, mode; uint32_t flags; char path[256];
    if (decode_open_req(c->rbuf, c->rlen, &req, &fid, &flags, &mode, path, sizeof(path)) != 0) WFAIL;
    if (!(req == 0x0102 && fid == 0x0304 && flags == 0x0241 && mode == 0x01B6 && strcmp(path, "/tmp/test.ts") == 0)) WFAIL;
    return 0;
}

static int wt_echo_read(int s, wire_ctx *c) {
    int n = encode_read_req(c->buf, sizeof(c->buf), 5, 1, 65536);
    if (send_envelope(s, WIRE_MSG_READ, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_READ) WFAIL;
    uint16_t req, fid; uint32_t nb;
    if (decode_read_req(c->rbuf, c->rlen, &req, &fid, &nb) != 0) WFAIL;
    if (!(req == 5 && fid == 1 && nb == 65536)) WFAIL;
    return 0;
}

static int wt_echo_write(int s, wire_ctx *c) {
    const uint8_t wdata[] = "hello video data";
    int n = encode_write_req(c->buf, sizeof(c->buf), 10, 2, wdata, (uint32_t)strlen((const char *)wdata));
    if (send_envelope(s, WIRE_MSG_WRITE, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_WRITE) WFAIL;
    uint16_t req, fid; const uint8_t *data; uint32_t dlen;
    if (decode_write_req(c->rbuf, c->rlen, &req, &fid, &data, &dlen) != 0) WFAIL;
    if (!(req == 10 && fid == 2 && dlen == 16 && memcmp(data, "hello video data", 16) == 0)) WFAIL;
    return 0;
}

static int wt_echo_seek(int s, wire_ctx *c) {
    int n = encode_seek_req(c->buf, sizeof(c->buf), 7, 1, -9999999999LL, 2);
    if (send_envelope(s, WIRE_MSG_SEEK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_SEEK) WFAIL;
    uint16_t req, fid; int64_t off; uint8_t wh;
    if (decode_seek_req(c->rbuf, c->rlen, &req, &fid, &off, &wh) != 0) WFAIL;
    if (!(req == 7 && fid == 1 && off == -9999999999LL && wh == 2)) WFAIL;
    return 0;
}

static int wt_echo_close(int s, wire_ctx *c) {
    int n = encode_close_req(c->buf, sizeof(c->buf), 100, 50);
    if (send_envelope(s, WIRE_MSG_CLOSE, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_CLOSE) WFAIL;
    uint16_t req, fid;
    if (decode_close_req(c->rbuf, c->rlen, &req, &fid) != 0) WFAIL;
    if (!(req == 100 && fid == 50)) WFAIL;
    return 0;
}

static int wt_echo_fstat(int s, wire_ctx *c) {
    int n = encode_fstat_req(c->buf, sizeof(c->buf), 3, 7);
    if (send_envelope(s, WIRE_MSG_FSTAT, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_FSTAT) WFAIL;
    uint16_t req, fid;
    if (decode_fstat_req(c->rbuf, c->rlen, &req, &fid) != 0) WFAIL;
    if (!(req == 3 && fid == 7)) WFAIL;
    return 0;
}

static int wt_echo_ftruncate(int s, wire_ctx *c) {
    int n = encode_ftruncate_req(c->buf, sizeof(c->buf), 20, 5, 1048576);
    if (send_envelope(s, WIRE_MSG_FTRUNCATE, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_FTRUNCATE) WFAIL;
    uint16_t req, fid; int64_t length;
    if (decode_ftruncate_req(c->rbuf, c->rlen, &req, &fid, &length) != 0) WFAIL;
    if (!(req == 20 && fid == 5 && length == 1048576)) WFAIL;
    return 0;
}

static int wt_echo_unlink(int s, wire_ctx *c) {
    int n = encode_unlink_req(c->buf, sizeof(c->buf), 15, "/tmp/old.ts");
    if (send_envelope(s, WIRE_MSG_UNLINK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_UNLINK) WFAIL;
    uint16_t req; char path[256];
    if (decode_unlink_req(c->rbuf, c->rlen, &req, path, sizeof(path)) != 0) WFAIL;
    if (!(req == 15 && strcmp(path, "/tmp/old.ts") == 0)) WFAIL;
    return 0;
}

static int wt_echo_rename(int s, wire_ctx *c) {
    int n = encode_rename_req(c->buf, sizeof(c->buf), 30, "/tmp/a.tmp", "/tmp/a.m3u8");
    if (send_envelope(s, WIRE_MSG_RENAME, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_RENAME) WFAIL;
    uint16_t req; char oldp[256], newp[256];
    if (decode_rename_req(c->rbuf, c->rlen, &req, oldp, sizeof(oldp), newp, sizeof(newp)) != 0) WFAIL;
    if (!(req == 30 && strcmp(oldp, "/tmp/a.tmp") == 0 && strcmp(newp, "/tmp/a.m3u8") == 0)) WFAIL;
    return 0;
}

static int wt_echo_mkdir(int s, wire_ctx *c) {
    int n = encode_mkdir_req(c->buf, sizeof(c->buf), 8, 0755, "/tmp/2024");
    if (send_envelope(s, WIRE_MSG_MKDIR, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_MKDIR) WFAIL;
    uint16_t req, mode; char path[256];
    if (decode_mkdir_req(c->rbuf, c->rlen, &req, &mode, path, sizeof(path)) != 0) WFAIL;
    if (!(req == 8 && mode == 0755 && strcmp(path, "/tmp/2024") == 0)) WFAIL;
    return 0;
}

static int wt_echo_open_ok(int s, wire_ctx *c) {
    int n = encode_open_ok(c->buf, sizeof(c->buf), 1, 524288000, 0010640);
    if (send_envelope(s, WIRE_MSG_OPEN_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_OPEN_OK) WFAIL;
    uint16_t req; int64_t sz; uint32_t mode;
    if (decode_open_ok(c->rbuf, c->rlen, &req, &sz, &mode) != 0) WFAIL;
    if (!(req == 1 && sz == 524288000 && mode == 0010640)) WFAIL;
    return 0;
}

static int wt_echo_read_ok(int s, wire_ctx *c) {
    uint8_t rdata[16]; for (int i = 0; i < 16; i++) rdata[i] = (uint8_t)i;
    int n = encode_read_ok(c->buf, sizeof(c->buf), 5, rdata, 16);
    if (send_envelope(s, WIRE_MSG_READ_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_READ_OK) WFAIL;
    uint16_t req; const uint8_t *data; uint32_t dlen;
    if (decode_read_ok(c->rbuf, c->rlen, &req, &data, &dlen) != 0) WFAIL;
    uint8_t expected[16]; for (int i = 0; i < 16; i++) expected[i] = (uint8_t)i;
    if (!(req == 5 && dlen == 16 && memcmp(data, expected, 16) == 0)) WFAIL;
    return 0;
}

static int wt_echo_write_ok(int s, wire_ctx *c) {
    int n = encode_write_ok(c->buf, sizeof(c->buf), 10, 65536);
    if (send_envelope(s, WIRE_MSG_WRITE_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_WRITE_OK) WFAIL;
    uint16_t req; uint32_t written;
    if (decode_write_ok(c->rbuf, c->rlen, &req, &written) != 0) WFAIL;
    if (!(req == 10 && written == 65536)) WFAIL;
    return 0;
}

static int wt_echo_seek_ok(int s, wire_ctx *c) {
    int n = encode_seek_ok(c->buf, sizeof(c->buf), 7, 1048576);
    if (send_envelope(s, WIRE_MSG_SEEK_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_SEEK_OK) WFAIL;
    uint16_t req; int64_t off;
    if (decode_seek_ok(c->rbuf, c->rlen, &req, &off) != 0) WFAIL;
    if (!(req == 7 && off == 1048576)) WFAIL;
    return 0;
}

static int wt_echo_close_ok(int s, wire_ctx *c) {
    int n = encode_reqid_resp(c->buf, sizeof(c->buf), 100);
    if (send_envelope(s, WIRE_MSG_CLOSE_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_CLOSE_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 100) WFAIL;
    return 0;
}

static int wt_echo_fstat_ok(int s, wire_ctx *c) {
    int n = encode_fstat_ok(c->buf, sizeof(c->buf), 3, 999999, 0100644);
    if (send_envelope(s, WIRE_MSG_FSTAT_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_FSTAT_OK) WFAIL;
    uint16_t req; int64_t sz; uint32_t mode;
    if (decode_fstat_ok(c->rbuf, c->rlen, &req, &sz, &mode) != 0) WFAIL;
    if (!(req == 3 && sz == 999999 && mode == 0100644)) WFAIL;
    return 0;
}

static int wt_echo_ftruncate_ok(int s, wire_ctx *c) {
    int n = encode_reqid_resp(c->buf, sizeof(c->buf), 20);
    if (send_envelope(s, WIRE_MSG_FTRUNCATE_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_FTRUNCATE_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 20) WFAIL;
    return 0;
}

static int wt_echo_unlink_ok(int s, wire_ctx *c) {
    int n = encode_reqid_resp(c->buf, sizeof(c->buf), 15);
    if (send_envelope(s, WIRE_MSG_UNLINK_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_UNLINK_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 15) WFAIL;
    return 0;
}

static int wt_echo_rename_ok(int s, wire_ctx *c) {
    int n = encode_reqid_resp(c->buf, sizeof(c->buf), 30);
    if (send_envelope(s, WIRE_MSG_RENAME_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_RENAME_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 30) WFAIL;
    return 0;
}

static int wt_echo_mkdir_ok(int s, wire_ctx *c) {
    int n = encode_reqid_resp(c->buf, sizeof(c->buf), 8);
    if (send_envelope(s, WIRE_MSG_MKDIR_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_MKDIR_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 8) WFAIL;
    return 0;
}

static int wt_echo_io_error(int s, wire_ctx *c) {
    int n = encode_io_error(c->buf, sizeof(c->buf), 1, 2);
    if (send_envelope(s, WIRE_MSG_IO_ERROR, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_IO_ERROR) WFAIL;
    uint16_t req; int32_t errn;
    if (decode_io_error(c->rbuf, c->rlen, &req, &errn) != 0) WFAIL;
    if (!(req == 1 && errn == 2)) WFAIL;
    return 0;
}

static int wt_check_path(wire_ctx *c, uint8_t type, uint16_t want_req, const char *want_path) {
    if (c->rtype != type) return 1;
    char path[256]; uint16_t req;
    if (decode_unlink_req(c->rbuf, c->rlen, &req, path, sizeof(path)) != 0) return 1;
    return !(req == want_req && strcmp(path, want_path) == 0);
}

static int wt_check_stat_ok(wire_ctx *c) {
    if (c->rtype != WIRE_MSG_STAT_OK) return 1;
    uint16_t req; int64_t size, mtime; uint32_t mode; uint8_t acc;
    if (decode_stat_ok(c->rbuf, c->rlen, &req, &size, &mode, &mtime, &acc) != 0) return 1;
    return !(req == 41 && size == 123456789012LL && mode == 0100644 && mtime == -5 && acc == 0x05);
}

static int wt_check_listdir_ok(wire_ctx *c) {
    if (c->rtype != WIRE_MSG_LISTDIR_OK) return 1;
    uint16_t req; uint32_t count;
    if (decode_listdir_header(c->rbuf, c->rlen, &req, &count) != 6) return 1;
    if (req != 42 || count != 2) return 1;
    uint32_t pos = 6;
    const uint8_t *name; uint16_t nl; uint32_t mode; int64_t size, mtime;
    if (decode_listdir_entry(c->rbuf, c->rlen, &pos, &name, &nl, &mode, &size, &mtime) != 0) return 1;
    if (!(nl == 5 && memcmp(name, "a.ttf", 5) == 0 && mode == 0100644 && size == 1234 && mtime == 1700000000)) return 1;
    if (decode_listdir_entry(c->rbuf, c->rlen, &pos, &name, &nl, &mode, &size, &mtime) != 0) return 1;
    if (!(nl == 3 && memcmp(name, "sub", 3) == 0 && mode == 040755 && size == 96 && mtime == -1)) return 1;
    return pos != c->rlen;
}

static int wt_echo_stat(int s, wire_ctx *c) {
    int n = encode_path_req(c->buf, sizeof(c->buf), 41, "/fonts/a.ttf");
    if (send_envelope(s, WIRE_MSG_STAT, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (wt_check_path(c, WIRE_MSG_STAT, 41, "/fonts/a.ttf")) WFAIL;
    return 0;
}

static int wt_echo_listdir(int s, wire_ctx *c) {
    int n = encode_path_req(c->buf, sizeof(c->buf), 42, "/fonts");
    if (send_envelope(s, WIRE_MSG_LISTDIR, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (wt_check_path(c, WIRE_MSG_LISTDIR, 42, "/fonts")) WFAIL;
    return 0;
}

static int wt_echo_stat_ok(int s, wire_ctx *c) {
    int n = encode_stat_ok(c->buf, sizeof(c->buf), 41, 123456789012LL, 0100644, -5, 0x05);
    if (send_envelope(s, WIRE_MSG_STAT_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (wt_check_stat_ok(c)) WFAIL;
    return 0;
}

static int wt_echo_listdir_ok(int s, wire_ctx *c) {
    int n = encode_listdir_ok_header(c->buf, sizeof(c->buf), 42, 2);
    n += encode_listdir_entry(c->buf + n, sizeof(c->buf) - n, "a.ttf", 0100644, 1234, 1700000000);
    n += encode_listdir_entry(c->buf + n, sizeof(c->buf) - n, "sub", 040755, 96, -1);
    if (send_envelope(s, WIRE_MSG_LISTDIR_OK, c->buf, (uint32_t)n) != 0) WFAIL;
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (wt_check_listdir_ok(c)) WFAIL;
    return 0;
}

/* ---- Phase 2: verify Go-originated messages ---- */

static int wt_go_open(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_OPEN) WFAIL;
    uint16_t req, fid, mode; uint32_t flags; char path[256];
    if (decode_open_req(c->rbuf, c->rlen, &req, &fid, &flags, &mode, path, sizeof(path)) != 0) WFAIL;
    if (!(req == 0x0102 && fid == 0x0304 && flags == 0x0241 && mode == 0x01B6 && strcmp(path, "/tmp/test.ts") == 0)) WFAIL;
    return 0;
}

static int wt_go_read(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_READ) WFAIL;
    uint16_t req, fid; uint32_t nb;
    if (decode_read_req(c->rbuf, c->rlen, &req, &fid, &nb) != 0) WFAIL;
    if (!(req == 5 && fid == 1 && nb == 65536)) WFAIL;
    return 0;
}

static int wt_go_write(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_WRITE) WFAIL;
    uint16_t req, fid; const uint8_t *data; uint32_t dlen;
    if (decode_write_req(c->rbuf, c->rlen, &req, &fid, &data, &dlen) != 0) WFAIL;
    if (!(req == 10 && fid == 2 && dlen == 16 && memcmp(data, "hello video data", 16) == 0)) WFAIL;
    return 0;
}

static int wt_go_seek(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_SEEK) WFAIL;
    uint16_t req, fid; int64_t off; uint8_t wh;
    if (decode_seek_req(c->rbuf, c->rlen, &req, &fid, &off, &wh) != 0) WFAIL;
    if (!(req == 7 && fid == 1 && off == -9999999999LL && wh == 2)) WFAIL;
    return 0;
}

static int wt_go_close(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_CLOSE) WFAIL;
    uint16_t req, fid;
    if (decode_close_req(c->rbuf, c->rlen, &req, &fid) != 0) WFAIL;
    if (!(req == 100 && fid == 50)) WFAIL;
    return 0;
}

static int wt_go_fstat(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_FSTAT) WFAIL;
    uint16_t req, fid;
    if (decode_fstat_req(c->rbuf, c->rlen, &req, &fid) != 0) WFAIL;
    if (!(req == 3 && fid == 7)) WFAIL;
    return 0;
}

static int wt_go_ftruncate(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_FTRUNCATE) WFAIL;
    uint16_t req, fid; int64_t length;
    if (decode_ftruncate_req(c->rbuf, c->rlen, &req, &fid, &length) != 0) WFAIL;
    if (!(req == 20 && fid == 5 && length == 1048576)) WFAIL;
    return 0;
}

static int wt_go_unlink(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_UNLINK) WFAIL;
    uint16_t req; char path[256];
    if (decode_unlink_req(c->rbuf, c->rlen, &req, path, sizeof(path)) != 0) WFAIL;
    if (!(req == 15 && strcmp(path, "/tmp/old.ts") == 0)) WFAIL;
    return 0;
}

static int wt_go_rename(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_RENAME) WFAIL;
    uint16_t req; char oldp[256], newp[256];
    if (decode_rename_req(c->rbuf, c->rlen, &req, oldp, sizeof(oldp), newp, sizeof(newp)) != 0) WFAIL;
    if (!(req == 30 && strcmp(oldp, "/tmp/a.tmp") == 0 && strcmp(newp, "/tmp/a.m3u8") == 0)) WFAIL;
    return 0;
}

static int wt_go_mkdir(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_MKDIR) WFAIL;
    uint16_t req, mode; char path[256];
    if (decode_mkdir_req(c->rbuf, c->rlen, &req, &mode, path, sizeof(path)) != 0) WFAIL;
    if (!(req == 8 && mode == 0755 && strcmp(path, "/tmp/2024") == 0)) WFAIL;
    return 0;
}

static int wt_go_open_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_OPEN_OK) WFAIL;
    uint16_t req; int64_t sz; uint32_t mode;
    if (decode_open_ok(c->rbuf, c->rlen, &req, &sz, &mode) != 0) WFAIL;
    if (!(req == 1 && sz == 524288000 && mode == 0010640)) WFAIL;
    return 0;
}

static int wt_go_read_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_READ_OK) WFAIL;
    uint16_t req; const uint8_t *data; uint32_t dlen;
    if (decode_read_ok(c->rbuf, c->rlen, &req, &data, &dlen) != 0) WFAIL;
    uint8_t expected[16]; for (int i = 0; i < 16; i++) expected[i] = (uint8_t)i;
    if (!(req == 5 && dlen == 16 && memcmp(data, expected, 16) == 0)) WFAIL;
    return 0;
}

static int wt_go_write_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_WRITE_OK) WFAIL;
    uint16_t req; uint32_t written;
    if (decode_write_ok(c->rbuf, c->rlen, &req, &written) != 0) WFAIL;
    if (!(req == 10 && written == 65536)) WFAIL;
    return 0;
}

static int wt_go_seek_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_SEEK_OK) WFAIL;
    uint16_t req; int64_t off;
    if (decode_seek_ok(c->rbuf, c->rlen, &req, &off) != 0) WFAIL;
    if (!(req == 7 && off == 1048576)) WFAIL;
    return 0;
}

static int wt_go_close_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_CLOSE_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 100) WFAIL;
    return 0;
}

static int wt_go_fstat_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_FSTAT_OK) WFAIL;
    uint16_t req; int64_t sz; uint32_t mode;
    if (decode_fstat_ok(c->rbuf, c->rlen, &req, &sz, &mode) != 0) WFAIL;
    if (!(req == 3 && sz == 999999 && mode == 0100644)) WFAIL;
    return 0;
}

static int wt_go_ftruncate_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_FTRUNCATE_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 20) WFAIL;
    return 0;
}

static int wt_go_unlink_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_UNLINK_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 15) WFAIL;
    return 0;
}

static int wt_go_rename_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_RENAME_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 30) WFAIL;
    return 0;
}

static int wt_go_mkdir_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_MKDIR_OK) WFAIL;
    uint16_t req;
    if (decode_reqid_resp(c->rbuf, c->rlen, &req) != 0) WFAIL;
    if (req != 8) WFAIL;
    return 0;
}

static int wt_go_io_error(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (c->rtype != WIRE_MSG_IO_ERROR) WFAIL;
    uint16_t req; int32_t errn;
    if (decode_io_error(c->rbuf, c->rlen, &req, &errn) != 0) WFAIL;
    if (!(req == 1 && errn == 2)) WFAIL;
    return 0;
}

static int wt_go_stat(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (wt_check_path(c, WIRE_MSG_STAT, 41, "/fonts/a.ttf")) WFAIL;
    return 0;
}

static int wt_go_listdir(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (wt_check_path(c, WIRE_MSG_LISTDIR, 42, "/fonts")) WFAIL;
    return 0;
}

static int wt_go_stat_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (wt_check_stat_ok(c)) WFAIL;
    return 0;
}

static int wt_go_listdir_ok(int s, wire_ctx *c) {
    if (recv_envelope(s, &c->rtype, c->rbuf, sizeof(c->rbuf), &c->rlen) != 0) WFAIL;
    if (wt_check_listdir_ok(c)) WFAIL;
    return 0;
}

#undef WFAIL

#define WT_RUN(label, func) do { \
    wt_run++; \
    printf("  %-40s ", label); \
    if (func(sock, &ctx) == 0) { wt_pass++; printf("PASS\n"); } \
    else { printf("FAIL\n"); } \
} while(0)

static int wire_test_main(int port) {
    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) { perror("socket"); return 1; }

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)port);
    addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);

    if (connect(sock, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
        perror("connect");
        close(sock);
        return 1;
    }

    wire_ctx ctx;
    int wt_run = 0, wt_pass = 0;

    printf("--- Phase 1: C sends, Go echoes ---\n");
    WT_RUN("Open echo",        wt_echo_open);
    WT_RUN("Read echo",        wt_echo_read);
    WT_RUN("Write echo",       wt_echo_write);
    WT_RUN("Seek echo",        wt_echo_seek);
    WT_RUN("Close echo",       wt_echo_close);
    WT_RUN("Fstat echo",       wt_echo_fstat);
    WT_RUN("Ftruncate echo",   wt_echo_ftruncate);
    WT_RUN("Unlink echo",      wt_echo_unlink);
    WT_RUN("Rename echo",      wt_echo_rename);
    WT_RUN("Mkdir echo",       wt_echo_mkdir);
    WT_RUN("Stat echo",        wt_echo_stat);
    WT_RUN("ListDir echo",     wt_echo_listdir);
    WT_RUN("OpenOk echo",      wt_echo_open_ok);
    WT_RUN("ReadOk echo",      wt_echo_read_ok);
    WT_RUN("WriteOk echo",     wt_echo_write_ok);
    WT_RUN("SeekOk echo",      wt_echo_seek_ok);
    WT_RUN("CloseOk echo",     wt_echo_close_ok);
    WT_RUN("FstatOk echo",     wt_echo_fstat_ok);
    WT_RUN("FtruncateOk echo", wt_echo_ftruncate_ok);
    WT_RUN("UnlinkOk echo",    wt_echo_unlink_ok);
    WT_RUN("RenameOk echo",    wt_echo_rename_ok);
    WT_RUN("MkdirOk echo",     wt_echo_mkdir_ok);
    WT_RUN("StatOk echo",      wt_echo_stat_ok);
    WT_RUN("ListDirOk echo",   wt_echo_listdir_ok);
    WT_RUN("IoError echo",     wt_echo_io_error);

    printf("\n--- Phase 2: Go sends, C verifies ---\n");
    WT_RUN("Go Open",        wt_go_open);
    WT_RUN("Go Read",        wt_go_read);
    WT_RUN("Go Write",       wt_go_write);
    WT_RUN("Go Seek",        wt_go_seek);
    WT_RUN("Go Close",       wt_go_close);
    WT_RUN("Go Fstat",       wt_go_fstat);
    WT_RUN("Go Ftruncate",   wt_go_ftruncate);
    WT_RUN("Go Unlink",      wt_go_unlink);
    WT_RUN("Go Rename",      wt_go_rename);
    WT_RUN("Go Mkdir",       wt_go_mkdir);
    WT_RUN("Go Stat",        wt_go_stat);
    WT_RUN("Go ListDir",     wt_go_listdir);
    WT_RUN("Go OpenOk",      wt_go_open_ok);
    WT_RUN("Go ReadOk",      wt_go_read_ok);
    WT_RUN("Go WriteOk",     wt_go_write_ok);
    WT_RUN("Go SeekOk",      wt_go_seek_ok);
    WT_RUN("Go CloseOk",     wt_go_close_ok);
    WT_RUN("Go FstatOk",     wt_go_fstat_ok);
    WT_RUN("Go FtruncateOk", wt_go_ftruncate_ok);
    WT_RUN("Go UnlinkOk",    wt_go_unlink_ok);
    WT_RUN("Go RenameOk",    wt_go_rename_ok);
    WT_RUN("Go MkdirOk",     wt_go_mkdir_ok);
    WT_RUN("Go StatOk",      wt_go_stat_ok);
    WT_RUN("Go ListDirOk",   wt_go_listdir_ok);
    WT_RUN("Go IoError",     wt_go_io_error);

#undef WT_RUN

    close(sock);

    printf("\n========================================\n");
    printf("wire-test: %d passed, %d failed\n", wt_pass, wt_run - wt_pass);
    printf("========================================\n");

    return (wt_pass == wt_run) ? 0 : 1;
}

/* ======================================================================
 * Tunnel-mode tests: in-process mock server + read-ahead/cache/prefetch
 *
 * These exercise the virtual-fd path in fio.c (read-ahead, range cache,
 * async prefetch, cache-aware seek, short-read handling, write invalidation)
 * which the passthrough tests never reach. A socketpair connects fio's reader
 * thread to a mock server thread that serves a simulated read-only file.
 * ====================================================================== */

/* Deterministic content pattern so reads can be verified byte-for-byte. */
static uint8_t tun_pat(int64_t i) {
    return (uint8_t)(((uint64_t)i * 1103515245u + 12345u) >> 16);
}

#define MOCK_FILE_SLOTS 1024    /* per-file offsets, indexed by file_id */
#define MOCK_STAT_MTIME 1700000000LL

typedef struct {
    int       sock;            /* server end of the socketpair */
    pthread_t thread;
    uint8_t  *content;         /* NULL: generate tun_pat bytes on the fly */
    int64_t   size;
    int64_t   offsets[MOCK_FILE_SLOTS]; /* server-side offset of each open file */
    int       hangup_on_read;  /* on a READ, hang up without replying */
    uint16_t  last_open_file_id;
    uint16_t  last_req_id;     /* request id of the last message received */
    uint32_t  max_read_req;    /* largest READ nbytes seen */
    uint32_t  max_write_len;   /* largest WRITE data length seen */
    uint32_t  short_read_cap;  /* if nonzero, cap each READ response (simulates
                                  short reads from NFS/FUSE-backed storage) */
    uint32_t  open_mode;       /* st_mode bits sent in OPEN_OK (0: 0100644) */
    uint32_t  write_fail_after;/* if nonzero, a WRITE stores this many bytes
                                  and then answers IO_ERROR (ENOSPC), as an
                                  older client did after a partial write */
    int       write_ok_extra;  /* added to the WRITE_OK count (a bad server) */
    int       seek_reply_negative; /* answer SEEK with a negative offset */
    int       seek_reply_huge;     /* answer SEEK with an offset near INT64_MAX */
    int32_t   seek_error;          /* if nonzero, answer SEEK with this errno
                                      (29 = ESPIPE, as a pipe does) */
    pthread_mutex_t mu;
    int       reads;           /* READ requests served */
    int       past_eof_reads;  /* READ requests that arrived at or past EOF */
    int       seeks;           /* SEEK requests served */
    int       writes;          /* WRITE requests served */
} mock_server;

static void mock_send_error(int sock, uint16_t req_id, int32_t wire_err) {
    uint8_t resp[8];
    int n = encode_io_error(resp, sizeof(resp), req_id, wire_err);
    send_envelope(sock, WIRE_MSG_IO_ERROR, resp, (uint32_t)n);
}

/* STAT and LISTDIR. Paths: /missing -> ENOENT, /fonts -> a directory,
 * /truncated -> a listing whose count claims more entries than the payload
 * holds; anything else is a regular file. */
static void mock_path_op(mock_server *m, uint8_t type, uint16_t req_id, const char *path) {
    if (!strcmp(path, "/missing")) { mock_send_error(m->sock, req_id, 2); return; }

    if (type == WIRE_MSG_STAT) {
        uint8_t resp[32];
        int is_dir = !strcmp(path, "/fonts");
        int n = encode_stat_ok(resp, sizeof(resp), req_id, is_dir ? 4096 : m->size,
                               is_dir ? 0040755 : 0100640, MOCK_STAT_MTIME,
                               is_dir ? (0x01 | 0x04) : 0x01);
        send_envelope(m->sock, WIRE_MSG_STAT_OK, resp, (uint32_t)n);
        return;
    }

    static const struct { const char *name; uint16_t len; uint32_t mode; int64_t size; } entries[] = {
        { ".",          1, 0040755, 0 },
        { "..",         2, 0040755, 0 },
        { "a.ttf",      5, 0100644, 1234 },
        { "sub",        3, 0040755, 96 },
        { "",           0, 0100644, 1 },  /* empty name: skipped */
        { "bad\0name",  8, 0100644, 1 },  /* embedded NUL: skipped */
        { "link.otf",   8, 0120777, 9 },
        { "a/b",        3, 0100644, 1 },  /* path separator: skipped */
        { ".hidden",    7, 0100600, 5 },
    };
    int truncated = !strcmp(path, "/truncated");
    int count = truncated ? 1 : (int)(sizeof(entries) / sizeof(entries[0]));
    uint8_t resp[1024];
    int pos = encode_listdir_ok_header(resp, sizeof(resp), req_id, truncated ? 3 : (uint32_t)count);
    for (int i = 0; i < count; i++) {
        uint16_t len = truncated ? 5 : entries[truncated ? 2 : i].len;
        const char *name = entries[truncated ? 2 : i].name;
        resp[pos] = (uint8_t)(len >> 8); resp[pos + 1] = (uint8_t)len;
        memcpy(resp + pos + 2, name, len);
        uint8_t tail[24];
        /* encode_listdir_entry takes a C string, so build the fixed part with
         * an empty name and copy it after our raw (possibly NUL-holding) name. */
        encode_listdir_entry(tail, sizeof(tail), "", entries[truncated ? 2 : i].mode,
                             entries[truncated ? 2 : i].size, MOCK_STAT_MTIME + i);
        memcpy(resp + pos + 2 + len, tail + 2, 20);
        pos += 2 + len + 20;
    }
    send_envelope(m->sock, WIRE_MSG_LISTDIR_OK, resp, (uint32_t)pos);
}

static void *mock_server_thread(void *arg) {
    mock_server *m = (mock_server *)arg;
    uint8_t header[5];

    for (;;) {
        if (read_full(m->sock, header, 5) != 0) break;
        uint8_t type = header[0];
        uint32_t plen = test_get_u32(header + 1);

        uint8_t *payload = NULL;
        if (plen > 0) {
            payload = malloc(plen);
            if (!payload || read_full(m->sock, payload, plen) != 0) { free(payload); break; }
        }
        uint16_t req_id = (plen >= 2) ? (uint16_t)(((uint16_t)payload[0] << 8) | payload[1]) : 0;
        pthread_mutex_lock(&m->mu);
        m->last_req_id = req_id;
        pthread_mutex_unlock(&m->mu);

        if (type == WIRE_MSG_OPEN) {
            uint16_t r, f; uint32_t fl; uint16_t md; char path[256];
            if (decode_open_req(payload, plen, &r, &f, &fl, &md, path, sizeof(path)) != 0) {
                free(payload); break;
            }
            uint8_t resp[16];
            int n = encode_open_ok(resp, sizeof(resp), req_id, m->size,
                                   m->open_mode ? m->open_mode : 0100644);
            pthread_mutex_lock(&m->mu);
            m->offsets[f % MOCK_FILE_SLOTS] = 0;
            m->last_open_file_id = f;
            pthread_mutex_unlock(&m->mu);
            send_envelope(m->sock, WIRE_MSG_OPEN_OK, resp, (uint32_t)n);
        } else if (type == WIRE_MSG_READ && m->hangup_on_read) {
            free(payload);
            shutdown(m->sock, SHUT_RDWR);
            break;
        } else if (type == WIRE_MSG_READ) {
            uint16_t r, f; uint32_t nb;
            if (decode_read_req(payload, plen, &r, &f, &nb) != 0) { free(payload); break; }
            pthread_mutex_lock(&m->mu);
            int64_t *offset = &m->offsets[f % MOCK_FILE_SLOTS];
            if (nb > m->max_read_req) m->max_read_req = nb;
            int64_t avail = m->size - *offset;
            if (avail < 0) avail = 0;
            uint32_t give = (avail < (int64_t)nb) ? (uint32_t)avail : nb;
            if (m->short_read_cap && give > m->short_read_cap) give = m->short_read_cap;
            uint8_t *resp = malloc((size_t)give + 2);
            resp[0] = (uint8_t)(req_id >> 8);
            resp[1] = (uint8_t)req_id;
            if (give > 0 && m->content) {
                memcpy(resp + 2, m->content + *offset, give);
            } else {
                for (uint32_t i = 0; i < give; i++) resp[2 + i] = tun_pat(*offset + i);
            }
            if (*offset >= m->size) m->past_eof_reads++;
            *offset += give;
            m->reads++;
            pthread_mutex_unlock(&m->mu);
            send_envelope(m->sock, WIRE_MSG_READ_OK, resp, give + 2);
            free(resp);
        } else if (type == WIRE_MSG_SEEK) {
            uint16_t r, f; int64_t off; uint8_t wh;
            if (decode_seek_req(payload, plen, &r, &f, &off, &wh) != 0) { free(payload); break; }
            pthread_mutex_lock(&m->mu);
            m->seeks++;
            int32_t seek_err = m->seek_error;
            if (seek_err) {
                pthread_mutex_unlock(&m->mu);
                mock_send_error(m->sock, req_id, seek_err);
                free(payload);
                continue;
            }
            int64_t *offset = &m->offsets[f % MOCK_FILE_SLOTS];
            int64_t no = (wh == 0) ? off : (wh == 1) ? *offset + off : m->size + off;
            *offset = no;
            if (m->seek_reply_negative) no = -5;
            if (m->seek_reply_huge) no = INT64_MAX - 7;
            pthread_mutex_unlock(&m->mu);
            uint8_t resp[16];
            int n = encode_seek_ok(resp, sizeof(resp), req_id, no);
            send_envelope(m->sock, WIRE_MSG_SEEK_OK, resp, (uint32_t)n);
        } else if (type == WIRE_MSG_WRITE) {
            uint16_t r, f; const uint8_t *d; uint32_t dl;
            if (decode_write_req(payload, plen, &r, &f, &d, &dl) != 0) { free(payload); break; }
            pthread_mutex_lock(&m->mu);
            int64_t *offset = &m->offsets[f % MOCK_FILE_SLOTS];
            int fail = m->write_fail_after && dl > m->write_fail_after;
            if (fail) dl = m->write_fail_after;
            if (m->content && *offset >= 0 && *offset + (int64_t)dl <= m->size && dl > 0) {
                memcpy(m->content + *offset, d, dl);
            }
            if (dl > m->max_write_len) m->max_write_len = dl;
            *offset += dl;
            m->writes++;
            uint32_t reported = dl + (uint32_t)m->write_ok_extra;
            pthread_mutex_unlock(&m->mu);
            if (fail) {
                mock_send_error(m->sock, req_id, 28 /* ENOSPC */);
            } else {
                uint8_t resp[16];
                int n = encode_write_ok(resp, sizeof(resp), req_id, reported);
                send_envelope(m->sock, WIRE_MSG_WRITE_OK, resp, (uint32_t)n);
            }
        } else if (type == WIRE_MSG_CLOSE) {
            uint8_t resp[8];
            int n = encode_reqid_resp(resp, sizeof(resp), req_id);
            send_envelope(m->sock, WIRE_MSG_CLOSE_OK, resp, (uint32_t)n);
        } else if (type == WIRE_MSG_FSTAT) {
            uint8_t resp[16];
            int n = encode_fstat_ok(resp, sizeof(resp), req_id, m->size, 0100644);
            send_envelope(m->sock, WIRE_MSG_FSTAT_OK, resp, (uint32_t)n);
        } else if (type == WIRE_MSG_FTRUNCATE) {
            uint16_t r, f; int64_t len;
            if (decode_ftruncate_req(payload, plen, &r, &f, &len) == 0) {
                pthread_mutex_lock(&m->mu);
                if (len >= 0 && len <= m->size) m->size = len;
                pthread_mutex_unlock(&m->mu);
            }
            uint8_t resp[8];
            int n = encode_reqid_resp(resp, sizeof(resp), req_id);
            send_envelope(m->sock, WIRE_MSG_FTRUNCATE_OK, resp, (uint32_t)n);
        } else if (type == WIRE_MSG_STAT || type == WIRE_MSG_LISTDIR) {
            char path[256];
            if (decode_unlink_req(payload, plen, &(uint16_t){0}, path, sizeof(path)) != 0) {
                free(payload); break;
            }
            mock_path_op(m, type, req_id, path);
        }
        free(payload);
    }
    return NULL;
}

static int mock_reads(mock_server *m) {
    pthread_mutex_lock(&m->mu);
    int v = m->reads;
    pthread_mutex_unlock(&m->mu);
    return v;
}
static int mock_seeks(mock_server *m) {
    pthread_mutex_lock(&m->mu);
    int v = m->seeks;
    pthread_mutex_unlock(&m->mu);
    return v;
}
static int mock_past_eof_reads(mock_server *m) {
    pthread_mutex_lock(&m->mu);
    int v = m->past_eof_reads;
    pthread_mutex_unlock(&m->mu);
    return v;
}
/* Change the simulated file's size after open (a writer appending to it, or
 * truncating it). content, if set, must hold the larger of the two sizes. */
static void mock_set_size(mock_server *m, int64_t size) {
    pthread_mutex_lock(&m->mu);
    m->size = size;
    pthread_mutex_unlock(&m->mu);
}

static int tunnel_start(mock_server *m, uint8_t *content, int64_t size,
                        uint32_t read_ahead, int read_ahead_explicit,
                        uint32_t range_cache_max, uint32_t short_read_cap) {
    int sv[2];
    if (socketpair(AF_UNIX, SOCK_STREAM, 0, sv) != 0) return -1;

    memset(m, 0, sizeof(*m));
    m->sock = sv[1];
    m->content = content;
    m->size = size;
    m->short_read_cap = short_read_cap;
    pthread_mutex_init(&m->mu, NULL);

    if (pthread_create(&m->thread, NULL, mock_server_thread, m) != 0) {
        close(sv[0]); close(sv[1]);
        pthread_mutex_destroy(&m->mu);
        return -1;
    }
    if (fio_test_set_tunnel(sv[0], read_ahead, read_ahead_explicit, range_cache_max) != 0) {
        /* Undo everything here: callers fail with plain ASSERT, never
         * tunnel_stop, when tunnel_start fails. */
        close(sv[0]);                  /* mock server sees EOF and exits */
        pthread_join(m->thread, NULL);
        close(sv[1]);
        pthread_mutex_destroy(&m->mu);
        return -1;
    }
    return 0;
}

static void tunnel_stop(mock_server *m) {
    fio_test_teardown();          /* closes fio's socket end, joins reader thread */
    pthread_join(m->thread, NULL); /* server sees EOF and exits */
    close(m->sock);
    pthread_mutex_destroy(&m->mu);
}

/* Tear down the tunnel before failing, so a failed assertion never leaves a
 * live reader/server thread for the next test to race against. Only valid
 * after tunnel_start succeeded; check tunnel_start itself with ASSERT. */
#define TUN_ASSERT(cond) do { if (!(cond)) { \
    printf("ASSERT FAILED: %s (line %d) ", #cond, __LINE__); \
    tunnel_stop(&srv); return 1; } } while(0)

/* Fill a buffer with the verification pattern starting at file offset `base`. */
static int tun_verify(const uint8_t *buf, int64_t base, int64_t len) {
    for (int64_t i = 0; i < len; i++) {
        if (buf[i] != tun_pat(base + i)) return 0;
    }
    return 1;
}

TEST(tunnel_read_ahead_sequential) {
    /* Many small reads should be satisfied with far fewer server round-trips. */
    enum { SIZE = 4096 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 512, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[SIZE];
    int64_t total = 0;
    int fio_reads = 0;
    for (;;) {
        ssize_t n = fio_read(fd, got + total, 64);
        TUN_ASSERT(n >= 0);
        fio_reads++;
        if (n == 0) break;
        total += n;
        TUN_ASSERT(total <= SIZE);
    }
    TUN_ASSERT(total == SIZE);
    TUN_ASSERT(tun_verify(got, 0, SIZE));
    TUN_ASSERT(fio_read(fd, got, 64) == 0); /* stays at EOF */

    TUN_ASSERT(fio_close(fd) == 0);
    int server_reads = mock_reads(&srv);
    tunnel_stop(&srv);

    /* ~64 fio_read calls, but read-ahead should collapse them to a handful. */
    ASSERT(fio_reads >= 60);
    ASSERT(server_reads < 16);
    return 0;
}

TEST(tunnel_backward_seek_hits_cache) {
    /* Read once with read-ahead covering the whole file, seek back, re-read:
     * the backward seek and re-read must be served from cache (no new server
     * seek, no new server read). */
    enum { SIZE = 2048 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, SIZE, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[512];
    TUN_ASSERT(fio_read(fd, got, 512) == 512);          /* fills cache [0,2048) */
    TUN_ASSERT(tun_verify(got, 0, 512));
    TUN_ASSERT(mock_reads(&srv) == 1);

    off_t pos = fio_lseek(fd, 0, SEEK_SET);             /* backward, into cache */
    TUN_ASSERT(pos == 0);
    TUN_ASSERT(fio_read(fd, got, 512) == 512);
    TUN_ASSERT(tun_verify(got, 0, 512));

    pos = fio_lseek(fd, 1000, SEEK_SET);                /* forward, into cache */
    TUN_ASSERT(pos == 1000);
    TUN_ASSERT(fio_read(fd, got, 200) == 200);
    TUN_ASSERT(tun_verify(got, 1000, 200));

    int server_reads = mock_reads(&srv);
    int server_seeks = mock_seeks(&srv);
    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);

    ASSERT(server_reads == 1); /* whole file fetched once, no refetch */
    ASSERT(server_seeks == 0); /* every seek resolved from cache */
    return 0;
}

TEST(tunnel_seek_outside_cache_refetches) {
    /* A seek past the cached window must issue a real remote seek and read. */
    enum { SIZE = 8192 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 1024, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[256];
    TUN_ASSERT(fio_read(fd, got, 100) == 100);   /* cache [0,1024) */
    TUN_ASSERT(tun_verify(got, 0, 100));

    off_t pos = fio_lseek(fd, 5000, SEEK_SET);   /* outside cache */
    TUN_ASSERT(pos == 5000);
    TUN_ASSERT(mock_seeks(&srv) == 1);

    TUN_ASSERT(fio_read(fd, got, 100) == 100);
    TUN_ASSERT(tun_verify(got, 5000, 100));

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_short_reads_no_corruption) {
    /* The server never returns more than 500 bytes per READ even though the
     * client requests up to 2 KiB and more is available. fio.c must not take
     * these short replies for EOF; verify no data is dropped or corrupted and
     * reads still end cleanly at true EOF. */
    enum { SIZE = 4096 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 2048, 1, 64 * 1024 * 1024, 500) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[SIZE];
    int64_t total = 0;
    for (;;) {
        ssize_t n = fio_read(fd, got + total, 256);
        TUN_ASSERT(n >= 0);
        if (n == 0) break;
        total += n;
        TUN_ASSERT(total <= SIZE);
    }
    TUN_ASSERT(total == SIZE);
    TUN_ASSERT(tun_verify(got, 0, SIZE));

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_write_invalidates_cache) {
    /* After a write, a cached read region must be refetched so the reader sees
     * the new bytes rather than stale cached ones. */
    enum { SIZE = 2048 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, SIZE, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDWR, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[256];
    TUN_ASSERT(fio_read(fd, got, 100) == 100);    /* cache covers [0,2048) incl [100,110) */
    TUN_ASSERT(tun_verify(got, 0, 100));

    /* Overwrite [100,110) with a marker; server stores it. */
    uint8_t marker[10];
    for (int i = 0; i < 10; i++) marker[i] = 0xAB;
    TUN_ASSERT(fio_write(fd, marker, 10) == 10);   /* writes at offset 100 */

    off_t pos = fio_lseek(fd, 0, SEEK_SET);
    TUN_ASSERT(pos == 0);
    TUN_ASSERT(fio_read(fd, got, 200) == 200);
    TUN_ASSERT(tun_verify(got, 0, 100));           /* unchanged prefix */
    for (int i = 0; i < 10; i++) TUN_ASSERT(got[100 + i] == 0xAB); /* fresh, not stale */
    TUN_ASSERT(tun_verify(got + 110, 110, 90));    /* unchanged suffix */

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_close_with_inflight_prefetch) {
    /* Consuming a full cache block triggers an async prefetch; closing while it
     * is in flight must cancel and free it cleanly (leak/UAF caught by ASan). */
    enum { SIZE = 8192 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 512, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[512];
    TUN_ASSERT(fio_read(fd, got, 512) == 512); /* consumes whole block -> prefetch */
    TUN_ASSERT(tun_verify(got, 0, 512));

    TUN_ASSERT(fio_close(fd) == 0);            /* cancels the in-flight prefetch */
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_seek_end_and_read) {
    /* SEEK_END is computed from the cached size for read-only files. */
    enum { SIZE = 1000 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 4096, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    off_t end = fio_lseek(fd, 0, SEEK_END);
    TUN_ASSERT(end == SIZE);

    off_t pos = fio_lseek(fd, -100, SEEK_END);
    TUN_ASSERT(pos == SIZE - 100);

    uint8_t got[100];
    TUN_ASSERT(fio_read(fd, got, 100) == 100);
    TUN_ASSERT(tun_verify(got, SIZE - 100, 100));
    TUN_ASSERT(fio_read(fd, got, 100) == 0); /* at EOF */

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_oneshot_read_larger_than_file) {
    /* A single read larger than the file returns all bytes then EOF. */
    enum { SIZE = 300 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 512, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[4096];
    ssize_t n = fio_read(fd, got, sizeof(got));
    TUN_ASSERT(n == SIZE);
    TUN_ASSERT(tun_verify(got, 0, SIZE));
    TUN_ASSERT(fio_read(fd, got, sizeof(got)) == 0);

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_no_prefetch_past_eof_sequential) {
    /* Reading a file exactly to its end must not leave a speculative READ for
     * the bytes past it in flight. Only the caller's own read at EOF may ask
     * the server for more. */
    enum { SIZE = 4096 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 512, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[SIZE];
    int64_t total = 0;
    while (total < SIZE) {
        ssize_t n = fio_read(fd, got + total, 64);
        TUN_ASSERT(n > 0);
        total += n;
    }
    TUN_ASSERT(total == SIZE);
    TUN_ASSERT(tun_verify(got, 0, SIZE));
    TUN_ASSERT(!fio_test_prefetch_in_flight(fd));

    TUN_ASSERT(fio_read(fd, got, 64) == 0); /* the caller's own read at EOF */
    TUN_ASSERT(fio_close(fd) == 0);
    int past_eof = mock_past_eof_reads(&srv); /* final: close waited on all replies */
    tunnel_stop(&srv);

    ASSERT_EQ(past_eof, 1);
    return 0;
}

TEST(tunnel_no_read_past_eof_tail) {
    /* Demuxers read indexes stored at the end of a file (mp4 moov, mkv cues),
     * then seek back without ever reading at EOF. With the production
     * read-ahead defaults, neither a small file nor a >= 1 GiB file (which
     * prefetches without waiting for half the block) may ask for bytes past
     * the end. */
    const int64_t sizes[] = { 3LL * 1024 * 1024, 2LL * 1024 * 1024 * 1024 };
    static uint8_t got[32768];

    for (size_t s = 0; s < sizeof(sizes) / sizeof(sizes[0]); s++) {
        int64_t size = sizes[s];
        int64_t from = size - 200 * 1024;

        mock_server srv;
        /* A failed start leaves no tunnel to tear down. */
        ASSERT(tunnel_start(&srv, NULL, size, 2 * 1024 * 1024, 0,
                            256u * 1024 * 1024, 0) == 0);

        int fd = fio_open("/movie.mp4", O_RDONLY, 0);
        TUN_ASSERT(fd >= 0);
        TUN_ASSERT(fio_lseek(fd, from, SEEK_SET) == from);

        for (int64_t pos = from; pos < size;) {
            ssize_t n = fio_read(fd, got, sizeof(got));
            TUN_ASSERT(n > 0);
            TUN_ASSERT(tun_verify(got, pos, n));
            pos += n;
        }
        TUN_ASSERT(!fio_test_prefetch_in_flight(fd));
        TUN_ASSERT(fio_lseek(fd, 0, SEEK_SET) == 0);

        TUN_ASSERT(fio_close(fd) == 0);
        int past_eof = mock_past_eof_reads(&srv);
        tunnel_stop(&srv);

        ASSERT_EQ(past_eof, 0);
    }
    return 0;
}

TEST(tunnel_short_reads_keep_size) {
    /* A short reply that still carries data is not EOF (network or FUSE
     * storage can return one mid-file). The size fstat reports, which ffmpeg
     * uses for AVSEEK_SIZE, and SEEK_END must stay at the real file size. */
    enum { SIZE = 4096 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 2048, 1, 64 * 1024 * 1024, 500) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[SIZE];
    TUN_ASSERT(fio_read(fd, got, 100) == 100); /* server replies with 500 of 2048 */
    TUN_ASSERT(tun_verify(got, 0, 100));

    struct stat st;
    TUN_ASSERT(fio_fstat(fd, &st) == 0);
    TUN_ASSERT(st.st_size == SIZE);
    TUN_ASSERT(fio_lseek(fd, 0, SEEK_END) == SIZE);
    TUN_ASSERT(fio_lseek(fd, 100, SEEK_SET) == 100);

    int64_t total = 100;
    for (;;) {
        ssize_t n = fio_read(fd, got + total, 256);
        TUN_ASSERT(n >= 0);
        if (n == 0) break;
        total += n;
        TUN_ASSERT(total <= SIZE);
    }
    TUN_ASSERT(total == SIZE);
    TUN_ASSERT(tun_verify(got, 0, SIZE));
    TUN_ASSERT(fio_fstat(fd, &st) == 0);
    TUN_ASSERT(st.st_size == SIZE);

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_growing_file_keeps_prefetching) {
    /* A file still being written grows past the size reported at open. Reads
     * past that size must return the new bytes, prefetch must resume past it
     * once the growth has been seen, and fstat must cover what was read. */
    enum { INITIAL = 4096, FINAL = 8192 };
    uint8_t content[FINAL];
    for (int i = 0; i < FINAL; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, INITIAL, 512, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/recording.ts", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[FINAL];
    int64_t total = 0;
    while (total < INITIAL) {
        ssize_t n = fio_read(fd, got + total, 64);
        TUN_ASSERT(n > 0);
        total += n;
    }
    TUN_ASSERT(!fio_test_prefetch_in_flight(fd)); /* stopped at the open-time size */

    mock_set_size(&srv, FINAL); /* the writer appends */

    int saw_prefetch = 0;
    for (;;) {
        ssize_t n = fio_read(fd, got + total, 64);
        TUN_ASSERT(n >= 0);
        if (n == 0) break;
        total += n;
        TUN_ASSERT(total <= FINAL);
        if (fio_test_prefetch_in_flight(fd)) saw_prefetch = 1;
    }
    TUN_ASSERT(total == FINAL);
    TUN_ASSERT(tun_verify(got, 0, FINAL));
    TUN_ASSERT(saw_prefetch);

    struct stat st;
    TUN_ASSERT(fio_fstat(fd, &st) == 0);
    TUN_ASSERT(st.st_size == FINAL);

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_truncated_file_empty_reply_sets_size) {
    /* An empty reply is real EOF: if the file shrank after open, reads end
     * there and the reported size drops to match. */
    enum { INITIAL = 4096, SHRUNK = 1000 };
    uint8_t content[INITIAL];
    for (int i = 0; i < INITIAL; i++) content[i] = tun_pat(i);

    mock_server srv;
    ASSERT(tunnel_start(&srv, content, INITIAL, 512, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    mock_set_size(&srv, SHRUNK);

    uint8_t got[INITIAL];
    int64_t total = 0;
    for (;;) {
        ssize_t n = fio_read(fd, got + total, 64);
        TUN_ASSERT(n >= 0);
        if (n == 0) break;
        total += n;
        TUN_ASSERT(total <= SHRUNK);
    }
    TUN_ASSERT(total == SHRUNK);
    TUN_ASSERT(tun_verify(got, 0, SHRUNK));

    struct stat st;
    TUN_ASSERT(fio_fstat(fd, &st) == 0);
    TUN_ASSERT(st.st_size == SHRUNK);

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_req_id_skips_parked_prefetch) {
    /* A parked prefetch keeps its request id while the 16-bit counter moves
     * on. When the counter comes back around, a new request must not get the
     * same id: the reader thread matches replies by id and would give the
     * new request's reply to the prefetch slot, leaving the request waiting
     * forever. */
    enum { SIZE = 8192 };
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, SIZE, 512, 1, 64 * 1024 * 1024, 0) == 0);

    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    uint8_t got[512];
    TUN_ASSERT(fio_read(fd, got, 300) == 300); /* past 50% of the block: prefetch */
    int parked = fio_test_prefetch_req_id(fd);
    TUN_ASSERT(parked > 0);

    fio_test_set_next_req_id((uint16_t)parked);
    struct stat st;
    TUN_ASSERT(fio_stat("/movie.mkv", &st) == 0);
    TUN_ASSERT(st.st_size == SIZE);
    pthread_mutex_lock(&srv.mu);
    uint16_t seen = srv.last_req_id;
    pthread_mutex_unlock(&srv.mu);
    TUN_ASSERT(seen != parked);

    fio_test_set_next_req_id(0);        /* id 0 is never used */
    TUN_ASSERT(fio_stat("/movie.mkv", &st) == 0);
    pthread_mutex_lock(&srv.mu);
    seen = srv.last_req_id;
    pthread_mutex_unlock(&srv.mu);
    TUN_ASSERT(seen != 0);

    /* The prefetched bytes still belong to the prefetch. */
    TUN_ASSERT(fio_read(fd, got, 212) == 212);
    TUN_ASSERT(tun_verify(got, 300, 212));
    TUN_ASSERT(fio_read(fd, got, 512) == 512);
    TUN_ASSERT(tun_verify(got, 512, 512));

    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_prefetches_never_starve_requests) {
    /* Every open file parks a prefetch. With the old 64-slot table the 65th
     * file's read failed with ENOMEM; now prefetches stop short of the slots
     * kept for synchronous requests. */
    enum { SIZE = 8192 };
    int nfiles = fio_test_max_files();
    int fds[1024];
    mock_server srv;
    ASSERT(nfiles <= 1024);
    ASSERT(tunnel_start(&srv, NULL, SIZE, 512, 1, 64 * 1024 * 1024, 0) == 0);

    uint8_t got[512];
    for (int i = 0; i < nfiles; i++) {
        fds[i] = fio_open("/movie.mkv", O_RDONLY, 0);
        TUN_ASSERT(fds[i] >= 0);
        TUN_ASSERT(fio_read(fds[i], got, 300) == 300);
        TUN_ASSERT(tun_verify(got, 0, 300));
    }
    TUN_ASSERT(fio_test_pending_used() <= fio_test_max_pending() - 64);
    for (int i = 0; i < nfiles; i++) {
        struct stat st;
        TUN_ASSERT(fio_fstat(fds[i], &st) == 0);
        TUN_ASSERT(fio_read(fds[i], got, 512) == 212);
        TUN_ASSERT(tun_verify(got, 300, 212));
        TUN_ASSERT(fio_read(fds[i], got, 512) == 512);
        TUN_ASSERT(tun_verify(got, 512, 512));
    }
    for (int i = 0; i < nfiles; i++) TUN_ASSERT(fio_close(fds[i]) == 0);
    TUN_ASSERT(fio_test_pending_used() == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_large_read_and_write_are_capped) {
    /* One read() or write() bigger than FIO_MAX_IO_BYTES becomes a short
     * read/write instead of a message over the 100 MiB limit. */
    uint32_t cap = fio_test_max_io_bytes();
    int64_t size = (int64_t)cap + 4096;
    size_t big = (size_t)cap * 2;
    uint8_t *buf = malloc(big);
    ASSERT(buf != NULL);

    mock_server srv;
    if (tunnel_start(&srv, NULL, size, 0, 1, 0, 0) != 0) { free(buf); ASSERT(0); }
    int fd = fio_open("/big.bin", O_RDONLY, 0);
    if (fd < 0) { free(buf); TUN_ASSERT(0); }
    ssize_t n = fio_read(fd, buf, big);
    int ok = n == (ssize_t)cap && tun_verify(buf, 0, n);
    n = fio_read(fd, buf, big);
    ok = ok && n == 4096 && tun_verify(buf, cap, 4096);
    fio_close(fd);

    fd = fio_open("/out.bin", O_WRONLY | O_CREAT, 0644);
    n = fd >= 0 ? fio_write(fd, buf, big) : -1;
    ok = ok && n == (ssize_t)cap && srv.max_write_len == cap && srv.max_read_req <= cap;
    if (fd >= 0) fio_close(fd);
    free(buf);
    TUN_ASSERT(ok);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_range_cache_budget_is_process_wide) {
    /* FFOIP_RANGE_CACHE_BYTES bounds the cache of all open files together. */
    enum { SIZE = 128 * 1024, BUDGET = 64 * 1024, NFILES = 4 };
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, SIZE, 16 * 1024, 1, BUDGET, 0) == 0);

    int fds[NFILES];
    uint8_t got[4096];
    for (int f = 0; f < NFILES; f++) {
        fds[f] = fio_open("/movie.mkv", O_RDONLY, 0);
        TUN_ASSERT(fds[f] >= 0);
        for (int64_t off = 0; off < SIZE; off += sizeof(got)) {
            TUN_ASSERT(fio_read(fds[f], got, sizeof(got)) == (ssize_t)sizeof(got));
            TUN_ASSERT(tun_verify(got, off, sizeof(got)));
            TUN_ASSERT(fio_test_range_cache_total() <= BUDGET);
        }
    }
    TUN_ASSERT(fio_test_range_cache_total() > 0);
    /* Cached data is still correct after cross-file eviction. */
    for (int f = 0; f < NFILES; f++) {
        TUN_ASSERT(fio_lseek(fds[f], 1000, SEEK_SET) == 1000);
        TUN_ASSERT(fio_read(fds[f], got, 100) == 100);
        TUN_ASSERT(tun_verify(got, 1000, 100));
        TUN_ASSERT(fio_close(fds[f]) == 0);
    }
    TUN_ASSERT(fio_test_range_cache_total() == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_dead_fails_fast) {
    /* The server hangs up mid-request: the waiting call gets EIO instead of
     * blocking forever, and later calls fail at once. */
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 4096, 512, 1, 0, 0) == 0);
    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    pthread_mutex_lock(&srv.mu);
    srv.hangup_on_read = 1;
    pthread_mutex_unlock(&srv.mu);

    uint8_t got[64];
    errno = 0;
    TUN_ASSERT(fio_read(fd, got, sizeof(got)) == -1);
    TUN_ASSERT(errno == EIO);
    struct stat st;
    errno = 0;
    TUN_ASSERT(fio_stat("/movie.mkv", &st) == -1);
    TUN_ASSERT(errno == EIO);
    errno = 0;
    TUN_ASSERT(fio_open("/other.mkv", O_RDONLY, 0) == -1);
    TUN_ASSERT(errno == EIO);
    TUN_ASSERT(fio_close(fd) == -1);   /* the vfd is released anyway */
    TUN_ASSERT(fio_test_pending_used() == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_file_id_wrap_skips_open_files) {
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 4096, 512, 1, 0, 0) == 0);

    int a = fio_open("/a.mkv", O_RDONLY, 0);
    TUN_ASSERT(a >= 0);
    uint16_t a_id = srv.last_open_file_id;
    TUN_ASSERT(a_id == 1);

    fio_test_set_next_file_id(a_id);   /* counter wrapped onto a's id */
    int b = fio_open("/b.mkv", O_RDONLY, 0);
    TUN_ASSERT(b >= 0);
    TUN_ASSERT(srv.last_open_file_id == 2);

    fio_test_set_next_file_id(0);      /* 0 is never used */
    int c = fio_open("/c.mkv", O_RDONLY, 0);
    TUN_ASSERT(c >= 0);
    TUN_ASSERT(srv.last_open_file_id == 3);

    TUN_ASSERT(fio_close(a) == 0);
    TUN_ASSERT(fio_close(b) == 0);
    TUN_ASSERT(fio_close(c) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_stat_and_check) {
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 777, 512, 1, 0, 0) == 0);

    struct stat st;
    TUN_ASSERT(fio_stat("/movie.mkv", &st) == 0);
    TUN_ASSERT(S_ISREG(st.st_mode));
    TUN_ASSERT((st.st_mode & 0777) == 0640);
    TUN_ASSERT(st.st_size == 777);
    TUN_ASSERT((int64_t)st.st_mtime == MOCK_STAT_MTIME);
    TUN_ASSERT(fio_check("/movie.mkv") == FIO_R_OK);

    TUN_ASSERT(fio_stat("/fonts", &st) == 0);
    TUN_ASSERT(S_ISDIR(st.st_mode));
    TUN_ASSERT(fio_check("/fonts") == (FIO_R_OK | FIO_X_OK));

    errno = 0;
    TUN_ASSERT(fio_stat("/missing", &st) == -1);
    TUN_ASSERT(errno == ENOENT);
    errno = 0;
    TUN_ASSERT(fio_check("/missing") == -1);
    TUN_ASSERT(errno == ENOENT);
    TUN_ASSERT(fio_tunneled() == 1);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_listdir) {
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 777, 512, 1, 0, 0) == 0);

    fio_dir *d = fio_opendir("/fonts");
    TUN_ASSERT(d != NULL);
    const char *want[] = { "a.ttf", "sub", "link.otf", ".hidden" };
    const int want_type[] = { FIO_DT_FILE, FIO_DT_DIR, FIO_DT_LINK, FIO_DT_FILE };
    fio_dirent e;
    int i = 0, rc;
    while ((rc = fio_readdir(d, &e)) == 1) {
        if (i >= 4) break;
        if (strcmp(e.name, want[i]) != 0 || e.type != want_type[i] || !e.has_stat) break;
        if (i == 0 && (e.size != 1234 || e.perm != 0644)) break;
        i++;
    }
    fio_closedir(d);
    TUN_ASSERT(rc == 0);
    TUN_ASSERT(i == 4);

    errno = 0;
    TUN_ASSERT(fio_opendir("/missing") == NULL);
    TUN_ASSERT(errno == ENOENT);

    /* A listing cut short by a bad payload ends in EIO, not a read past it. */
    d = fio_opendir("/truncated");
    TUN_ASSERT(d != NULL);
    rc = fio_readdir(d, &e);
    int rc2 = fio_readdir(d, &e);
    int err2 = errno;
    int rc3 = fio_readdir(d, &e);
    fio_closedir(d);
    TUN_ASSERT(rc == 1);
    TUN_ASSERT(rc2 == -1 && err2 == EIO);
    TUN_ASSERT(rc3 == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_fstat_reports_fifo) {
    /* file.c marks a file as streamed (unseekable) when fstat says FIFO. */
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 0, 512, 1, 64 * 1024 * 1024, 0) == 0);
    srv.open_mode = 0010600;
    int fd = fio_open("/pipe", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    struct stat st;
    TUN_ASSERT(fio_fstat(fd, &st) == 0);
    TUN_ASSERT(S_ISFIFO(st.st_mode));
    TUN_ASSERT((st.st_mode & 0777) == 0600);

    /* Data arriving on the pipe: no prefetch is parked behind it. */
    mock_set_size(&srv, 4096);
    uint8_t got[100];
    for (int i = 0; i < 20; i++) {
        TUN_ASSERT(fio_read(fd, got, sizeof(got)) > 0);
        TUN_ASSERT(!fio_test_prefetch_in_flight(fd));
    }
    TUN_ASSERT(fio_close(fd) == 0);

    srv.open_mode = 0;
    fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    TUN_ASSERT(fio_fstat(fd, &st) == 0);
    TUN_ASSERT(S_ISREG(st.st_mode));
    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

/* A device reports size 0 at open, like fstat(2) locally. Reading from it
 * must not make fstat report the bytes read so far as its size: avio_size()
 * would then give device inputs a made-up length and duration. */
TEST(tunnel_fstat_device_size_stays_zero) {
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 0, 512, 1, 64 * 1024 * 1024, 0) == 0);
    srv.open_mode = 0020600; /* character device */
    int fd = fio_open("/dev/zero", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    mock_set_size(&srv, 300000);
    static uint8_t got[65536];
    int64_t total = 0;
    ssize_t n;
    while ((n = fio_read(fd, got, sizeof(got))) > 0) total += n;
    TUN_ASSERT(total == 300000);
    struct stat st;
    TUN_ASSERT(fio_fstat(fd, &st) == 0);
    TUN_ASSERT(S_ISCHR(st.st_mode));
    TUN_ASSERT(st.st_size == 0);

    /* After seeking back, a large read isn't cut short at that made-up
     * size either. */
    TUN_ASSERT(fio_lseek(fd, 0, SEEK_SET) == 0);
    TUN_ASSERT(fio_read(fd, got, 1000) == 1000);
    TUN_ASSERT(tun_verify(got, 0, 1000));
    TUN_ASSERT(fio_fstat(fd, &st) == 0 && st.st_size == 0);
    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

/* A failed seek on a pipe leaves its position alone, as lseek(2) does:
 * later reads and writes go on without a SEEK first, and lseek never
 * "succeeds" by replaying data fio has already read. */
TEST(tunnel_fifo_failed_seek_keeps_stream) {
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 4 << 20, 64 * 1024, 1, 256u << 20, 0) == 0);
    pthread_mutex_lock(&srv.mu);
    srv.open_mode = 0010600;
    srv.seek_error = 29; /* ESPIPE */
    pthread_mutex_unlock(&srv.mu);

    int fd = fio_open("/client/in.fifo", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    uint8_t buf[100];
    TUN_ASSERT(fio_read(fd, buf, sizeof(buf)) == 100);
    TUN_ASSERT(tun_verify(buf, 0, 100));
    errno = 0;
    TUN_ASSERT(fio_lseek(fd, 0, SEEK_SET) == -1 && errno == ESPIPE); /* 0 is in fio's buffer */
    errno = 0;
    TUN_ASSERT(fio_lseek(fd, 50000, SEEK_SET) == -1 && errno == ESPIPE);
    int seeks = mock_seeks(&srv);
    TUN_ASSERT(fio_read(fd, buf, sizeof(buf)) == 100);
    TUN_ASSERT(tun_verify(buf, 100, 100));
    TUN_ASSERT(fio_read(fd, buf, sizeof(buf)) == 100);
    TUN_ASSERT(mock_seeks(&srv) == seeks); /* no SEEK before the reads */
    fio_close(fd);

    int wfd = fio_open("/client/out.fifo", O_WRONLY, 0);
    TUN_ASSERT(wfd >= 0);
    TUN_ASSERT(fio_write(wfd, buf, 100) == 100);
    errno = 0;
    TUN_ASSERT(fio_lseek(wfd, 0, SEEK_SET) == -1 && errno == ESPIPE);
    seeks = mock_seeks(&srv);
    TUN_ASSERT(fio_write(wfd, buf, 100) == 100);
    TUN_ASSERT(fio_write(wfd, buf, 100) == 100);
    TUN_ASSERT(mock_seeks(&srv) == seeks);
    fio_close(wfd);
    tunnel_stop(&srv);
    return 0;
}

/* Data read from a pipe can't be read again, so none of it is kept in the
 * process-wide range cache (where it would crowd out regular files). */
TEST(tunnel_fifo_data_not_range_cached) {
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 8 << 20, 512 * 1024, 1, 256u << 20, 0) == 0);
    pthread_mutex_lock(&srv.mu);
    srv.open_mode = 0010600;
    pthread_mutex_unlock(&srv.mu);
    int fd = fio_open("/client/live.fifo", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    static uint8_t buf[32768];
    int64_t total = 0;
    ssize_t n;
    while ((n = fio_read(fd, buf, sizeof(buf))) > 0) total += n;
    TUN_ASSERT(total == 8 << 20);
    TUN_ASSERT(fio_test_range_cache_total() == 0);
    fio_close(fd);
    tunnel_stop(&srv);
    return 0;
}

/* A file still being written: a prefetch sent at the writer's position comes
 * back empty, and the writer appends before the reader gets there. read(2)
 * would return the new data, so fio must not report end of file. */
TEST(tunnel_growing_file_empty_prefetch_is_not_eof) {
    enum { OPEN_SIZE = 100000, RA = 16384, FINAL = 300000 };
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, OPEN_SIZE, RA, 1, 64u << 20, 0) == 0);
    int fd = fio_open("/client/recording.ts", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    uint8_t buf[1000];
    int64_t pos = 0;
    ssize_t n;
    while (pos < OPEN_SIZE && (n = fio_read(fd, buf, sizeof(buf))) > 0) pos += n;
    TUN_ASSERT(pos == OPEN_SIZE);
    mock_set_size(&srv, OPEN_SIZE + RA);  /* the writer appends */
    n = fio_read(fd, buf, sizeof(buf));    /* a synchronous read sees it grow */
    TUN_ASSERT(n > 0);
    pos += n;
    for (int i = 0; i < 1000 && !fio_test_prefetch_in_flight(fd); i++) {
        n = fio_read(fd, buf, sizeof(buf));
        TUN_ASSERT(n > 0);
        pos += n;
    }
    TUN_ASSERT(fio_test_prefetch_in_flight(fd));
    usleep(100000);                        /* the prefetch is answered: empty */
    mock_set_size(&srv, FINAL);            /* then the writer appends more */
    while ((n = fio_read(fd, buf, sizeof(buf))) > 0) {
        TUN_ASSERT(tun_verify(buf, pos, n));
        pos += n;
    }
    TUN_ASSERT(n == 0);
    TUN_ASSERT(pos == FINAL);
    fio_close(fd);
    tunnel_stop(&srv);
    return 0;
}

/* Errors the client reports keep their meaning; offsets that don't fit fail
 * as lseek(2) does, without overflowing; offsets and sizes no real file has
 * are broken replies. */
TEST(tunnel_seek_errors_and_overflow) {
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 1 << 20, 0, 1, 0, 0) == 0);
    int fd = fio_open("/client/a.bin", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);
    static const struct { int32_t wire; int local; } errs[] = {
        { 9, EBADF }, { 27, EFBIG }, { 29, ESPIPE }, { 32, EPIPE },
        { 75, EOVERFLOW }, { 122, EDQUOT },
    };
    for (size_t i = 0; i < sizeof(errs) / sizeof(errs[0]); i++) {
        pthread_mutex_lock(&srv.mu);
        srv.seek_error = errs[i].wire;
        pthread_mutex_unlock(&srv.mu);
        errno = 0;
        TUN_ASSERT(fio_lseek(fd, 12345 + (off_t)i, SEEK_SET) == -1);
        TUN_ASSERT(errno == errs[i].local);
    }
    pthread_mutex_lock(&srv.mu);
    srv.seek_error = 0;
    pthread_mutex_unlock(&srv.mu);

    TUN_ASSERT(fio_lseek(fd, 100, SEEK_SET) == 100);
    errno = 0;
    TUN_ASSERT(fio_lseek(fd, INT64_MAX, SEEK_CUR) == -1 && errno == EOVERFLOW);
    errno = 0;
    TUN_ASSERT(fio_lseek(fd, INT64_MAX, SEEK_END) == -1 && errno == EOVERFLOW);
    errno = 0;
    TUN_ASSERT(fio_lseek(fd, INT64_MIN, SEEK_CUR) == -1 && errno == EINVAL);
    TUN_ASSERT(fio_lseek(fd, 0, SEEK_CUR) == 100);

    pthread_mutex_lock(&srv.mu);
    srv.seek_reply_huge = 1;
    pthread_mutex_unlock(&srv.mu);
    errno = 0;
    TUN_ASSERT(fio_lseek(fd, 4096, SEEK_SET) == -1 && errno == EIO);
    pthread_mutex_lock(&srv.mu);
    srv.seek_reply_huge = 0;
    pthread_mutex_unlock(&srv.mu);
    fio_close(fd);

    mock_set_size(&srv, INT64_MAX - 1);
    errno = 0;
    TUN_ASSERT(fio_open("/client/huge.bin", O_RDONLY, 0) == -1 && errno == EIO);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_failed_write_reseeks) {
    /* A WRITE that stored part of its data and then failed (disk full) must
     * not shift later writes: fio no longer knows the server's position and
     * seeks before the next request. */
    enum { SIZE = 64 };
    uint8_t content[SIZE];
    memset(content, '.', SIZE);
    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 512, 1, 0, 0) == 0);
    int fd = fio_open("/out.bin", O_RDWR, 0);
    TUN_ASSERT(fd >= 0);

    pthread_mutex_lock(&srv.mu);
    srv.write_fail_after = 4;
    pthread_mutex_unlock(&srv.mu);
    errno = 0;
    TUN_ASSERT(fio_write(fd, "AAAAAAAA", 8) == -1);
    TUN_ASSERT(errno == ENOSPC);

    pthread_mutex_lock(&srv.mu);
    srv.write_fail_after = 0;
    pthread_mutex_unlock(&srv.mu);
    TUN_ASSERT(fio_write(fd, "BBBBBBBB", 8) == 8);
    TUN_ASSERT(memcmp(content, "BBBBBBBB", 8) == 0);
    TUN_ASSERT(content[8] == '.');
    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_bad_counts_are_eio) {
    /* A WRITE_OK claiming more than was sent, or a SEEK_OK with a negative
     * offset, is a broken reply, not a result to pass on. */
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 8192, 512, 1, 0, 0) == 0);
    int fd = fio_open("/out.bin", O_RDWR, 0);
    TUN_ASSERT(fd >= 0);

    pthread_mutex_lock(&srv.mu);
    srv.write_ok_extra = 1;
    pthread_mutex_unlock(&srv.mu);
    errno = 0;
    TUN_ASSERT(fio_write(fd, "abcd", 4) == -1);
    TUN_ASSERT(errno == EIO);

    pthread_mutex_lock(&srv.mu);
    srv.write_ok_extra = 0;
    srv.seek_reply_negative = 1;
    pthread_mutex_unlock(&srv.mu);
    errno = 0;
    TUN_ASSERT(fio_lseek(fd, 4000, SEEK_SET) == -1);
    TUN_ASSERT(errno == EIO);
    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_real_fds_pass_through) {
    /* Real descriptors (pipe:, fd:) go to the kernel even when they are
     * large numbers; bad descriptors fail with EBADF. */
    struct rlimit rl;
    ASSERT(getrlimit(RLIMIT_NOFILE, &rl) == 0);
    int high = 10001;
    if (rl.rlim_cur <= (rlim_t)high) {
        struct rlimit raised = rl;
        raised.rlim_cur = (rl.rlim_max == RLIM_INFINITY || rl.rlim_max > 10100) ? 10100 : rl.rlim_max;
        if (setrlimit(RLIMIT_NOFILE, &raised) != 0 || raised.rlim_cur <= (rlim_t)high) {
            printf("(fd limit too low, high fd part skipped) ");
            high = -1;
        }
    }

    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 4096, 512, 1, 0, 0) == 0);
    int vfd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(vfd >= 0x40000000);

    int p[2];
    TUN_ASSERT(pipe(p) == 0);
    int rfd = p[0];
    if (high > 0) {
        TUN_ASSERT(dup2(p[0], high) == high);
        close(p[0]);
        rfd = high;
    }
    TUN_ASSERT(write(p[1], "hello", 5) == 5);
    char got[8];
    TUN_ASSERT(fio_read(rfd, got, sizeof(got)) == 5);
    TUN_ASSERT(memcmp(got, "hello", 5) == 0);
    TUN_ASSERT(fio_close(rfd) == 0);
    close(p[1]);

    errno = 0;
    TUN_ASSERT(fio_read(-1, got, 1) == -1 && errno == EBADF);
    TUN_ASSERT(fio_close(vfd) == 0);
    errno = 0;
    TUN_ASSERT(fio_read(vfd, got, 1) == -1 && errno == EBADF); /* closed vfd */
    tunnel_stop(&srv);
    if (high > 0) setrlimit(RLIMIT_NOFILE, &rl);
    return 0;
}

TEST(tunnel_seek_into_cancelled_prefetch_uses_cache) {
    /* Seeking into the range a prefetch just fetched cancels the prefetch;
     * its data is kept, so the seek and the read after it stay local. */
    enum { SIZE = 8192 };
    uint8_t content[SIZE];
    for (int i = 0; i < SIZE; i++) content[i] = tun_pat(i);
    mock_server srv;
    ASSERT(tunnel_start(&srv, content, SIZE, 1024, 1, 64 * 1024 * 1024, 0) == 0);
    int fd = fio_open("/movie.mkv", O_RDONLY, 0);
    TUN_ASSERT(fd >= 0);

    uint8_t got[200];
    TUN_ASSERT(fio_read(fd, got, 100) == 100);   /* cache [0,1024) */
    TUN_ASSERT(fio_lseek(fd, 600, SEEK_SET) == 600);
    TUN_ASSERT(fio_read(fd, got, 100) == 100);   /* past half: prefetch [1024,2048) */
    TUN_ASSERT(fio_test_prefetch_in_flight(fd));

    /* lseek cancels the prefetch, which waits for its reply. */
    TUN_ASSERT(fio_lseek(fd, 1500, SEEK_SET) == 1500);
    int reads = mock_reads(&srv);
    TUN_ASSERT(fio_read(fd, got, 200) == 200);
    TUN_ASSERT(tun_verify(got, 1500, 200));
    /* No SEEK, and no READ beyond the next prefetch (which may not have
     * reached the server yet). A cache miss would have needed a SEEK. */
    TUN_ASSERT(mock_seeks(&srv) == 0);
    TUN_ASSERT(mock_reads(&srv) <= reads + 1);
    TUN_ASSERT(fio_close(fd) == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(tunnel_listdir_skips_separators) {
    /* Names are joined onto the directory path by callers. */
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 777, 512, 1, 0, 0) == 0);
    fio_dir *d = fio_opendir("/fonts");
    TUN_ASSERT(d != NULL);
    fio_dirent e;
    int rc, bad = 0;
    while ((rc = fio_readdir(d, &e)) == 1) {
        if (strchr(e.name, '/')) bad = 1;
    }
    fio_closedir(d);
    TUN_ASSERT(rc == 0);
    TUN_ASSERT(!bad);
    tunnel_stop(&srv);
    return 0;
}

/* ---- FFOIP_PORT / FFOIP_TOKEN setup (fio_init_tunnel) ---- */

#define TEST_TOKEN "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

static int listen_loopback(int *port) {
    int s = socket(AF_INET, SOCK_STREAM, 0);
    if (s < 0) return -1;
    struct sockaddr_in a;
    memset(&a, 0, sizeof(a));
    a.sin_family = AF_INET;
    a.sin_addr.s_addr = htonl(0x7f000001);
    socklen_t len = sizeof(a);
    if (bind(s, (struct sockaddr *)&a, sizeof(a)) != 0 || listen(s, 4) != 0 ||
        getsockname(s, (struct sockaddr *)&a, &len) != 0) {
        close(s);
        return -1;
    }
    *port = ntohs(a.sin_port);
    return s;
}

/* With the tunnel failed closed, nothing touches this machine's files. */
static int check_failed_closed(const char *why) {
    int dead = 0;
    if (fio_test_init_from_env(&dead) != 2 || !dead) {
        printf("(%s: not failed closed) ", why);
        fio_test_teardown();
        return 0;
    }
    int ok = 1;
    char dir[] = "/tmp/fio_test_fc_XXXXXX";
    if (!mkdtemp(dir)) { fio_test_teardown(); return 0; }
    char path[256], other[256];
    snprintf(path, sizeof(path), "%s/new.ts", dir);
    snprintf(other, sizeof(other), "%s/existing.ts", dir);
    int fd = open(other, O_WRONLY | O_CREAT, 0644);
    if (fd >= 0) close(fd);

    errno = 0;
    if (fio_open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644) != -1 || errno != EIO) ok = 0;
    if (access(path, F_OK) == 0) ok = 0;            /* not created locally */
    errno = 0;
    if (fio_unlink(other) != -1 || errno != EIO) ok = 0;
    if (access(other, F_OK) != 0) ok = 0;           /* not deleted locally */
    struct stat st;
    errno = 0;
    if (fio_stat(other, &st) != -1 || errno != EIO) ok = 0;
    errno = 0;
    if (fio_check(other) != -1 || errno != EIO) ok = 0;
    errno = 0;
    if (fio_opendir(dir) != NULL || errno != EIO) ok = 0;
    errno = 0;
    if (fio_mkdir(path, 0755) != -1 || errno != EIO) ok = 0;
    if (fio_tunneled() != 1) ok = 0;

    fio_test_teardown();
    unlink(other);
    unlink(path);
    rmdir(path);
    rmdir(dir);
    if (!ok) printf("(%s) ", why);
    return ok;
}

TEST(tunnel_setup_fails_closed) {
    char port_str[16];
    setenv("FFOIP_TOKEN", TEST_TOKEN, 1);

    setenv("FFOIP_PORT", "not-a-port", 1);
    ASSERT(check_failed_closed("bad port"));
    setenv("FFOIP_PORT", "70000", 1);
    ASSERT(check_failed_closed("port out of range"));

    /* Nothing listening: connect is refused. */
    int port = 0;
    int l = listen_loopback(&port);
    ASSERT(l >= 0);
    close(l);
    snprintf(port_str, sizeof(port_str), "%d", port);
    setenv("FFOIP_PORT", port_str, 1);
    ASSERT(check_failed_closed("connect refused"));

    /* Listening, but no usable token: fio doesn't even connect. */
    l = listen_loopback(&port);
    ASSERT(l >= 0);
    snprintf(port_str, sizeof(port_str), "%d", port);
    setenv("FFOIP_PORT", port_str, 1);
    unsetenv("FFOIP_TOKEN");
    int ok = check_failed_closed("no token");
    setenv("FFOIP_TOKEN", "short", 1);
    ok = ok && check_failed_closed("short token");
    fcntl(l, F_SETFL, O_NONBLOCK);
    int c = accept(l, NULL, NULL);
    int connected = c >= 0;
    if (c >= 0) close(c);
    close(l);
    unsetenv("FFOIP_PORT");
    unsetenv("FFOIP_TOKEN");
    ASSERT(ok);
    ASSERT(!connected);

    /* No FFOIP_PORT: passthrough. */
    int dead = 1;
    ASSERT(fio_test_init_from_env(&dead) == 1 && !dead);
    fio_test_teardown();
    return 0;
}

static void *stat_thread(void *arg) {
    struct stat st;
    *(int *)arg = fio_stat("/x", &st) == 0 && st.st_size == 42;
    return NULL;
}

TEST(tunnel_setup_sends_token_first) {
    int port = 0;
    int l = listen_loopback(&port);
    ASSERT(l >= 0);
    char port_str[16];
    snprintf(port_str, sizeof(port_str), "%d", port);
    setenv("FFOIP_PORT", port_str, 1);
    setenv("FFOIP_TOKEN", TEST_TOKEN, 1);
    int dead = 1;
    int mode = fio_test_init_from_env(&dead);
    unsetenv("FFOIP_PORT");
    unsetenv("FFOIP_TOKEN");
    int c = accept(l, NULL, NULL);
    close(l);
    if (mode != 2 || dead || c < 0) {
        if (c >= 0) close(c);
        fio_test_teardown();
        ASSERT(0);
    }

    /* Requests must not wait for Nagle's algorithm. */
    int nodelay = fio_test_tunnel_nodelay();

    struct timeval tv = { 5, 0 };  /* fail, don't hang, if no hello comes */
    setsockopt(c, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
    uint8_t hello[68];
    int got_hello = read_full(c, hello, sizeof(hello)) == 0 &&
                    memcmp(hello, "FIO1", 4) == 0 &&
                    memcmp(hello + 4, TEST_TOKEN, 64) == 0;

    /* Requests follow, framed as usual. */
    int stat_ok = 0;
    pthread_t th;
    pthread_create(&th, NULL, stat_thread, &stat_ok);
    uint8_t type = 0, payload[64], resp[32];
    uint32_t plen = 0;
    int served = recv_envelope(c, &type, payload, sizeof(payload), &plen) == 0 &&
                 type == WIRE_MSG_STAT && plen >= 2;
    if (served) {
        uint16_t req_id = (uint16_t)((payload[0] << 8) | payload[1]);
        int n = encode_stat_ok(resp, sizeof(resp), req_id, 42, 0100644, 0, 1);
        send_envelope(c, WIRE_MSG_STAT_OK, resp, (uint32_t)n);
    } else {
        shutdown(c, SHUT_RDWR);
    }
    pthread_join(th, NULL);
    fio_test_teardown();
    close(c);
    ASSERT(got_hello);
    ASSERT(served);
    ASSERT(stat_ok);
    ASSERT(nodelay == 1);
    return 0;
}

typedef struct {
    int ok;
    int id;
} tun_worker_arg;

static void *tun_worker(void *p) {
    tun_worker_arg *a = (tun_worker_arg *)p;
    enum { SIZE = 256 * 1024 };
    uint8_t *got = malloc(7001);
    a->ok = got != NULL;
    for (int round = 0; a->ok && round < 4; round++) {
        int fd = fio_open("/movie.mkv", O_RDONLY, 0);
        if (fd < 0) { a->ok = 0; break; }
        int64_t off = 0;
        int jumped = 0;
        for (;;) {
            ssize_t n = fio_read(fd, got, 7001);
            if (n < 0) { a->ok = 0; break; }
            if (n == 0) break;
            if (!tun_verify(got, off, n)) { a->ok = 0; break; }
            off += n;
            if (!jumped && off > 100000 && (a->id + round) % 2) {
                /* jump back into the cached range once */
                jumped = 1;
                off = fio_lseek(fd, 4096, SEEK_SET);
                if (off != 4096) { a->ok = 0; break; }
            }
        }
        if (a->ok && off != SIZE) a->ok = 0;
        struct stat st;
        if (a->ok && (fio_fstat(fd, &st) != 0 || st.st_size != SIZE)) a->ok = 0;
        if (fio_close(fd) != 0) a->ok = 0;
    }
    free(got);
    return NULL;
}

TEST(tunnel_concurrent_files) {
    /* ffmpeg reads inputs and writes outputs from several threads at once.
     * Run under the sanitizer builds to catch races in the shared tables. */
    enum { NTHREADS = 8 };
    mock_server srv;
    ASSERT(tunnel_start(&srv, NULL, 256 * 1024, 16 * 1024, 1, 96 * 1024, 0) == 0);
    pthread_t th[NTHREADS];
    tun_worker_arg args[NTHREADS];
    for (int i = 0; i < NTHREADS; i++) {
        args[i].id = i;
        TUN_ASSERT(pthread_create(&th[i], NULL, tun_worker, &args[i]) == 0);
    }
    int ok = 1;
    for (int i = 0; i < NTHREADS; i++) {
        pthread_join(th[i], NULL);
        ok = ok && args[i].ok;
    }
    TUN_ASSERT(ok);
    TUN_ASSERT(fio_test_pending_used() == 0);
    TUN_ASSERT(fio_test_range_cache_total() == 0);
    tunnel_stop(&srv);
    return 0;
}

TEST(mode_from_wire_values) {
    ASSERT(S_ISREG(mode_from_wire(0100644)));
    ASSERT((mode_from_wire(0100644) & 0777) == 0644);
    ASSERT(S_ISDIR(mode_from_wire(0040755)));
    ASSERT(S_ISREG(mode_from_wire(0644)));           /* no type bits */
    ASSERT(S_ISDIR(mode_from_wire(0x80000000U | 0755))); /* old client: Go ModeDir */
    ASSERT((mode_from_wire(0x80000000U | 0755) & 0777) == 0755);
    ASSERT(S_ISLNK(mode_from_wire(0x08000000U | 0777))); /* Go ModeSymlink */
    ASSERT(S_ISREG(mode_from_wire(0x00800000U | 0644))); /* Go ModeSetuid only */
    return 0;
}

TEST(stat_ok_roundtrip) {
    uint8_t buf[32];
    ASSERT_EQ(encode_stat_ok(buf, sizeof(buf), 0xBEEF, 123456789012LL, 0100644, -5, 0x05), 23);
    ASSERT_EQ(encode_stat_ok(buf, 22, 1, 0, 0, 0, 0), -1);
    uint16_t r; int64_t size, mtime; uint32_t mode; uint8_t acc;
    ASSERT_EQ(decode_stat_ok(buf, 23, &r, &size, &mode, &mtime, &acc), 0);
    ASSERT_EQ(r, 0xBEEF);
    ASSERT_EQ(size, 123456789012LL);
    ASSERT_EQ(mode, 0100644);
    ASSERT_EQ(mtime, -5);
    ASSERT_EQ(acc, 0x05);
    ASSERT_EQ(decode_stat_ok(buf, 22, &r, &size, &mode, &mtime, &acc), -1);
    /* Same layout as Go's StatOkResponse: req_id, size, mode, mtime, access. */
    ASSERT_EQ(buf[0], 0xBE);
    ASSERT_EQ(buf[10], 0x00); ASSERT_EQ(buf[11], 0x00); ASSERT_EQ(buf[12], 0x81); ASSERT_EQ(buf[13], 0xA4);
    ASSERT_EQ(buf[22], 0x05);
    return 0;
}

TEST(listdir_roundtrip) {
    uint8_t buf[128];
    int pos = encode_listdir_ok_header(buf, sizeof(buf), 7, 2);
    ASSERT_EQ(pos, 6);
    int n = encode_listdir_entry(buf + pos, sizeof(buf) - pos, "font.ttf", 0100644, 99, 42);
    ASSERT_EQ(n, 2 + 8 + 20);
    pos += n;
    n = encode_listdir_entry(buf + pos, sizeof(buf) - pos, "d", 0040755, 0, 1);
    pos += n;
    ASSERT_EQ(encode_listdir_entry(buf, 10, "font.ttf", 0, 0, 0), -1);

    uint16_t r; uint32_t count;
    ASSERT_EQ(decode_listdir_header(buf, (uint32_t)pos, &r, &count), 6);
    ASSERT_EQ(r, 7);
    ASSERT_EQ(count, 2);
    uint32_t p = 6;
    const uint8_t *name; uint16_t nl; uint32_t mode; int64_t size, mtime;
    ASSERT_EQ(decode_listdir_entry(buf, (uint32_t)pos, &p, &name, &nl, &mode, &size, &mtime), 0);
    ASSERT_EQ(nl, 8);
    ASSERT_MEM_EQ(name, "font.ttf", 8);
    ASSERT_EQ(mode, 0100644);
    ASSERT_EQ(size, 99);
    ASSERT_EQ(mtime, 42);
    ASSERT_EQ(decode_listdir_entry(buf, (uint32_t)pos, &p, &name, &nl, &mode, &size, &mtime), 0);
    ASSERT_EQ(nl, 1);
    ASSERT_EQ(p, (uint32_t)pos);
    ASSERT_EQ(decode_listdir_entry(buf, (uint32_t)pos, &p, &name, &nl, &mode, &size, &mtime), -1);
    p = 6;
    ASSERT_EQ(decode_listdir_entry(buf, 6 + 2 + 8 + 19, &p, &name, &nl, &mode, &size, &mtime), -1);
    return 0;
}

TEST(path_req_layout) {
    uint8_t a[64], b[64];
    int n = encode_path_req(a, sizeof(a), 9, "/fonts");
    ASSERT_EQ(n, encode_unlink_req(b, sizeof(b), 9, "/fonts"));
    ASSERT_MEM_EQ(a, b, (size_t)n);
    return 0;
}

#undef TUN_ASSERT

/* ======================================================================
 * main
 * ====================================================================== */

int main(int argc, char *argv[]) {
    if (argc >= 3 && strcmp(argv[1], "--wire-test") == 0) {
        int port = atoi(argv[2]);
        if (port <= 0 || port > 65535) {
            fprintf(stderr, "invalid port: %s\n", argv[2]);
            return 1;
        }
        return wire_test_main(port);
    }

    printf("fio_test: running tests...\n\n");

    printf("--- Encode/Decode Round-Trips ---\n");
    RUN(open_req_roundtrip);
    RUN(read_req_roundtrip);
    RUN(write_req_roundtrip);
    RUN(seek_req_roundtrip);
    RUN(close_req_roundtrip);
    RUN(fstat_req_roundtrip);
    RUN(ftruncate_req_roundtrip);
    RUN(unlink_req_roundtrip);
    RUN(rename_req_roundtrip);
    RUN(mkdir_req_roundtrip);
    RUN(open_ok_roundtrip);
    RUN(read_ok_roundtrip);
    RUN(read_ok_eof);
    RUN(write_ok_roundtrip);
    RUN(seek_ok_roundtrip);
    RUN(reqid_resp_roundtrip);
    RUN(fstat_ok_roundtrip);
    RUN(io_error_roundtrip);

    printf("\n--- Byte Layout Verification (Go-compatible) ---\n");
    RUN(open_req_byte_layout);
    RUN(rename_req_byte_layout);
    RUN(envelope_byte_layout);

    printf("\n--- Short Payload Rejection ---\n");
    RUN(short_payload_open_req);
    RUN(short_payload_read_req);
    RUN(short_payload_write_req);
    RUN(short_payload_seek_req);
    RUN(short_payload_close_req);
    RUN(short_payload_fstat_req);
    RUN(short_payload_ftruncate_req);
    RUN(short_payload_unlink_req);
    RUN(short_payload_rename_req);
    RUN(short_payload_mkdir_req);
    RUN(short_payload_open_ok);
    RUN(short_payload_read_ok);
    RUN(short_payload_write_ok);
    RUN(short_payload_seek_ok);
    RUN(short_payload_reqid_resp);
    RUN(short_payload_fstat_ok);
    RUN(short_payload_io_error);

    printf("\n--- Signed Int64 ---\n");
    RUN(seek_negative_offset);
    RUN(ftruncate_negative_length);

    printf("\n--- Flag Translation ---\n");
    RUN(flag_translation_wronly_creat_trunc);

    printf("\n--- Passthrough Mode ---\n");
    RUN(passthrough_file_ops);
    RUN(passthrough_mkdir_rename);
    RUN(passthrough_ftruncate);

    printf("\n--- Virtual FD ---\n");
    RUN(vfd_constants);

    printf("\n--- UTF-8 Paths ---\n");
    RUN(utf8_path_roundtrip);

    printf("\n--- Buffer Capacity / Boundary ---\n");
    RUN(encode_open_req_tiny_buffer);
    RUN(encode_unlink_req_tiny_buffer);
    RUN(encode_rename_req_tiny_buffer);
    RUN(encode_mkdir_req_tiny_buffer);
    RUN(encode_write_req_tiny_buffer);
    RUN(encode_read_ok_tiny_buffer);
    RUN(zero_length_write_data);
    RUN(max_uint16_ids);
    RUN(empty_path_unlink);
    RUN(empty_paths_rename);
    RUN(open_ok_negative_file_size);
    RUN(io_error_all_errno_values);
    RUN(long_path_roundtrip);
    RUN(read_ok_empty_data);
    RUN(seek_all_whence_values);
    RUN(fstat_ok_large_mode);

    printf("\n--- Passthrough Edge Cases ---\n");
    RUN(passthrough_seek_end);
    RUN(passthrough_read_write_large);
    RUN(passthrough_fstat_permissions);

    printf("\n--- Tunnel Mode: Read-Ahead / Cache / Prefetch ---\n");
    RUN(tunnel_read_ahead_sequential);
    RUN(tunnel_backward_seek_hits_cache);
    RUN(tunnel_seek_outside_cache_refetches);
    RUN(tunnel_short_reads_no_corruption);
    RUN(tunnel_write_invalidates_cache);
    RUN(tunnel_close_with_inflight_prefetch);
    RUN(tunnel_seek_end_and_read);
    RUN(tunnel_oneshot_read_larger_than_file);
    RUN(tunnel_no_prefetch_past_eof_sequential);
    RUN(tunnel_no_read_past_eof_tail);
    RUN(tunnel_short_reads_keep_size);
    RUN(tunnel_growing_file_keeps_prefetching);
    RUN(tunnel_truncated_file_empty_reply_sets_size);
    RUN(tunnel_req_id_skips_parked_prefetch);
    RUN(tunnel_prefetches_never_starve_requests);
    RUN(tunnel_large_read_and_write_are_capped);
    RUN(tunnel_range_cache_budget_is_process_wide);
    RUN(tunnel_dead_fails_fast);
    RUN(tunnel_file_id_wrap_skips_open_files);
    RUN(tunnel_concurrent_files);

    printf("\n--- Path Queries: stat / check / listdir ---\n");
    RUN(stat_ok_roundtrip);
    RUN(listdir_roundtrip);
    RUN(path_req_layout);
    RUN(mode_from_wire_values);
    RUN(tunnel_stat_and_check);
    RUN(tunnel_listdir);
    RUN(tunnel_listdir_skips_separators);

    printf("\n--- Audit fixes: types, offsets, replies, fds, setup ---\n");
    RUN(tunnel_fstat_reports_fifo);
    RUN(tunnel_fstat_device_size_stays_zero);
    RUN(tunnel_failed_write_reseeks);
    RUN(tunnel_fifo_failed_seek_keeps_stream);
    RUN(tunnel_fifo_data_not_range_cached);
    RUN(tunnel_growing_file_empty_prefetch_is_not_eof);
    RUN(tunnel_seek_errors_and_overflow);
    RUN(tunnel_bad_counts_are_eio);
    RUN(tunnel_real_fds_pass_through);
    RUN(tunnel_seek_into_cancelled_prefetch_uses_cache);
    RUN(tunnel_setup_fails_closed);
    RUN(tunnel_setup_sends_token_first);

    printf("\n========================================\n");
    printf("Results: %d/%d tests passed\n", tests_passed, tests_run);
    printf("========================================\n");

    return (tests_passed == tests_run) ? 0 : 1;
}
