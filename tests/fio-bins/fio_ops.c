/*
 * fio_ops — Exercises remaining fio operations: lseek, fstat, ftruncate,
 *           unlink, rename, mkdir.
 *
 * Usage: fio_ops <workdir>
 *
 * All operations are performed under <workdir>. Each test prints PASS/FAIL.
 * Exit code 0 if all pass, 1 on first failure.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <errno.h>
#include <sys/stat.h>
#include "fio.h"

#define CHECK(cond, fmt, ...) do { \
    if (!(cond)) { \
        fprintf(stderr, "FAIL: " fmt "\n", ##__VA_ARGS__); \
        return 1; \
    } \
} while (0)

static int test_lseek(const char *workdir) {
    char path[1024];
    snprintf(path, sizeof(path), "%s/seek_test.bin", workdir);

    /* Create a file with known content */
    int fd = fio_open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    CHECK(fd >= 0, "lseek: open for write failed: %s", strerror(errno));

    const char *data = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
    ssize_t nw = fio_write(fd, data, 26);
    CHECK(nw == 26, "lseek: write failed: %zd", nw);
    fio_close(fd);

    /* Reopen for reading */
    fd = fio_open(path, O_RDONLY, 0);
    CHECK(fd >= 0, "lseek: open for read failed: %s", strerror(errno));

    /* Seek to offset 10, read 5 bytes -> "KLMNO" */
    off_t pos = fio_lseek(fd, 10, SEEK_SET);
    CHECK(pos == 10, "lseek: SEEK_SET returned %lld, expected 10", (long long)pos);

    char buf[16];
    ssize_t nr = fio_read(fd, buf, 5);
    CHECK(nr == 5, "lseek: read after seek returned %zd", nr);
    CHECK(memcmp(buf, "KLMNO", 5) == 0, "lseek: got '%.*s', expected 'KLMNO'", 5, buf);

    /* SEEK_CUR: already at 15, seek +5 -> 20 */
    pos = fio_lseek(fd, 5, SEEK_CUR);
    CHECK(pos == 20, "lseek: SEEK_CUR returned %lld, expected 20", (long long)pos);

    nr = fio_read(fd, buf, 6);
    CHECK(nr == 6, "lseek: read after SEEK_CUR returned %zd", nr);
    CHECK(memcmp(buf, "UVWXYZ", 6) == 0, "lseek: got '%.*s', expected 'UVWXYZ'", 6, buf);

    /* SEEK_END: seek to -3 from end -> offset 23 */
    pos = fio_lseek(fd, -3, SEEK_END);
    CHECK(pos == 23, "lseek: SEEK_END returned %lld, expected 23", (long long)pos);

    nr = fio_read(fd, buf, 3);
    CHECK(nr == 3, "lseek: read after SEEK_END returned %zd", nr);
    CHECK(memcmp(buf, "XYZ", 3) == 0, "lseek: got '%.*s', expected 'XYZ'", 3, buf);

    fio_close(fd);
    printf("PASS: lseek (SEEK_SET, SEEK_CUR, SEEK_END)\n");
    return 0;
}

static int test_fstat(const char *workdir) {
    char path[1024];
    snprintf(path, sizeof(path), "%s/stat_test.bin", workdir);

    /* Create a file with 100 bytes */
    int fd = fio_open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    CHECK(fd >= 0, "fstat: open failed: %s", strerror(errno));

    char data[100];
    memset(data, 'X', sizeof(data));
    ssize_t nw = fio_write(fd, data, sizeof(data));
    CHECK(nw == 100, "fstat: write failed: %zd", nw);
    fio_close(fd);

    /* Reopen and fstat */
    fd = fio_open(path, O_RDONLY, 0);
    CHECK(fd >= 0, "fstat: reopen failed: %s", strerror(errno));

    struct stat st;
    int rc = fio_fstat(fd, &st);
    CHECK(rc == 0, "fstat: fstat failed: %s", strerror(errno));
    CHECK(st.st_size == 100, "fstat: size=%lld, expected 100", (long long)st.st_size);
    CHECK(S_ISREG(st.st_mode), "fstat: not a regular file (mode=0%o)", st.st_mode);

    fio_close(fd);
    printf("PASS: fstat (size and mode)\n");
    return 0;
}

static int test_ftruncate(const char *workdir) {
    char path[1024];
    snprintf(path, sizeof(path), "%s/trunc_test.bin", workdir);

    /* Create a file with 200 bytes */
    int fd = fio_open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    CHECK(fd >= 0, "ftruncate: open failed: %s", strerror(errno));

    char data[200];
    memset(data, 'Y', sizeof(data));
    ssize_t nw = fio_write(fd, data, sizeof(data));
    CHECK(nw == 200, "ftruncate: write failed: %zd", nw);

    /* Truncate to 50 bytes */
    int rc = fio_ftruncate(fd, 50);
    CHECK(rc == 0, "ftruncate: ftruncate failed: %s", strerror(errno));

    /* Verify size via fstat */
    struct stat st;
    rc = fio_fstat(fd, &st);
    CHECK(rc == 0, "ftruncate: fstat failed: %s", strerror(errno));
    CHECK(st.st_size == 50, "ftruncate: size=%lld, expected 50", (long long)st.st_size);

    fio_close(fd);
    printf("PASS: ftruncate (200 -> 50 bytes)\n");
    return 0;
}

