#ifndef FIO_H
#define FIO_H

#include <stdint.h>
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
 * server over TCP (authenticated with FFOIP_TOKEN). When unset, they pass
 * through to real POSIX syscalls. If FFOIP_PORT is set but the tunnel can't
 * be set up, tunneled calls fail with EIO; they never fall back to this
 * machine's files.
 *
 * Virtual file descriptors are a small range starting at 0x40000000, far
 * above real fds; real fds passed in go to the real syscalls.
 *
 * Inside libavformat, include this after os_support.h when calling
 * fio_stat/fio_fstat: on Windows os_support.h renames `struct stat`.
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

/* 1 when FFOIP_PORT is set (file operations go to the client, or fail with
 * EIO if the tunnel could not be set up), 0 in passthrough mode. Patched call
 * sites use this to keep FFmpeg's own code path when not tunneled. */
int   fio_tunneled(void);

/* stat(2) on a path. When tunneled, only st_size, st_mode and st_mtime are
 * filled in. */
int   fio_stat(const char *path, struct stat *st);

/* Check a path in one round trip. Returns -1 with errno set (ENOENT, ...) if
 * it can't be reached, otherwise the FIO_*_OK bits the calling user is
 * granted, as access(2) would report them. */
#define FIO_R_OK 4
#define FIO_W_OK 2
#define FIO_X_OK 1
int   fio_check(const char *path);

/* Directory listing. "." and ".." are never returned. Entry attributes come
 * from lstat(2): a symlink is FIO_DT_LINK. In passthrough mode on Windows
 * fio_opendir fails with ENOSYS. */
#define FIO_DT_UNKNOWN 0
#define FIO_DT_FILE    1
#define FIO_DT_DIR     2
#define FIO_DT_LINK    3
#define FIO_DT_FIFO    4
#define FIO_DT_CHR     5
#define FIO_DT_BLK     6
#define FIO_DT_SOCK    7

typedef struct fio_dir fio_dir;

typedef struct {
    const char *name;   /* valid until the next fio_readdir or fio_closedir */
    int      has_stat;  /* 0 if the entry's attributes could not be read */
    int      type;      /* FIO_DT_* */
    unsigned perm;      /* permission bits (0777) */
    int64_t  size;
    int64_t  mtime;     /* seconds since the epoch */
    int64_t  atime;     /* 0 when tunneled */
    int64_t  ctime;     /* 0 when tunneled */
    int64_t  uid;       /* 0 when tunneled */
    int64_t  gid;       /* 0 when tunneled */
} fio_dirent;

/* Returns NULL with errno set on failure. */
fio_dir *fio_opendir(const char *path);
/* Returns 1 and fills *entry, 0 at the end of the listing, -1 with errno. */
int   fio_readdir(fio_dir *dir, fio_dirent *entry);
int   fio_closedir(fio_dir *dir);

#ifdef __cplusplus
}
#endif

#endif /* FIO_H */
