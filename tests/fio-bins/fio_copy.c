/*
 * fio_copy — A simple file copy tool using the fio layer.
 *
 * Usage: fio_copy <src> <dst>
 *
 * When FFOIP_PORT is set, all file operations are tunneled through the
 * fio protocol to a remote server. This exercises the full data path:
 * C fio → loopback → Go server relay → Go client filehandler → filesystem.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <errno.h>
#include "fio.h"

#define BUF_SIZE 32768

int main(int argc, char *argv[]) {
    if (argc != 3) {
        fprintf(stderr, "usage: fio_copy <src> <dst>\n");
        return 1;
    }

    const char *src_path = argv[1];
    const char *dst_path = argv[2];

    /* Open source file for reading */
    int src_fd = fio_open(src_path, O_RDONLY, 0);
    if (src_fd < 0) {
        fprintf(stderr, "fio_copy: cannot open '%s': %s\n", src_path, strerror(errno));
        return 1;
    }

    /* Open destination file for writing (create/truncate) */
    int dst_fd = fio_open(dst_path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    if (dst_fd < 0) {
        fprintf(stderr, "fio_copy: cannot create '%s': %s\n", dst_path, strerror(errno));
        fio_close(src_fd);
        return 1;
    }

    /* Copy loop */
    unsigned char buf[BUF_SIZE];
    ssize_t nread;
    while ((nread = fio_read(src_fd, buf, sizeof(buf))) > 0) {
        ssize_t total_written = 0;
        while (total_written < nread) {
            ssize_t nwritten = fio_write(dst_fd, buf + total_written,
                                         (size_t)(nread - total_written));
            if (nwritten < 0) {
                fprintf(stderr, "fio_copy: write error: %s\n", strerror(errno));
                fio_close(src_fd);
                fio_close(dst_fd);
                return 1;
            }
            total_written += nwritten;
        }
    }

    if (nread < 0) {
        fprintf(stderr, "fio_copy: read error: %s\n", strerror(errno));
        fio_close(src_fd);
        fio_close(dst_fd);
        return 1;
    }

    /* Close both files */
    if (fio_close(src_fd) < 0) {
        fprintf(stderr, "fio_copy: error closing source: %s\n", strerror(errno));
        fio_close(dst_fd);
        return 1;
    }
    if (fio_close(dst_fd) < 0) {
        fprintf(stderr, "fio_copy: error closing destination: %s\n", strerror(errno));
        return 1;
    }

    return 0;
}