static int test_unlink(const char *workdir) {
    char path[1024];
    snprintf(path, sizeof(path), "%s/unlink_test.txt", workdir);

    /* Create a file */
    int fd = fio_open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    CHECK(fd >= 0, "unlink: open failed: %s", strerror(errno));
    fio_write(fd, "delete me", 9);
    fio_close(fd);

    /* Verify it exists by opening for read */
    fd = fio_open(path, O_RDONLY, 0);
    CHECK(fd >= 0, "unlink: file doesn't exist before unlink");
    fio_close(fd);

    /* Unlink */
    int rc = fio_unlink(path);
    CHECK(rc == 0, "unlink: fio_unlink failed: %s", strerror(errno));

    /* Verify it's gone */
    fd = fio_open(path, O_RDONLY, 0);
    CHECK(fd < 0, "unlink: file still exists after unlink");

    printf("PASS: unlink\n");
    return 0;
}

static int test_rename(const char *workdir) {
    char oldpath[1024], newpath[1024];
    snprintf(oldpath, sizeof(oldpath), "%s/rename_old.txt", workdir);
    snprintf(newpath, sizeof(newpath), "%s/rename_new.txt", workdir);

    /* Create a file with content */
    int fd = fio_open(oldpath, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    CHECK(fd >= 0, "rename: open failed: %s", strerror(errno));
    const char *content = "rename test content";
    fio_write(fd, content, (size_t)strlen(content));
    fio_close(fd);

    /* Rename */
    int rc = fio_rename(oldpath, newpath);
    CHECK(rc == 0, "rename: fio_rename failed: %s", strerror(errno));

    /* Old path should not exist */
    fd = fio_open(oldpath, O_RDONLY, 0);
    CHECK(fd < 0, "rename: old file still exists");

    /* New path should exist with same content */
    fd = fio_open(newpath, O_RDONLY, 0);
    CHECK(fd >= 0, "rename: new file doesn't exist: %s", strerror(errno));

    char buf[64];
    ssize_t nr = fio_read(fd, buf, sizeof(buf));
    CHECK(nr == (ssize_t)strlen(content), "rename: read returned %zd", nr);
    CHECK(memcmp(buf, content, (size_t)nr) == 0, "rename: content mismatch");
    fio_close(fd);

    printf("PASS: rename\n");
    return 0;
}

static int test_mkdir(const char *workdir) {
    char dirpath[1024], filepath[1024];
    snprintf(dirpath, sizeof(dirpath), "%s/newdir", workdir);
    snprintf(filepath, sizeof(filepath), "%s/newdir/file.txt", workdir);

    /* Create directory */
    int rc = fio_mkdir(dirpath, 0755);
    CHECK(rc == 0, "mkdir: fio_mkdir failed: %s", strerror(errno));

    /* Create a file inside the new directory */
    int fd = fio_open(filepath, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    CHECK(fd >= 0, "mkdir: open file in new dir failed: %s", strerror(errno));
    fio_write(fd, "in subdir", 9);
    fio_close(fd);

    /* Read it back */
    fd = fio_open(filepath, O_RDONLY, 0);
    CHECK(fd >= 0, "mkdir: reopen file in new dir failed: %s", strerror(errno));
    char buf[16];
    ssize_t nr = fio_read(fd, buf, sizeof(buf));
    CHECK(nr == 9, "mkdir: read returned %zd", nr);
    CHECK(memcmp(buf, "in subdir", 9) == 0, "mkdir: content mismatch");
    fio_close(fd);

    /* mkdir on existing dir should fail with EEXIST */
    rc = fio_mkdir(dirpath, 0755);
    CHECK(rc < 0, "mkdir: creating existing dir should fail");

    printf("PASS: mkdir\n");
    return 0;
}

int main(int argc, char *argv[]) {
    if (argc != 2) {
        fprintf(stderr, "usage: fio_ops <workdir>\n");
        return 1;
    }

    const char *workdir = argv[1];
    int failed = 0;

    failed |= test_lseek(workdir);
    failed |= test_fstat(workdir);
    failed |= test_ftruncate(workdir);
    failed |= test_unlink(workdir);
    failed |= test_rename(workdir);
    failed |= test_mkdir(workdir);

    if (failed) {
        fprintf(stderr, "\nSome tests FAILED\n");
        return 1;
    }

    printf("\nAll fio_ops tests PASSED\n");
    return 0;
}
