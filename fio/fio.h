#ifndef FIO_H
#define FIO_H

#include <sys/types.h>
#include <sys/stat.h>
#include <fcntl.h>

#ifdef _WIN32
#include <io.h>
#else
#include <unistd.h>
#endif

/*
 * fio — POSIX file I/O over TCP tunnel for ffmpeg-over-ip.
 *
 * When FFOIP_PORT is set, these functions tunnel file operations to a remote
 * server over TCP. When unset, they pass through to real POSIX syscalls.
 *
 * Virtual file descriptors start at FIO_VFD_BASE (10000) to avoid collisions
 * with real fds.
 */

#ifdef __cplusplus
extern "C" {
#endif

int   fio_open(const char *path, int flags, mode_t mode);
ssize_t fio_read(int fd, void *buf, size_t count);
ssize_t fio_write(int fd, const void *buf, size_t count);
off_t fio_lseek(int fd, off_t offset, int whence);
int   fio_close(int fd);
int   fio_fstat(int fd, struct stat *buf);
int   fio_ftruncate(int fd, off_t length);
int   fio_unlink(const char *path);
int   fio_rename(const char *oldpath, const char *newpath);
int   fio_mkdir(const char *path, mode_t mode);

#ifdef __cplusplus
}
#endif

#endif /* FIO_H */
